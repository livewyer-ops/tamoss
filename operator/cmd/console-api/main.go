package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	crzap "sigs.k8s.io/controller-runtime/pkg/log/zap"

	tamossv1alpha1 "github.com/livewyer-ops/tamoss/operator/api/v1alpha1"
	"github.com/livewyer-ops/tamoss/operator/internal/consoleapi"
)

const (
	instanceLabel       = "app.kubernetes.io/instance"
	defaultBindAddress  = ":8080"
	defaultRefresh      = 5 * time.Second
	shutdownGracePeriod = 10 * time.Second

	consoleAuthModeEnv              = "TAMOSS_CONSOLE_AUTH_MODE"
	consoleForwardAuthSecretFileEnv = "TAMOSS_CONSOLE_FORWARD_AUTH_SECRET_FILE"
	consoleGroupRoleBindingsEnv     = "TAMOSS_CONSOLE_GROUP_ROLE_BINDINGS"
	consoleAuthModeForwardAuth      = "forward-auth"
	consoleAuthModeDevelopment      = "development-anonymous"
	consoleAuthModeUnavailable      = "unavailable"
	maxForwardAuthSecretFileSize    = 4096

	// One uncached client serves the snapshotter's live IngestRun reads, the
	// command API, and browser history traversal. A single sparse history page
	// can issue up to 32 bounded List calls, and up to four such reads run at
	// once, so the client-go defaults of QPS 5 and burst 10 would queue those
	// calls in the client-side limiter until the 4s request timeout expires and
	// would starve the periodic snapshot refresh behind them. These limits let
	// one worst-case traversal proceed without queueing while still bounding
	// what a single Console replica can ask of the API server.
	consoleClientQPS   = 50
	consoleClientBurst = 100
)

type options struct {
	bindAddress     string
	namespace       string
	instance        string
	refreshInterval time.Duration
	authenticator   consoleapi.Authenticator
	cursorKey       []byte
}

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(logger); err != nil {
		logger.Error("console API stopped", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	ctrl.SetLogger(crzap.New(crzap.WriteTo(os.Stdout)))
	opts, err := parseOptions()
	if err != nil {
		return err
	}

	scheme := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(tamossv1alpha1.AddToScheme(scheme))

	config, err := ctrl.GetConfig()
	if err != nil {
		return fmt.Errorf("load Kubernetes configuration: %w", err)
	}
	config.UserAgent = "tamoss-console-api/1.0"
	applyConsoleClientRateLimits(config)
	runtimeCache, err := newRuntimeCache(config, scheme, opts.namespace, opts.instance)
	if err != nil {
		return fmt.Errorf("create Kubernetes cache: %w", err)
	}
	ingestRunReader, err := client.New(config, client.Options{Scheme: scheme})
	if err != nil {
		return fmt.Errorf("create live IngestRun reader: %w", err)
	}
	ingestRuns, err := consoleapi.NewIngestRunReadStore(consoleapi.IngestRunReadConfig{
		Reader:    ingestRunReader,
		Namespace: opts.namespace,
		Instance:  opts.instance,
		CursorKey: opts.cursorKey,
	})
	if err != nil {
		return fmt.Errorf("create IngestRun read store: %w", err)
	}
	commands, err := consoleapi.NewCommandAPI(consoleapi.CommandAPIConfig{
		Client:        ingestRunReader,
		Authenticator: opts.authenticator,
		Auditor:       consoleapi.NewSlogCommandAuditor(logger),
		Namespace:     opts.namespace,
		Instance:      opts.instance,
	})
	if err != nil {
		return fmt.Errorf("create Console command API: %w", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := registerInformers(ctx, runtimeCache); err != nil {
		return err
	}
	snapshotter, err := consoleapi.NewSnapshotter(runtimeCache, ingestRunReader, opts.namespace, opts.instance)
	if err != nil {
		return err
	}
	monitor := consoleapi.NewMonitor(snapshotter)
	httpServer := newConsoleHTTPServer(opts.bindAddress, consoleapi.NewServer(
		monitor,
		opts.authenticator,
		consoleapi.WithIngestRunReadAPI(ingestRuns),
		consoleapi.WithCommandAPI(commands),
		// Shutdown does not cancel in-flight request contexts, so event
		// streams need the process lifetime to close before the grace period.
		consoleapi.WithShutdownContext(ctx),
	).Handler())

	cacheErrors := make(chan error, 1)
	go func() { cacheErrors <- runtimeCache.Start(ctx) }()
	go func() {
		if runtimeCache.WaitForCacheSync(ctx) {
			logger.Info("Kubernetes cache synced", "namespace", opts.namespace, "instance", opts.instance)
			monitor.Run(ctx, opts.refreshInterval, func(err error) {
				logger.Warn("runtime refresh failed", "error", err)
			})
		}
	}()

	serverErrors := make(chan error, 1)
	go func() {
		logger.Info("console API listening", "address", opts.bindAddress)
		serverErrors <- httpServer.ListenAndServe()
	}()

	var runErr error
	select {
	case <-ctx.Done():
	case err := <-cacheErrors:
		if err != nil {
			runErr = fmt.Errorf("kubernetes cache stopped: %w", err)
		}
		stop()
	case err := <-serverErrors:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			runErr = fmt.Errorf("http server stopped: %w", err)
		}
		stop()
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownGracePeriod)
	defer cancel()
	if err := httpServer.Shutdown(shutdownCtx); err != nil && runErr == nil {
		runErr = fmt.Errorf("shut down HTTP server: %w", err)
	}
	return runErr
}

// applyConsoleClientRateLimits replaces the client-go defaults on the shared
// uncached client. See consoleClientQPS for why the defaults are too low.
func applyConsoleClientRateLimits(config *rest.Config) {
	config.QPS = consoleClientQPS
	config.Burst = consoleClientBurst
}

func newConsoleHTTPServer(address string, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:              address,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       5 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    64 << 10,
	}
}

func parseOptions() (options, error) {
	opts := options{}
	flag.StringVar(
		&opts.bindAddress,
		"bind-address",
		envOrDefault("TAMOSS_CONSOLE_BIND_ADDRESS", defaultBindAddress),
		"HTTP bind address",
	)
	flag.StringVar(
		&opts.namespace,
		"namespace",
		envOrDefault("TAMOSS_CONSOLE_NAMESPACE", os.Getenv("POD_NAMESPACE")),
		"namespace containing the Tamoss instance",
	)
	flag.StringVar(&opts.instance, "instance", os.Getenv("TAMOSS_CONSOLE_INSTANCE"), "Tamoss instance name")
	flag.DurationVar(&opts.refreshInterval, "refresh-interval", defaultRefresh, "runtime snapshot refresh interval")
	flag.Parse()
	opts.namespace = strings.TrimSpace(opts.namespace)
	opts.instance = strings.TrimSpace(opts.instance)
	if opts.namespace == "" {
		return options{}, fmt.Errorf("namespace is required (set --namespace or TAMOSS_CONSOLE_NAMESPACE)")
	}
	if opts.instance == "" {
		return options{}, fmt.Errorf("instance is required (set --instance or TAMOSS_CONSOLE_INSTANCE)")
	}
	if opts.refreshInterval < time.Second || opts.refreshInterval > 5*time.Minute {
		return options{}, fmt.Errorf("refresh interval must be between 1s and 5m")
	}
	authenticator, cursorKey, err := consoleAuthenticatorFromEnvironment()
	if err != nil {
		return options{}, err
	}
	opts.authenticator = authenticator
	opts.cursorKey = cursorKey
	return opts, nil
}

func consoleAuthenticatorFromEnvironment() (consoleapi.Authenticator, []byte, error) {
	mode := strings.TrimSpace(os.Getenv(consoleAuthModeEnv))
	switch mode {
	case consoleAuthModeDevelopment:
		return consoleapi.NewDevelopmentAnonymousAuthenticator(), nil, nil
	case consoleAuthModeUnavailable:
		return consoleapi.NewUnavailableAuthenticator(), nil, nil
	case consoleAuthModeForwardAuth:
		secretFile := strings.TrimSpace(os.Getenv(consoleForwardAuthSecretFileEnv))
		if secretFile == "" {
			return nil, nil, fmt.Errorf("%s is required when %s=%s", consoleForwardAuthSecretFileEnv, consoleAuthModeEnv, consoleAuthModeForwardAuth)
		}
		if !filepath.IsAbs(secretFile) {
			return nil, nil, fmt.Errorf("%s must be an absolute path", consoleForwardAuthSecretFileEnv)
		}
		secret, err := readForwardAuthSecretFile(secretFile)
		if err != nil {
			return nil, nil, err
		}
		bindings, err := consoleapi.ParseGroupRoleBindingsJSON([]byte(os.Getenv(consoleGroupRoleBindingsEnv)))
		if err != nil {
			return nil, nil, err
		}
		authenticator, err := consoleapi.NewForwardAuthAuthenticator(consoleapi.ForwardAuthConfig{
			SharedSecret:      secret,
			GroupRoleBindings: bindings,
		})
		if err != nil {
			return nil, nil, err
		}
		return authenticator, consoleapi.DeriveIngestRunCursorKey(secret), nil
	case "":
		return nil, nil, fmt.Errorf("%s is required; use %q, %q, or the explicit development-only %q mode", consoleAuthModeEnv, consoleAuthModeForwardAuth, consoleAuthModeUnavailable, consoleAuthModeDevelopment)
	default:
		return nil, nil, fmt.Errorf("unsupported %s value %q", consoleAuthModeEnv, mode)
	}
}

func readForwardAuthSecretFile(path string) ([]byte, error) {
	// #nosec G304,G703 -- the operator supplies an absolute projected-Secret path.
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open Console forward-auth shared secret file: %w", err)
	}
	defer func() { _ = file.Close() }()
	secret, err := io.ReadAll(io.LimitReader(file, maxForwardAuthSecretFileSize+1))
	if err != nil {
		return nil, fmt.Errorf("read Console forward-auth shared secret file: %w", err)
	}
	if len(secret) > maxForwardAuthSecretFileSize {
		return nil, fmt.Errorf("console forward-auth shared secret file exceeds %d bytes", maxForwardAuthSecretFileSize)
	}
	return secret, nil
}

func envOrDefault(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}

func newRuntimeCache(config *rest.Config, scheme *runtime.Scheme, namespace, instance string) (cache.Cache, error) {
	namespaces := map[string]cache.Config{namespace: {}}
	byObject := runtimeCacheByObject(namespaces, instance)
	return cache.New(config, cache.Options{
		Scheme:                      scheme,
		DefaultNamespaces:           namespaces,
		ByObject:                    byObject,
		ReaderFailOnMissingInformer: true,
	})
}

func runtimeCacheByObject(namespaces map[string]cache.Config, instance string) map[client.Object]cache.ByObject {
	instanceSelector := labels.SelectorFromSet(labels.Set{instanceLabel: instance})
	byObject := make(map[client.Object]cache.ByObject)
	for _, object := range runtimeInformerObjects() {
		scope := cache.ByObject{Namespaces: namespaces}
		switch object.(type) {
		case *tamossv1alpha1.Tamoss:
			scope.Field = fields.OneTermEqualSelector("metadata.name", instance)
		case *tamossv1alpha1.StorageBackend, *corev1.Event:
			// StorageBackend roots are established from their spec reference;
			// Events are filtered against retained UID-bound objects. IngestRun
			// roots use bounded live GETs and deliberately have no informer.
		default:
			scope.Label = instanceSelector
		}
		byObject[object] = scope
	}
	return byObject
}

func registerInformers(ctx context.Context, runtimeCache cache.Cache) error {
	for _, object := range runtimeInformerObjects() {
		if _, err := runtimeCache.GetInformer(ctx, object); err != nil {
			return fmt.Errorf("register informer for %T: %w", object, err)
		}
	}
	return nil
}

func runtimeInformerObjects() []client.Object {
	return []client.Object{
		&tamossv1alpha1.Tamoss{},
		&appsv1.Deployment{},
		&appsv1.ReplicaSet{},
		&corev1.Service{},
		&discoveryv1.EndpointSlice{},
		&tamossv1alpha1.StorageBackend{},
		&corev1.Pod{},
		&batchv1.Job{},
		&corev1.Event{},
	}
}
