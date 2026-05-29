package metrics

import (
	"strings"
	"time"

	tamossv1alpha1 "github.com/livewyer-ops/tamoss/operator/api/v1alpha1"
	"github.com/prometheus/client_golang/prometheus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrlmetrics "sigs.k8s.io/controller-runtime/pkg/metrics"
)

var (
	operatorSchemaVersion = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "tamoss_operator_schema_version",
			Help: "Schema bundle version shipped by the running TAMOSS operator.",
		},
		[]string{"version"},
	)
	reconcilePhaseCount = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "tamoss_reconcile_phase_count",
			Help: "Number of TAMOSS reconciles observed by resulting phase.",
		},
		[]string{"phase"},
	)
	schemaMigrationsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "tamoss_schema_migrations_total",
			Help: "Number of schema migration events observed by result.",
		},
		[]string{"result"},
	)
	resourceCondition = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "tamoss_resource_condition",
			Help: "Current TAMOSS custom resource condition status. One sample is emitted for each observed condition state.",
		},
		[]string{"kind", "namespace", "name", "condition", "status", "reason"},
	)
	reconcileDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "tamoss_reconcile_duration_seconds",
			Help:    "Duration of TAMOSS operator reconcile loops.",
			Buckets: []float64{0.01, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60},
		},
		[]string{"controller", "result"},
	)
	reconcileErrors = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "tamoss_reconcile_errors_total",
			Help: "Number of TAMOSS operator reconcile loops that returned an error.",
		},
		[]string{"controller"},
	)
	providerReady = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "tamoss_provider_ready",
			Help: "Readiness of TAMOSS provider domains derived from status conditions.",
		},
		[]string{"kind", "namespace", "name", "domain", "provider", "ownership"},
	)
)

func init() {
	ctrlmetrics.Registry.MustRegister(
		operatorSchemaVersion,
		reconcilePhaseCount,
		schemaMigrationsTotal,
		resourceCondition,
		reconcileDuration,
		reconcileErrors,
		providerReady,
	)
}

func SetSchemaVersion(version string) {
	operatorSchemaVersion.Reset()
	operatorSchemaVersion.WithLabelValues(normalizeLabel(version, "unknown")).Set(1)
}

func RecordReconcilePhase(phase string) {
	reconcilePhaseCount.WithLabelValues(normalizeLabel(phase, "unknown")).Inc()
}

func RecordSchemaMigration(result string) {
	schemaMigrationsTotal.WithLabelValues(normalizeLabel(result, "unknown")).Inc()
}

func RecordTamossStatus(tamoss *tamossv1alpha1.Tamoss) {
	if tamoss == nil {
		return
	}
	recordResourceConditions("Tamoss", tamoss.Namespace, tamoss.Name, tamoss.Status.Conditions)
	recordProviderReadiness(tamoss)
}

func RecordStorageBackendStatus(storageBackend *tamossv1alpha1.StorageBackend) {
	if storageBackend == nil {
		return
	}
	recordResourceConditions("StorageBackend", storageBackend.Namespace, storageBackend.Name, storageBackend.Status.Conditions)
}

func RecordReconcile(controller, result string, duration time.Duration) {
	reconcileDuration.WithLabelValues(
		normalizeLabel(controller, "unknown"),
		normalizeLabel(result, "unknown"),
	).Observe(duration.Seconds())
}

func RecordReconcileError(controller string) {
	reconcileErrors.WithLabelValues(normalizeLabel(controller, "unknown")).Inc()
}

func recordResourceConditions(kind, namespace, name string, conditions []metav1.Condition) {
	labels := prometheus.Labels{
		"kind":      kind,
		"namespace": namespace,
		"name":      name,
	}
	resourceCondition.DeletePartialMatch(labels)
	for _, condition := range conditions {
		resourceCondition.WithLabelValues(
			kind,
			namespace,
			name,
			trimLabel(condition.Type, "unknown"),
			trimLabel(string(condition.Status), "Unknown"),
			trimLabel(condition.Reason, "unknown"),
		).Set(1)
	}
}

func recordProviderReadiness(tamoss *tamossv1alpha1.Tamoss) {
	labels := prometheus.Labels{
		"kind":      "Tamoss",
		"namespace": tamoss.Namespace,
		"name":      tamoss.Name,
	}
	providerReady.DeletePartialMatch(labels)

	conditions := conditionStatusByType(tamoss.Status.Conditions)
	recordProviderDomain(tamoss, "db", tamoss.Status.Providers.DB, conditionReadyValue(conditions, "BackendsReady", false))
	recordProviderDomain(tamoss, "s3", tamoss.Status.Providers.S3, conditionReadyValue(conditions, "BackendsReady", false))
	recordProviderDomain(tamoss, "auth", tamoss.Status.Providers.Auth, conditionReadyValue(conditions, "IdentityReady", false))

	routingReady := conditionReadyValue(conditions, "RoutingReady", false)
	if _, found := conditions["RoutingReady"]; !found && tamoss.Status.Providers.Routing.Ownership == tamossv1alpha1.ProviderOwnershipExternal {
		routingReady = 1
	}
	recordProviderDomain(tamoss, "routing", tamoss.Status.Providers.Routing, routingReady)
}

func recordProviderDomain(tamoss *tamossv1alpha1.Tamoss, domain string, status tamossv1alpha1.ProviderDomainStatus, value float64) {
	if status.Provider == "" || status.Ownership == "" {
		return
	}
	providerReady.WithLabelValues(
		"Tamoss",
		tamoss.Namespace,
		tamoss.Name,
		domain,
		trimLabel(status.Provider, "unknown"),
		trimLabel(string(status.Ownership), "unknown"),
	).Set(value)
}

func conditionStatusByType(conditions []metav1.Condition) map[string]metav1.ConditionStatus {
	statuses := make(map[string]metav1.ConditionStatus, len(conditions))
	for _, condition := range conditions {
		statuses[condition.Type] = condition.Status
	}
	return statuses
}

func conditionReadyValue(conditions map[string]metav1.ConditionStatus, condition string, fallback bool) float64 {
	status, found := conditions[condition]
	if !found {
		if fallback {
			return 1
		}
		return 0
	}
	if status == metav1.ConditionTrue {
		return 1
	}
	return 0
}

func normalizeLabel(value, fallback string) string {
	return strings.ToLower(trimLabel(value, fallback))
}

func trimLabel(value, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		value = fallback
	}
	return value
}
