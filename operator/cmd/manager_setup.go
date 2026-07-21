package main

import (
	"context"
	"crypto/tls"
	"os"
	"strings"
	"time"

	k8sdiscovery "k8s.io/client-go/discovery"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	crmanager "sigs.k8s.io/controller-runtime/pkg/manager"
	metricsfilters "sigs.k8s.io/controller-runtime/pkg/metrics/filters"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/livewyer-ops/tamoss/operator/internal/controller"
	"github.com/livewyer-ops/tamoss/operator/internal/controller/auth/authentik"
	operatordiscovery "github.com/livewyer-ops/tamoss/operator/internal/discovery"
	"github.com/livewyer-ops/tamoss/operator/internal/webhook/deleteprotection"
)

func watchNamespacesFromEnv() map[string]struct{} {
	return controller.ParseWatchNamespaces(os.Getenv("WATCH_NAMESPACES"))
}

func cacheOptionsForWatchNamespaces(watchNamespaces map[string]struct{}) cache.Options {
	options := cache.Options{}
	if len(watchNamespaces) == 0 {
		return options
	}
	options.DefaultNamespaces = map[string]cache.Config{}
	for namespace := range watchNamespaces {
		options.DefaultNamespaces[namespace] = cache.Config{}
	}
	setupLog.Info("watching configured namespaces", "namespaces", os.Getenv("WATCH_NAMESPACES"))
	return options
}

func managerTLSOptions(enableHTTP2 bool) []func(*tls.Config) {
	if enableHTTP2 {
		return nil
	}
	return []func(*tls.Config){
		func(c *tls.Config) {
			setupLog.Info("disabling http/2")
			c.NextProtos = []string{"http/1.1"}
		},
	}
}

func newOperatorManager(
	metricsAddr string,
	probeAddr string,
	secureMetrics bool,
	enableLeaderElection bool,
	tlsOpts []func(*tls.Config),
	cacheOptions cache.Options,
) (ctrl.Manager, error) {
	webhookServer := webhook.NewServer(webhook.Options{
		TLSOpts: tlsOpts,
	})
	metricsOptions := metricsserver.Options{
		BindAddress:   metricsAddr,
		SecureServing: secureMetrics,
		TLSOpts:       tlsOpts,
	}
	if secureMetrics {
		// Protect the metrics endpoint with TokenReview authentication and
		// SubjectAccessReview authorisation, matching the current kubebuilder
		// scaffold. The shipped deployment serves metrics insecurely on
		// localhost behind kube-rbac-proxy, so this only takes effect when
		// --metrics-secure is set explicitly.
		metricsOptions.FilterProvider = metricsfilters.WithAuthenticationAndAuthorization
	}
	return ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                        scheme,
		Cache:                         cacheOptions,
		Metrics:                       metricsOptions,
		WebhookServer:                 webhookServer,
		HealthProbeBindAddress:        probeAddr,
		LeaderElection:                enableLeaderElection,
		LeaderElectionID:              "73850053.tamoss.livewyer.io",
		LeaderElectionReleaseOnCancel: true,
	})
}

func addDependencyDiscovery(mgr ctrl.Manager) (*operatordiscovery.Manager, time.Duration, error) {
	dependencyClient, err := k8sdiscovery.NewDiscoveryClientForConfig(mgr.GetConfig())
	if err != nil {
		return nil, 0, err
	}
	dependencyDiscovery := operatordiscovery.NewManager(dependencyClient, operatordiscovery.OptionalResourceGVRs())
	interval := dependencyDiscoveryInterval()
	if err := dependencyDiscovery.Refresh(context.Background()); err != nil {
		setupLog.Info("dependency discovery prewarm completed with unavailable dependencies", "error", err)
	}
	if err := mgr.Add(crmanager.RunnableFunc(func(ctx context.Context) error {
		dependencyDiscovery.Run(ctx, interval)
		return nil
	})); err != nil {
		return nil, 0, err
	}
	return dependencyDiscovery, interval, nil
}

func setupControllers(
	mgr ctrl.Manager,
	watchNamespaces map[string]struct{},
	dependencyDiscovery *operatordiscovery.Manager,
	dependencyProbeInterval time.Duration,
) error {
	tamossReconciler := &controller.TamossReconciler{
		Client:                      mgr.GetClient(),
		Scheme:                      mgr.GetScheme(),
		Recorder:                    eventRecorderFor(mgr, "tamoss-controller"),
		WatchNamespaces:             watchNamespaces,
		Discovery:                   dependencyDiscovery,
		DependencyProbeInterval:     dependencyProbeInterval,
		AuthentikPlatformNamespaces: authentik.NewPlatformNamespacePolicy(os.Getenv("TAMOSS_AUTHENTIK_PLATFORM_NAMESPACES")),
		AuthentikHTTPClient:         authentik.NewHTTPClient(),
	}
	if err := tamossReconciler.SetupWithManager(mgr); err != nil {
		return err
	}
	if err := (&controller.TamossHibernateReconciler{
		Client:          mgr.GetClient(),
		Scheme:          mgr.GetScheme(),
		Recorder:        eventRecorderFor(mgr, "tamosshibernate-controller"),
		WatchNamespaces: watchNamespaces,
	}).SetupWithManager(mgr); err != nil {
		return err
	}
	if dependencyDiscovery != nil {
		dependencyDiscovery.AddObserver(func(ctx context.Context) {
			if err := tamossReconciler.RegisterOptionalWatches(ctx); err != nil {
				setupLog.Error(err, "unable to register optional resource watches")
			}
		})
	}
	return (&controller.StorageBackendReconciler{
		Client:          mgr.GetClient(),
		Scheme:          mgr.GetScheme(),
		Recorder:        eventRecorderFor(mgr, "storagebackend-controller"),
		WatchNamespaces: watchNamespaces,
	}).SetupWithManager(mgr)
}

// eventRecorderFor returns a core/v1 event recorder. The controllers and
// their tests are built around record.EventRecorder; migrating to the
// events API is deliberately out of scope for this toolchain upgrade.
func eventRecorderFor(mgr ctrl.Manager, name string) record.EventRecorder {
	return mgr.GetEventRecorderFor(name) //nolint:staticcheck,nolintlint // events-API migration deferred
}

func registerDeleteProtectionWebhooks(mgr ctrl.Manager) {
	server := mgr.GetWebhookServer()
	server.Register(deleteprotection.TamossWebhookPath, &admission.Webhook{
		Handler: deleteprotection.NewHandler("Tamoss"),
	})
	server.Register(deleteprotection.StorageBackendWebhookPath, &admission.Webhook{
		Handler: deleteprotection.NewHandler(
			"StorageBackend",
			operatorCleanupUsername(operatorNamespaceFromEnv(), operatorServiceAccountFromEnv()),
		),
	})
	server.Register(deleteprotection.TamossHibernateWebhookPath, &admission.Webhook{
		Handler: deleteprotection.NewHandler("TamossHibernate"),
	})
}

func operatorNamespaceFromEnv() string {
	return strings.TrimSpace(os.Getenv("POD_NAMESPACE"))
}

func operatorServiceAccountFromEnv() string {
	return strings.TrimSpace(os.Getenv("POD_SERVICE_ACCOUNT_NAME"))
}

func operatorCleanupUsername(namespace, serviceAccount string) string {
	if namespace == "" || serviceAccount == "" {
		return ""
	}
	return "system:serviceaccount:" + namespace + ":" + serviceAccount
}

func addHealthChecks(mgr ctrl.Manager) error {
	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		return err
	}
	return mgr.AddReadyzCheck("readyz", healthz.Ping)
}

func dependencyDiscoveryInterval() time.Duration {
	value := os.Getenv("TAMOSS_DEPENDENCY_DISCOVERY_INTERVAL")
	if value == "" {
		return 5 * time.Minute
	}
	interval, err := time.ParseDuration(value)
	if err != nil || interval <= 0 {
		setupLog.Info("invalid TAMOSS_DEPENDENCY_DISCOVERY_INTERVAL; using default", "value", value)
		return 5 * time.Minute
	}
	return interval
}
