package main

import (
	"flag"
	"fmt"
	"os"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	_ "k8s.io/client-go/plugin/pkg/client/auth"

	gozap "go.uber.org/zap"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	crzap "sigs.k8s.io/controller-runtime/pkg/log/zap"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	tamossv1alpha1 "github.com/livewyer-ops/tamoss/operator/api/v1alpha1"
	operatormetrics "github.com/livewyer-ops/tamoss/operator/internal/metrics"
	"github.com/livewyer-ops/tamoss/operator/internal/schema"
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))

	utilruntime.Must(tamossv1alpha1.AddToScheme(scheme))
	utilruntime.Must(cnpgv1.AddToScheme(scheme))
	utilruntime.Must(gatewayv1.Install(scheme))
}

func main() {
	var metricsAddr string
	var enableLeaderElection bool
	var probeAddr string
	var secureMetrics bool
	var enableHTTP2 bool
	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	flag.BoolVar(&secureMetrics, "metrics-secure", false,
		"If set the metrics endpoint is served securely")
	flag.BoolVar(&enableHTTP2, "enable-http2", false,
		"If set, HTTP/2 will be enabled for the metrics and webhook servers")
	logging, err := loggingConfigFromEnv()
	if err != nil {
		fmt.Fprintf(os.Stderr, "invalid logging configuration: %v\n", err)
		os.Exit(1)
	}
	opts := crzap.Options{}
	logging.applyMinimumLevel(&opts)
	opts.BindFlags(flag.CommandLine)
	flag.Parse()
	if value, ok := parsedFlagValue("zap-log-level"); ok {
		level, err := parseLogLevel(value)
		if err != nil {
			fmt.Fprintf(os.Stderr, "invalid --zap-log-level: %v\n", err)
			os.Exit(1)
		}
		logging.BaseLevel = level
		logging.applyMinimumLevel(&opts)
	}

	ctrl.SetLogger(crzap.New(
		crzap.UseFlagOptions(&opts),
		crzap.RawZapOpts(gozap.WrapCore(logging.wrapCore)),
	))
	if err := schema.Verify(); err != nil {
		setupLog.Error(err, "schema version verification failed")
		os.Exit(1)
	}
	operatormetrics.SetSchemaVersion(schema.SchemaVersion)
	setupLog.Info("schema version loaded", "schemaVersion", schema.SchemaVersion)
	watchNamespaces := watchNamespacesFromEnv()
	cacheOptions := cacheOptionsForWatchNamespaces(watchNamespaces)
	tlsOpts := managerTLSOptions(enableHTTP2)
	mgr, err := newOperatorManager(metricsAddr, probeAddr, secureMetrics, enableLeaderElection, tlsOpts, cacheOptions)
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	dependencyDiscovery, dependencyProbeInterval, err := addDependencyDiscovery(mgr)
	if err != nil {
		setupLog.Error(err, "unable to add dependency discovery manager")
		os.Exit(1)
	}

	if err = setupControllers(mgr, watchNamespaces, dependencyDiscovery, dependencyProbeInterval); err != nil {
		setupLog.Error(err, "unable to create controllers")
		os.Exit(1)
	}
	if os.Getenv("ENABLE_WEBHOOKS") != "false" {
		registerDeleteProtectionWebhooks(mgr)
	} else {
		setupLog.Info("ENABLE_WEBHOOKS=false; skipping delete-protection webhook registration")
	}

	if err := addHealthChecks(mgr); err != nil {
		setupLog.Error(err, "unable to set up health checks")
		os.Exit(1)
	}

	setupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}
