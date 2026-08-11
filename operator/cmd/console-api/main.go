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
)

type options struct {
	bindAddress     string
	namespace       string
	instance        string
	refreshInterval time.Duration
	authenticator   consoleapi.Authenticator
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
	runtimeCache, err := newRuntimeCache(config, scheme, opts.namespace, opts.instance)
	if err != nil {
		return fmt.Errorf("create Kubernetes cache: %w", err)
	}
	ingestRunReader, err := client.New(config, client.Options{Scheme: scheme})
	if err != nil {
		return fmt.Errorf("create live IngestRun reader: %w", err)
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
	httpServer := &http.Server{
		Addr:              opts.bindAddress,
		Handler:           consoleapi.NewServer(monitor, opts.authenticator).Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    64 << 10,
	}

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
			runErr = fmt.Errorf("HTTP server stopped: %w", err)
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
	authenticator, err := consoleAuthenticatorFromEnvironment()
	if err != nil {
		return options{}, err
	}
	opts.authenticator = authenticator
	return opts, nil
}

func consoleAuthenticatorFromEnvironment() (consoleapi.Authenticator, error) {
	mode := strings.TrimSpace(os.Getenv(consoleAuthModeEnv))
	switch mode {
	case consoleAuthModeDevelopment:
		return consoleapi.NewDevelopmentAnonymousAuthenticator(), nil
	case consoleAuthModeUnavailable:
		return consoleapi.NewUnavailableAuthenticator(), nil
	case consoleAuthModeForwardAuth:
		secretFile := strings.TrimSpace(os.Getenv(consoleForwardAuthSecretFileEnv))
		if secretFile == "" {
			return nil, fmt.Errorf("%s is required when %s=%s", consoleForwardAuthSecretFileEnv, consoleAuthModeEnv, consoleAuthModeForwardAuth)
		}
		if !filepath.IsAbs(secretFile) {
			return nil, fmt.Errorf("%s must be an absolute path", consoleForwardAuthSecretFileEnv)
		}
		secret, err := readForwardAuthSecretFile(secretFile)
		if err != nil {
			return nil, err
		}
		bindings, err := consoleapi.ParseGroupRoleBindingsJSON([]byte(os.Getenv(consoleGroupRoleBindingsEnv)))
		if err != nil {
			return nil, err
		}
		return consoleapi.NewForwardAuthAuthenticator(consoleapi.ForwardAuthConfig{
			SharedSecret:      secret,
			GroupRoleBindings: bindings,
		})
	case "":
		return nil, fmt.Errorf("%s is required; use %q, %q, or the explicit development-only %q mode", consoleAuthModeEnv, consoleAuthModeForwardAuth, consoleAuthModeUnavailable, consoleAuthModeDevelopment)
	default:
		return nil, fmt.Errorf("unsupported %s value %q", consoleAuthModeEnv, mode)
	}
}

func readForwardAuthSecretFile(path string) ([]byte, error) {
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
		return nil, fmt.Errorf("Console forward-auth shared secret file exceeds %d bytes", maxForwardAuthSecretFileSize)
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
