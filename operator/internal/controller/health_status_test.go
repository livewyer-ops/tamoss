package controller

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	tamossv1alpha1 "github.com/livewyer-ops/tamoss/operator/api/v1alpha1"
	operatorstatus "github.com/livewyer-ops/tamoss/operator/internal/status"
)

func TestBackupPolicyConditionDisabledAndExternal(t *testing.T) {
	external := recoveryTamoss()
	setBackupPolicyCondition(&external.Status.Conditions, external)
	condition := findCondition(t, external.Status.Conditions, operatorstatus.ConditionBackupPolicyReady)
	if condition.Status != metav1.ConditionUnknown || condition.Reason != operatorstatus.ReasonBackupPolicyNotManaged {
		t.Fatalf("expected external backup ownership, got %#v", condition)
	}
	if external.Status.BackupPolicy.Managed || external.Status.BackupPolicy.Enabled {
		t.Fatalf("expected external backup policy to be unmanaged, got %#v", external.Status.BackupPolicy)
	}

	managed := cnpgBackupTamoss(false)
	setBackupPolicyCondition(&managed.Status.Conditions, managed)
	condition = findCondition(t, managed.Status.Conditions, operatorstatus.ConditionBackupPolicyReady)
	if condition.Status != metav1.ConditionTrue || condition.Reason != operatorstatus.ReasonBackupPolicyDisabled {
		t.Fatalf("expected disabled backup visibility, got %#v", condition)
	}
	if !managed.Status.BackupPolicy.Managed || managed.Status.BackupPolicy.Enabled {
		t.Fatalf("expected managed but disabled backup policy, got %#v", managed.Status.BackupPolicy)
	}
}

func TestBackupPolicyConditionConfigured(t *testing.T) {
	tamoss := cnpgBackupTamoss(true)
	setBackupPolicyCondition(&tamoss.Status.Conditions, tamoss)

	condition := findCondition(t, tamoss.Status.Conditions, operatorstatus.ConditionBackupPolicyReady)
	if condition.Status != metav1.ConditionTrue || condition.Reason != operatorstatus.ReasonBackupPolicyConfigured {
		t.Fatalf("expected configured backup policy, got %#v", condition)
	}
	if !tamoss.Status.BackupPolicy.Managed ||
		!tamoss.Status.BackupPolicy.Enabled ||
		tamoss.Status.BackupPolicy.Cluster != "example-db" ||
		tamoss.Status.BackupPolicy.ScheduledBackup != "example-db-backup" {
		t.Fatalf("expected configured backup status, got %#v", tamoss.Status.BackupPolicy)
	}
}

func TestObservedBackupPolicyMissingScheduledBackup(t *testing.T) {
	tamoss := cnpgBackupTamoss(true)
	reconciler := &TamossReconciler{Client: fake.NewClientBuilder().WithScheme(healthStatusScheme(t)).Build()}

	condition, status, err := reconciler.observedBackupPolicy(context.Background(), tamoss)
	if err != nil {
		t.Fatalf("expected backup condition: %v", err)
	}
	if condition.Status != metav1.ConditionFalse || condition.Reason != operatorstatus.ReasonBackupPolicyMissing {
		t.Fatalf("expected missing scheduled backup, got %#v", condition)
	}
	if status.ScheduledBackup != "example-db-backup" || status.Cluster != "example-db" {
		t.Fatalf("expected backup resource names in status, got %#v", status)
	}
}

func TestObservedBackupPolicyArchivingUnknown(t *testing.T) {
	tamoss := cnpgBackupTamoss(true)
	scheduled := scheduledBackup(tamoss)
	cluster := cnpgCluster(tamoss)
	reconciler := &TamossReconciler{
		Client: fake.NewClientBuilder().WithScheme(healthStatusScheme(t)).WithObjects(scheduled, cluster).Build(),
	}

	condition, status, err := reconciler.observedBackupPolicy(context.Background(), tamoss)
	if err != nil {
		t.Fatalf("expected backup condition: %v", err)
	}
	if condition.Status != metav1.ConditionFalse || condition.Reason != operatorstatus.ReasonBackupArchivingUnknown {
		t.Fatalf("expected unknown archiving state, got %#v", condition)
	}
	if !status.Enabled || status.Status != metav1.ConditionFalse || status.Reason != operatorstatus.ReasonBackupArchivingUnknown {
		t.Fatalf("expected unknown archiving status, got %#v", status)
	}
}

func TestObservedBackupPolicyFailureAndHealthy(t *testing.T) {
	tamoss := cnpgBackupTamoss(true)
	scheduled := scheduledBackup(tamoss)
	now := metav1.NewTime(time.Date(2026, 5, 22, 12, 0, 0, 0, time.UTC))
	scheduled.Status.LastScheduleTime = &now
	cluster := cnpgCluster(tamoss)
	cluster.Status.Conditions = []metav1.Condition{{
		Type:   string(cnpgv1.ConditionContinuousArchiving),
		Status: metav1.ConditionTrue,
		Reason: "Archiving",
	}}
	//nolint:staticcheck // CNPG deprecates these fields for backup plugins only; this operator configures the in-tree barman object store, which still populates them.
	cluster.Status.LastSuccessfulBackup = "2026-05-22T11:00:00Z"
	cluster.Status.LastFailedBackup = "2026-05-22T12:00:00Z"         //nolint:staticcheck // see above
	cluster.Status.FirstRecoverabilityPoint = "2026-05-22T10:00:00Z" //nolint:staticcheck // see above
	reconciler := &TamossReconciler{
		Client: fake.NewClientBuilder().WithScheme(healthStatusScheme(t)).WithObjects(scheduled, cluster).Build(),
	}

	condition, status, err := reconciler.observedBackupPolicy(context.Background(), tamoss)
	if err != nil {
		t.Fatalf("expected backup condition: %v", err)
	}
	if condition.Status != metav1.ConditionFalse || condition.Reason != operatorstatus.ReasonBackupPolicyFailed {
		t.Fatalf("expected failed backup state, got %#v", condition)
	}
	if status.LastFailedBackup != "2026-05-22T12:00:00Z" || status.FirstRecoverabilityPoint != "2026-05-22T10:00:00Z" {
		t.Fatalf("expected CNPG backup timestamps in status, got %#v", status)
	}

	cluster.Status.LastFailedBackup = "2026-05-22T10:00:00Z" //nolint:staticcheck // deprecated for backup plugins only
	reconciler.Client = fake.NewClientBuilder().WithScheme(healthStatusScheme(t)).WithObjects(scheduled, cluster).Build()
	condition, status, err = reconciler.observedBackupPolicy(context.Background(), tamoss)
	if err != nil {
		t.Fatalf("expected backup condition: %v", err)
	}
	if condition.Status != metav1.ConditionTrue || condition.Reason != operatorstatus.ReasonBackupPolicyHealthy {
		t.Fatalf("expected healthy backup state, got %#v", condition)
	}
	if status.Status != metav1.ConditionTrue || status.LastSuccessfulBackup != "2026-05-22T11:00:00Z" {
		t.Fatalf("expected healthy backup status, got %#v", status)
	}
}

func TestExternalS3DiagnosticSuccessFailureAndSkipped(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "https://app.tamoss.example.com")
		w.Header().Set("Access-Control-Allow-Methods", "PUT, OPTIONS")
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	tamoss := recoveryTamoss()
	tamoss.Spec.PublicEndpoint.BaseDomain = "tamoss.example.com"
	spec := externalStorageBackendSpecFixture()
	spec.Endpoint.Public.URL = server.URL
	reconciler := &StorageBackendReconciler{HTTPClient: server.Client()}

	diagnostic := reconciler.externalS3Diagnostic(context.Background(), tamoss, spec)
	if diagnostic.Status != metav1.ConditionTrue || diagnostic.Reason != operatorstatus.ReasonExternalS3DiagnosticReady {
		t.Fatalf("expected successful diagnostic, got %#v", diagnostic)
	}

	blockedServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer blockedServer.Close()
	spec.Endpoint.Public.URL = blockedServer.URL
	reconciler.HTTPClient = blockedServer.Client()
	diagnostic = reconciler.externalS3Diagnostic(context.Background(), tamoss, spec)
	if diagnostic.Status != metav1.ConditionFalse || diagnostic.Reason != operatorstatus.ReasonCORSMisconfigured {
		t.Fatalf("expected CORS diagnostic failure, got %#v", diagnostic)
	}

	tamoss.Spec.PublicEndpoint.BaseDomain = ""
	diagnostic = reconciler.externalS3Diagnostic(context.Background(), tamoss, spec)
	if diagnostic.Status != metav1.ConditionUnknown || diagnostic.Reason != operatorstatus.ReasonExternalS3DiagnosticSkipped {
		t.Fatalf("expected skipped diagnostic, got %#v", diagnostic)
	}
}

func cnpgBackupTamoss(enabled bool) *tamossv1alpha1.Tamoss {
	tamoss := recoveryTamoss()
	tamoss.Spec.Backends.DB = tamossv1alpha1.DBBackendSpec{
		ProvidedBy: tamossv1alpha1.BackendProvidedByCNPG,
		CNPG: &tamossv1alpha1.DBCNPGSpec{
			Backup: tamossv1alpha1.DBCNPGBackupSpec{
				Enabled:         enabled,
				Schedule:        "0 0 2 * * *",
				RetentionPolicy: "30d",
				ObjectStore: tamossv1alpha1.DBCNPGObjectStoreSpec{
					EndpointURL:    "https://s3.example.com",
					Bucket:         "pg-backups",
					ExistingSecret: "pg-backup-creds",
				},
			},
		},
	}
	return tamoss
}

func scheduledBackup(tamoss *tamossv1alpha1.Tamoss) *cnpgv1.ScheduledBackup {
	return &cnpgv1.ScheduledBackup{
		ObjectMeta: metav1.ObjectMeta{Name: tamoss.ResourceName("db-backup"), Namespace: tamoss.Namespace},
	}
}

func cnpgCluster(tamoss *tamossv1alpha1.Tamoss) *cnpgv1.Cluster {
	return &cnpgv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{Name: tamoss.ResourceName("db"), Namespace: tamoss.Namespace},
	}
}

func healthStatusScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	scheme := storageBackendTestScheme(t)
	if err := cnpgv1.AddToScheme(scheme); err != nil {
		t.Fatalf("add cnpg scheme: %v", err)
	}
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("add core scheme: %v", err)
	}
	return scheme
}

func findCondition(t *testing.T, conditions []metav1.Condition, conditionType string) metav1.Condition {
	t.Helper()
	for _, condition := range conditions {
		if condition.Type == conditionType {
			return condition
		}
	}
	t.Fatalf("condition %s not found", conditionType)
	return metav1.Condition{}
}
