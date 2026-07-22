package controller

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	tamossv1alpha1 "github.com/livewyer-ops/tamoss/operator/api/v1alpha1"
	operatorstatus "github.com/livewyer-ops/tamoss/operator/internal/status"
)

type fakeHibernationArtifactCleaner struct {
	calls          int
	namespace      string
	prefix         string
	objectsDeleted int64
	err            error
}

func (f *fakeHibernationArtifactCleaner) DeletePrefix(_ context.Context, namespace string, _ tamossv1alpha1.StorageBackendSpec, prefix string) (int64, error) {
	f.calls++
	f.namespace = namespace
	f.prefix = prefix
	if f.err != nil {
		return 0, f.err
	}
	return f.objectsDeleted, nil
}

func retentionTamossFixture(mode tamossv1alpha1.HibernationRetentionMode) (*tamossv1alpha1.Tamoss, *tamossv1alpha1.StorageBackend, *cnpgv1.Cluster) {
	tamoss := hibernateTamossFixture()
	tamoss.Status.Lifecycle = tamossv1alpha1.TamossLifecycleStatus{
		Phase:  string(tamossv1alpha1.TamossLifecyclePhaseResuming),
		Reason: operatorstatus.ReasonTamossResuming,
		ResolvedRestore: &tamossv1alpha1.TamossResolvedRestore{
			Restore:            tamossv1alpha1.DBCNPGRestoreSpec{Enabled: true, Source: "source-db"},
			StorageBackendName: "archive",
			ManifestKey:        "hibernate/example/snap-1/manifest.json",
			Checksum:           bootstrapTestChecksum,
		},
	}
	destination := hibernateDestinationFixture()
	destination.Spec.Hibernate.Retention.Mode = mode
	dbCluster := &cnpgv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{Name: tamoss.ResourceName("db"), Namespace: tamoss.Namespace},
		Status: cnpgv1.ClusterStatus{Conditions: []metav1.Condition{{
			Type:               string(cnpgv1.ConditionClusterReady),
			Status:             metav1.ConditionTrue,
			Reason:             "ClusterIsReady",
			Message:            "ready",
			LastTransitionTime: metav1.Now(),
		}}},
	}
	return tamoss, destination, dbCluster
}

func TestResumeArtifactRetentionRecordsThenDeletes(t *testing.T) {
	ctx := context.Background()
	scheme := hibernateTestScheme(t)
	tamoss, destination, dbCluster := retentionTamossFixture(tamossv1alpha1.HibernationRetentionModeDeleteAfterResume)
	cleaner := &fakeHibernationArtifactCleaner{objectsDeleted: 7}

	reconciler := &TamossReconciler{
		Client: fake.NewClientBuilder().WithInterceptorFuncs(fakeApplyInterceptor()).
			WithScheme(scheme).
			WithStatusSubresource(&tamossv1alpha1.Tamoss{}).
			WithObjects(tamoss, destination, dbCluster).
			Build(),
		Scheme:          scheme,
		ArtifactCleaner: cleaner,
	}

	result, err := reconciler.reconcileResumeArtifactRetention(ctx, tamoss)
	if err != nil {
		t.Fatalf("expected restore completion recording without error, got %v", err)
	}
	if result.RequeueAfter <= 0 {
		t.Fatalf("expected requeue after recording restore completion, got %#v", result)
	}
	if cleaner.calls != 0 {
		t.Fatalf("expected deletion to wait until completion is recorded, got %d calls", cleaner.calls)
	}

	updated := &tamossv1alpha1.Tamoss{}
	key := types.NamespacedName{Name: tamoss.Name, Namespace: tamoss.Namespace}
	if err := reconciler.Client.Get(ctx, key, updated); err != nil {
		t.Fatalf("get updated Tamoss: %v", err)
	}
	if updated.Status.Lifecycle.ResolvedRestore.ResumedAt == nil {
		t.Fatal("expected restore completion timestamp to be recorded")
	}
	if updated.Status.Lifecycle.Phase != string(tamossv1alpha1.TamossLifecyclePhaseRunning) {
		t.Fatalf("expected lifecycle Running after restore completes, got %#v", updated.Status.Lifecycle)
	}

	if _, err := reconciler.reconcileResumeArtifactRetention(ctx, updated); err != nil {
		t.Fatalf("expected artifact deletion without error, got %v", err)
	}
	if cleaner.calls != 1 || cleaner.prefix != "hibernate/example/snap-1/" || cleaner.namespace != "media" {
		t.Fatalf("expected one deletion of the artifact prefix, got %#v", cleaner)
	}
	if err := reconciler.Client.Get(ctx, key, updated); err != nil {
		t.Fatalf("get updated Tamoss: %v", err)
	}
	cleanup := updated.Status.Lifecycle.ResolvedRestore.Cleanup
	if cleanup.Phase != string(tamossv1alpha1.HibernationArtifactCleanupPhaseCompleted) || cleanup.ObjectsDeleted != 7 {
		t.Fatalf("expected completed cleanup, got %#v", cleanup)
	}

	// Terminal cleanup is idempotent.
	if result, err := reconciler.reconcileResumeArtifactRetention(ctx, updated); err != nil || result.RequeueAfter != 0 || cleaner.calls != 1 {
		t.Fatalf("expected terminal cleanup to be a no-op, got result %#v err %v calls %d", result, err, cleaner.calls)
	}
}

func TestResumeArtifactRetentionWaitsForDatabaseReadiness(t *testing.T) {
	ctx := context.Background()
	scheme := hibernateTestScheme(t)
	tamoss, destination, _ := retentionTamossFixture(tamossv1alpha1.HibernationRetentionModeDeleteAfterResume)
	cleaner := &fakeHibernationArtifactCleaner{}

	reconciler := &TamossReconciler{
		Client: fake.NewClientBuilder().WithInterceptorFuncs(fakeApplyInterceptor()).
			WithScheme(scheme).
			WithStatusSubresource(&tamossv1alpha1.Tamoss{}).
			WithObjects(tamoss, destination).
			Build(),
		Scheme:          scheme,
		ArtifactCleaner: cleaner,
	}

	result, err := reconciler.reconcileResumeArtifactRetention(ctx, tamoss)
	if err != nil || result.RequeueAfter != 0 || cleaner.calls != 0 {
		t.Fatalf("expected retention to wait for the restored database, got result %#v err %v calls %d", result, err, cleaner.calls)
	}
	updated := &tamossv1alpha1.Tamoss{}
	if err := reconciler.Client.Get(ctx, types.NamespacedName{Name: tamoss.Name, Namespace: tamoss.Namespace}, updated); err != nil {
		t.Fatalf("get updated Tamoss: %v", err)
	}
	if updated.Status.Lifecycle.ResolvedRestore.ResumedAt != nil {
		t.Fatal("expected no restore completion before the database is ready")
	}
}

func TestResumeArtifactRetentionTTLSchedulesAndRetries(t *testing.T) {
	ctx := context.Background()
	scheme := hibernateTestScheme(t)
	tamoss, destination, dbCluster := retentionTamossFixture(tamossv1alpha1.HibernationRetentionModeTTL)
	destination.Spec.Hibernate.Retention.TTLSecondsAfterResume = 3600
	resumedAt := metav1.NewTime(time.Now().Add(-time.Minute))
	tamoss.Status.Lifecycle.ResolvedRestore.ResumedAt = &resumedAt
	cleaner := &fakeHibernationArtifactCleaner{}

	reconciler := &TamossReconciler{
		Client: fake.NewClientBuilder().WithInterceptorFuncs(fakeApplyInterceptor()).
			WithScheme(scheme).
			WithStatusSubresource(&tamossv1alpha1.Tamoss{}).
			WithObjects(tamoss, destination, dbCluster).
			Build(),
		Scheme:          scheme,
		ArtifactCleaner: cleaner,
	}

	result, err := reconciler.reconcileResumeArtifactRetention(ctx, tamoss)
	if err != nil {
		t.Fatalf("expected TTL scheduling without error, got %v", err)
	}
	if result.RequeueAfter <= 0 || result.RequeueAfter > time.Hour {
		t.Fatalf("expected TTL requeue within an hour, got %#v", result)
	}
	if cleaner.calls != 0 {
		t.Fatalf("expected cleanup to wait for the TTL, got %d calls", cleaner.calls)
	}

	// Once due, transient deletion failures retry rather than block.
	overdue := metav1.NewTime(time.Now().Add(-2 * time.Hour))
	tamoss.Status.Lifecycle.ResolvedRestore.ResumedAt = &overdue
	if err := reconciler.Client.Status().Update(ctx, tamoss); err != nil {
		t.Fatalf("age the restore completion: %v", err)
	}
	cleaner.err = fmt.Errorf("access denied")
	result, err = reconciler.reconcileResumeArtifactRetention(ctx, tamoss)
	if err != nil {
		t.Fatalf("expected transient failure handling without error, got %v", err)
	}
	if result.RequeueAfter != hibernationCleanupRetryInterval {
		t.Fatalf("expected cleanup retry interval, got %#v", result)
	}
	updated := &tamossv1alpha1.Tamoss{}
	if err := reconciler.Client.Get(ctx, types.NamespacedName{Name: tamoss.Name, Namespace: tamoss.Namespace}, updated); err != nil {
		t.Fatalf("get updated Tamoss: %v", err)
	}
	cleanup := updated.Status.Lifecycle.ResolvedRestore.Cleanup
	if cleanup.Reason != operatorstatus.ReasonArtifactCleanupRetrying || !strings.Contains(cleanup.Message, "access denied") {
		t.Fatalf("expected retrying cleanup state, got %#v", cleanup)
	}

	cleaner.err = nil
	cleaner.objectsDeleted = 3
	if _, err := reconciler.reconcileResumeArtifactRetention(ctx, updated); err != nil {
		t.Fatalf("expected cleanup retry to succeed, got %v", err)
	}
	if cleaner.calls != 2 {
		t.Fatalf("expected a second deletion attempt, got %d", cleaner.calls)
	}
}
