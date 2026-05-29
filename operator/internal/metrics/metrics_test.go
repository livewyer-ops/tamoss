package metrics

import (
	"strings"
	"testing"
	"time"

	tamossv1alpha1 "github.com/livewyer-ops/tamoss/operator/api/v1alpha1"
	"github.com/prometheus/client_golang/prometheus/testutil"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestRecordTamossStatusMetrics(t *testing.T) {
	resetCollectorsForTest()
	tamoss := &tamossv1alpha1.Tamoss{
		ObjectMeta: metav1.ObjectMeta{Name: "example", Namespace: "tams"},
		Status: tamossv1alpha1.TamossStatus{
			Conditions: []metav1.Condition{
				{Type: "Ready", Status: metav1.ConditionFalse, Reason: "MissingSecret"},
				{Type: "BackendsReady", Status: metav1.ConditionTrue, Reason: "BackendReferencesConfigured"},
				{Type: "IdentityReady", Status: metav1.ConditionFalse, Reason: "IssuerUnreachable"},
			},
			Providers: tamossv1alpha1.ProviderStatus{
				DB:      tamossv1alpha1.ProviderDomainStatus{Provider: "cnpg", Ownership: tamossv1alpha1.ProviderOwnershipManaged},
				S3:      tamossv1alpha1.ProviderDomainStatus{Provider: "rustfs-operator", Ownership: tamossv1alpha1.ProviderOwnershipManaged},
				Auth:    tamossv1alpha1.ProviderDomainStatus{Provider: "external", Ownership: tamossv1alpha1.ProviderOwnershipExternal},
				Routing: tamossv1alpha1.ProviderDomainStatus{Provider: "external", Ownership: tamossv1alpha1.ProviderOwnershipExternal},
			},
		},
	}

	RecordTamossStatus(tamoss)

	if err := testutil.CollectAndCompare(resourceCondition, strings.NewReader(`# HELP tamoss_resource_condition Current TAMOSS custom resource condition status. One sample is emitted for each observed condition state.
# TYPE tamoss_resource_condition gauge
tamoss_resource_condition{condition="BackendsReady",kind="Tamoss",name="example",namespace="tams",reason="BackendReferencesConfigured",status="True"} 1
tamoss_resource_condition{condition="IdentityReady",kind="Tamoss",name="example",namespace="tams",reason="IssuerUnreachable",status="False"} 1
tamoss_resource_condition{condition="Ready",kind="Tamoss",name="example",namespace="tams",reason="MissingSecret",status="False"} 1
`)); err != nil {
		t.Fatal(err)
	}
	if err := testutil.CollectAndCompare(providerReady, strings.NewReader(`# HELP tamoss_provider_ready Readiness of TAMOSS provider domains derived from status conditions.
# TYPE tamoss_provider_ready gauge
tamoss_provider_ready{domain="auth",kind="Tamoss",name="example",namespace="tams",ownership="external",provider="external"} 0
tamoss_provider_ready{domain="db",kind="Tamoss",name="example",namespace="tams",ownership="managed",provider="cnpg"} 1
tamoss_provider_ready{domain="routing",kind="Tamoss",name="example",namespace="tams",ownership="external",provider="external"} 1
tamoss_provider_ready{domain="s3",kind="Tamoss",name="example",namespace="tams",ownership="managed",provider="rustfs-operator"} 1
`)); err != nil {
		t.Fatal(err)
	}
}

func TestRecordResourceConditionReplacesStaleLabels(t *testing.T) {
	resetCollectorsForTest()
	storageBackend := &tamossv1alpha1.StorageBackend{
		ObjectMeta: metav1.ObjectMeta{Name: "archive", Namespace: "tams"},
		Status: tamossv1alpha1.StorageBackendStatus{
			Conditions: []metav1.Condition{{Type: "Ready", Status: metav1.ConditionFalse, Reason: "BucketNotReady"}},
		},
	}
	RecordStorageBackendStatus(storageBackend)

	storageBackend.Status.Conditions = []metav1.Condition{{Type: "Ready", Status: metav1.ConditionTrue, Reason: "StorageBackendReady"}}
	RecordStorageBackendStatus(storageBackend)

	if err := testutil.CollectAndCompare(resourceCondition, strings.NewReader(`# HELP tamoss_resource_condition Current TAMOSS custom resource condition status. One sample is emitted for each observed condition state.
# TYPE tamoss_resource_condition gauge
tamoss_resource_condition{condition="Ready",kind="StorageBackend",name="archive",namespace="tams",reason="StorageBackendReady",status="True"} 1
`)); err != nil {
		t.Fatal(err)
	}
}

func TestRecordReconcileMetrics(t *testing.T) {
	resetCollectorsForTest()
	RecordReconcile("Tamoss", "Success", 1500*time.Millisecond)
	RecordReconcileError("Tamoss")

	if got := testutil.CollectAndCount(reconcileDuration, "tamoss_reconcile_duration_seconds"); got != 1 {
		t.Fatalf("expected one reconcile duration series, got %d", got)
	}
	if got := testutil.ToFloat64(reconcileErrors.WithLabelValues("tamoss")); got != 1 {
		t.Fatalf("expected one reconcile error, got %f", got)
	}
}

func TestSetSchemaVersionUsesNonPlaceholderLabel(t *testing.T) {
	resetCollectorsForTest()
	SetSchemaVersion("dev")

	if got := testutil.ToFloat64(operatorSchemaVersion.WithLabelValues("dev")); got != 1 {
		t.Fatalf("expected schema version gauge for dev, got %f", got)
	}
}

func resetCollectorsForTest() {
	operatorSchemaVersion.Reset()
	resourceCondition.Reset()
	providerReady.Reset()
	reconcileDuration.Reset()
	reconcileErrors.Reset()
}
