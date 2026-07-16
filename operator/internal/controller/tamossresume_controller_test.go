package controller

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/minio/minio-go/v7"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	tamossv1alpha1 "github.com/livewyer-ops/tamoss/operator/api/v1alpha1"
	cnpgbackend "github.com/livewyer-ops/tamoss/operator/internal/controller/backend/cnpg"
	schemabundle "github.com/livewyer-ops/tamoss/operator/internal/schema"
	operatorstatus "github.com/livewyer-ops/tamoss/operator/internal/status"
)

const testResumeManifestChecksum = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

func TestTamossResumeAppliesCNPGRecoveryCluster(t *testing.T) {
	ctx := context.Background()
	scheme := hibernateTestScheme(t)
	tamoss := hibernateTamossFixture()
	tamoss.Spec.Paused = true
	destination := hibernateDestinationFixture()
	resume := resumeFixture()
	reader := &fakeHibernationManifestReader{manifest: resumeManifestFixture()}

	reconciler := TamossResumeReconciler{
		Client: fake.NewClientBuilder().WithInterceptorFuncs(fakeApplyInterceptor()).
			WithScheme(scheme).
			WithStatusSubresource(&tamossv1alpha1.Tamoss{}, &tamossv1alpha1.TamossResume{}, &cnpgv1.Cluster{}).
			WithObjects(tamoss, destination, resume).
			Build(),
		Scheme:         scheme,
		ManifestReader: reader,
		PollInterval:   time.Second,
	}

	result, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: resume.Name, Namespace: resume.Namespace}})
	if err != nil {
		t.Fatalf("expected resume reconcile to apply recovery cluster, got error %v", err)
	}
	if result.RequeueAfter != time.Second {
		t.Fatalf("expected poll requeue, got %#v", result)
	}

	cluster := &cnpgv1.Cluster{}
	if err := reconciler.Client.Get(ctx, types.NamespacedName{Name: tamoss.ResourceName("db"), Namespace: tamoss.Namespace}, cluster); err != nil {
		t.Fatalf("expected recovery CNPG Cluster: %v", err)
	}
	if !resumeOperationMatches(cluster, resume) {
		t.Fatalf("expected resume operation annotation, got %#v", cluster.Annotations)
	}
	if cluster.Spec.Bootstrap == nil || cluster.Spec.Bootstrap.Recovery == nil {
		t.Fatalf("expected recovery bootstrap, got %#v", cluster.Spec.Bootstrap)
	}
	if cluster.Spec.Bootstrap.Recovery.Source != "source-db" {
		t.Fatalf("expected recovery source source-db, got %#v", cluster.Spec.Bootstrap.Recovery)
	}
	if cluster.Spec.Bootstrap.Recovery.RecoveryTarget == nil || cluster.Spec.Bootstrap.Recovery.RecoveryTarget.BackupID != "20260707T100000" {
		t.Fatalf("expected exact recovery target backup ID, got %#v", cluster.Spec.Bootstrap.Recovery.RecoveryTarget)
	}
	external := cluster.Spec.ExternalClusters[0]
	if external.BarmanObjectStore.DestinationPath != "s3://archive/hibernate/example/snap-1/cnpg" ||
		external.BarmanObjectStore.ServerName != "source-db" ||
		external.BarmanObjectStore.EndpointURL != "https://s3.eu-west-2.amazonaws.com" {
		t.Fatalf("unexpected recovery object store: %#v", external.BarmanObjectStore)
	}
	if external.BarmanObjectStore.AWS == nil ||
		external.BarmanObjectStore.AWS.AccessKeyIDReference.Key != "accessKeyID" ||
		external.BarmanObjectStore.AWS.SecretAccessKeyReference.Key != "secretAccessKey" {
		t.Fatalf("expected StorageBackend credential keys on recovery object store, got %#v", external.BarmanObjectStore.AWS)
	}

	updatedTamoss := &tamossv1alpha1.Tamoss{}
	if err := reconciler.Client.Get(ctx, types.NamespacedName{Name: tamoss.Name, Namespace: tamoss.Namespace}, updatedTamoss); err != nil {
		t.Fatalf("get updated Tamoss: %v", err)
	}
	if updatedTamoss.Status.Lifecycle.Phase != string(tamossv1alpha1.TamossLifecyclePhaseResuming) {
		t.Fatalf("expected Tamoss lifecycle Resuming, got %#v", updatedTamoss.Status.Lifecycle)
	}
	if updatedTamoss.Status.Lifecycle.ActiveOperationRef == nil || updatedTamoss.Status.Lifecycle.ActiveOperationRef.Name != resume.Name {
		t.Fatalf("expected active resume operation ref, got %#v", updatedTamoss.Status.Lifecycle.ActiveOperationRef)
	}

	updatedResume := &tamossv1alpha1.TamossResume{}
	if err := reconciler.Client.Get(ctx, types.NamespacedName{Name: resume.Name, Namespace: resume.Namespace}, updatedResume); err != nil {
		t.Fatalf("get updated TamossResume: %v", err)
	}
	if updatedResume.Status.Phase != string(tamossv1alpha1.TamossOperationPhaseRecoveringDatabase) {
		t.Fatalf("expected resume status RecoveringDatabase, got %#v", updatedResume.Status)
	}
	if updatedResume.Status.Artifact.CNPGBackup.DestinationPath != "s3://archive/hibernate/example/snap-1/cnpg" {
		t.Fatalf("expected artifact from manifest, got %#v", updatedResume.Status.Artifact)
	}
}

func TestTamossResumeCompletesWhenCNPGClusterIsReady(t *testing.T) {
	ctx := context.Background()
	scheme := hibernateTestScheme(t)
	tamoss := hibernateTamossFixture()
	tamoss.Status.Lifecycle = tamossv1alpha1.TamossLifecycleStatus{
		Phase:            string(tamossv1alpha1.TamossLifecyclePhaseHibernated),
		Reason:           operatorstatus.ReasonTamossHibernated,
		LastHibernateRef: operationObjectReference(hibernateFixture(), "TamossHibernate"),
	}
	destination := hibernateDestinationFixture()
	resume := resumeFixture()
	cluster := hibernateClusterFixture(tamoss)
	cluster.Annotations = resumeOperationAnnotations(resume)
	cluster.Status.Conditions = []metav1.Condition{{
		Type:    string(cnpgv1.ConditionClusterReady),
		Status:  metav1.ConditionTrue,
		Reason:  "ClusterIsReady",
		Message: "Cluster is ready",
	}}
	reader := &fakeHibernationManifestReader{manifest: resumeManifestFixture()}

	reconciler := TamossResumeReconciler{
		Client: fake.NewClientBuilder().WithInterceptorFuncs(fakeApplyInterceptor()).
			WithScheme(scheme).
			WithStatusSubresource(&tamossv1alpha1.Tamoss{}, &tamossv1alpha1.TamossResume{}, &cnpgv1.Cluster{}).
			WithObjects(tamoss, destination, resume, cluster).
			Build(),
		Scheme:         scheme,
		ManifestReader: reader,
	}

	request := ctrl.Request{NamespacedName: types.NamespacedName{Name: resume.Name, Namespace: resume.Namespace}}
	_, err := reconciler.Reconcile(ctx, request)
	if err != nil {
		t.Fatalf("expected resume reconcile to start services, got error %v", err)
	}
	markTamossResumeServicesReady(t, ctx, &reconciler, tamoss.Name)
	if _, err := reconciler.Reconcile(ctx, request); err != nil {
		t.Fatalf("expected ready services to complete Resume, got error %v", err)
	}

	updatedResume := &tamossv1alpha1.TamossResume{}
	if err := reconciler.Client.Get(ctx, types.NamespacedName{Name: resume.Name, Namespace: resume.Namespace}, updatedResume); err != nil {
		t.Fatalf("get updated TamossResume: %v", err)
	}
	if updatedResume.Status.Phase != string(tamossv1alpha1.TamossOperationPhaseCompleted) {
		t.Fatalf("expected resume status Completed, got %#v", updatedResume.Status)
	}
	if updatedResume.Status.Artifact.Cleanup.Phase != string(tamossv1alpha1.HibernationArtifactCleanupPhaseRetained) ||
		updatedResume.Status.Artifact.Cleanup.Reason != operatorstatus.ReasonArtifactCleanupRetained {
		t.Fatalf("expected retained artifact cleanup status, got %#v", updatedResume.Status.Artifact.Cleanup)
	}

	updatedTamoss := &tamossv1alpha1.Tamoss{}
	if err := reconciler.Client.Get(ctx, types.NamespacedName{Name: tamoss.Name, Namespace: tamoss.Namespace}, updatedTamoss); err != nil {
		t.Fatalf("get updated Tamoss: %v", err)
	}
	if updatedTamoss.Status.Lifecycle.Phase != string(tamossv1alpha1.TamossLifecyclePhaseRunning) {
		t.Fatalf("expected Tamoss lifecycle Running, got %#v", updatedTamoss.Status.Lifecycle)
	}
	if updatedTamoss.Status.Lifecycle.ActiveOperationRef != nil {
		t.Fatalf("expected active resume operation to be cleared, got %#v", updatedTamoss.Status.Lifecycle.ActiveOperationRef)
	}
	if updatedTamoss.Status.Lifecycle.LastHibernateRef == nil || updatedTamoss.Status.Lifecycle.LastResumeRef == nil {
		t.Fatalf("expected lifecycle history refs, got %#v", updatedTamoss.Status.Lifecycle)
	}
}

func TestTamossResumeDeletesArtifactAfterResumeWhenRetentionRequiresIt(t *testing.T) {
	ctx := context.Background()
	scheme := hibernateTestScheme(t)
	tamoss := hibernateTamossFixture()
	tamoss.Status.Lifecycle = tamossv1alpha1.TamossLifecycleStatus{
		Phase: string(tamossv1alpha1.TamossLifecyclePhaseHibernated),
	}
	destination := hibernateDestinationFixture()
	destination.Spec.Hibernate.Retention.Mode = tamossv1alpha1.HibernationRetentionModeDeleteAfterResume
	resume := resumeFixture()
	cluster := hibernateClusterFixture(tamoss)
	cluster.Annotations = resumeOperationAnnotations(resume)
	cluster.Status.Conditions = []metav1.Condition{{
		Type:   string(cnpgv1.ConditionClusterReady),
		Status: metav1.ConditionTrue,
		Reason: "ClusterIsReady",
	}}
	reader := &fakeHibernationManifestReader{manifest: resumeManifestFixture()}
	cleaner := &fakeHibernationArtifactCleaner{objectsDeleted: 7}

	reconciler := TamossResumeReconciler{
		Client: fake.NewClientBuilder().WithInterceptorFuncs(fakeApplyInterceptor()).
			WithScheme(scheme).
			WithStatusSubresource(&tamossv1alpha1.Tamoss{}, &tamossv1alpha1.TamossResume{}, &cnpgv1.Cluster{}).
			WithObjects(tamoss, destination, resume, cluster).
			Build(),
		Scheme:          scheme,
		ManifestReader:  reader,
		ArtifactCleaner: cleaner,
	}

	request := ctrl.Request{NamespacedName: types.NamespacedName{Name: resume.Name, Namespace: resume.Namespace}}
	result, err := reconciler.Reconcile(ctx, request)
	if err != nil {
		t.Fatalf("expected resume reconcile to start services, got error %v", err)
	}
	markTamossResumeServicesReady(t, ctx, &reconciler, tamoss.Name)
	result, err = reconciler.Reconcile(ctx, request)
	if err != nil {
		t.Fatalf("expected ready services to complete Resume, got error %v", err)
	}
	if result.RequeueAfter <= 0 {
		t.Fatalf("expected cleanup requeue after marking resume completed, got %#v", result)
	}
	if cleaner.calls != 0 {
		t.Fatalf("expected cleanup to wait until resume status is completed, got %d calls", cleaner.calls)
	}
	if _, err := reconciler.Reconcile(ctx, request); err != nil {
		t.Fatalf("expected completed resume cleanup reconcile without error, got %v", err)
	}
	if cleaner.calls != 1 || cleaner.namespace != "media" || cleaner.prefix != "hibernate/example/snap-1/" {
		t.Fatalf("expected one cleanup call for artifact prefix, got %#v", cleaner)
	}
	if result, err := reconciler.Reconcile(ctx, request); err != nil || result.RequeueAfter != 0 || cleaner.calls != 1 {
		t.Fatalf("expected completed cleanup to stay idempotent, got result %#v, err %v, %d cleaner calls", result, err, cleaner.calls)
	}

	updatedResume := &tamossv1alpha1.TamossResume{}
	if err := reconciler.Client.Get(ctx, types.NamespacedName{Name: resume.Name, Namespace: resume.Namespace}, updatedResume); err != nil {
		t.Fatalf("get updated TamossResume: %v", err)
	}
	if updatedResume.Status.Phase != string(tamossv1alpha1.TamossOperationPhaseCompleted) {
		t.Fatalf("expected resume status Completed, got %#v", updatedResume.Status)
	}
	cleanup := updatedResume.Status.Artifact.Cleanup
	if cleanup.Phase != string(tamossv1alpha1.HibernationArtifactCleanupPhaseCompleted) ||
		cleanup.Reason != operatorstatus.ReasonArtifactCleanupComplete ||
		cleanup.ObjectsDeleted != 7 ||
		cleanup.CompletedAt == nil {
		t.Fatalf("expected completed artifact cleanup status, got %#v", cleanup)
	}
}

func TestTamossResumeCleanupFailureDoesNotFailResume(t *testing.T) {
	ctx := context.Background()
	scheme := hibernateTestScheme(t)
	tamoss := hibernateTamossFixture()
	tamoss.Status.Lifecycle = tamossv1alpha1.TamossLifecycleStatus{
		Phase: string(tamossv1alpha1.TamossLifecyclePhaseHibernated),
	}
	destination := hibernateDestinationFixture()
	destination.Spec.Hibernate.Retention.Mode = tamossv1alpha1.HibernationRetentionModeDeleteAfterResume
	resume := resumeFixture()
	cluster := hibernateClusterFixture(tamoss)
	cluster.Annotations = resumeOperationAnnotations(resume)
	cluster.Status.Conditions = []metav1.Condition{{
		Type:   string(cnpgv1.ConditionClusterReady),
		Status: metav1.ConditionTrue,
		Reason: "ClusterIsReady",
	}}
	reader := &fakeHibernationManifestReader{manifest: resumeManifestFixture()}
	cleaner := &fakeHibernationArtifactCleaner{err: fmt.Errorf("access denied")}

	reconciler := TamossResumeReconciler{
		Client: fake.NewClientBuilder().WithInterceptorFuncs(fakeApplyInterceptor()).
			WithScheme(scheme).
			WithStatusSubresource(&tamossv1alpha1.Tamoss{}, &tamossv1alpha1.TamossResume{}, &cnpgv1.Cluster{}).
			WithObjects(tamoss, destination, resume, cluster).
			Build(),
		Scheme:          scheme,
		ManifestReader:  reader,
		ArtifactCleaner: cleaner,
	}

	request := ctrl.Request{NamespacedName: types.NamespacedName{Name: resume.Name, Namespace: resume.Namespace}}
	_, err := reconciler.Reconcile(ctx, request)
	if err != nil {
		t.Fatalf("expected resume reconcile to start services, got error %v", err)
	}
	markTamossResumeServicesReady(t, ctx, &reconciler, tamoss.Name)
	if _, err := reconciler.Reconcile(ctx, request); err != nil {
		t.Fatalf("expected ready services to complete Resume, got error %v", err)
	}
	if cleaner.calls != 0 {
		t.Fatalf("expected cleanup to wait until resume status is completed, got %d calls", cleaner.calls)
	}
	result, err := reconciler.Reconcile(ctx, request)
	if err != nil {
		t.Fatalf("expected cleanup failure to be recorded without reconcile error, got %v", err)
	}
	if result.RequeueAfter != hibernationCleanupRetryInterval {
		t.Fatalf("expected cleanup retry requeue, got %#v", result)
	}

	updatedResume := &tamossv1alpha1.TamossResume{}
	if err := reconciler.Client.Get(ctx, types.NamespacedName{Name: resume.Name, Namespace: resume.Namespace}, updatedResume); err != nil {
		t.Fatalf("get updated TamossResume: %v", err)
	}
	if updatedResume.Status.Phase != string(tamossv1alpha1.TamossOperationPhaseCompleted) {
		t.Fatalf("expected resume status Completed, got %#v", updatedResume.Status)
	}
	cleanup := updatedResume.Status.Artifact.Cleanup
	if cleanup.Phase != string(tamossv1alpha1.HibernationArtifactCleanupPhasePending) ||
		cleanup.Reason != operatorstatus.ReasonArtifactCleanupRetrying ||
		!strings.Contains(cleanup.Message, "access denied") {
		t.Fatalf("expected retrying artifact cleanup status, got %#v", cleanup)
	}

	cleaner.err = nil
	cleaner.objectsDeleted = 5
	if _, err := reconciler.Reconcile(ctx, request); err != nil {
		t.Fatalf("expected cleanup retry to succeed, got %v", err)
	}
	if cleaner.calls != 2 {
		t.Fatalf("expected a second cleanup attempt, got %d calls", cleaner.calls)
	}
	if err := reconciler.Client.Get(ctx, types.NamespacedName{Name: resume.Name, Namespace: resume.Namespace}, updatedResume); err != nil {
		t.Fatalf("get updated TamossResume: %v", err)
	}
	if updatedResume.Status.Artifact.Cleanup.Phase != string(tamossv1alpha1.HibernationArtifactCleanupPhaseCompleted) ||
		updatedResume.Status.Artifact.Cleanup.ObjectsDeleted != 5 {
		t.Fatalf("expected cleanup to complete after retry, got %#v", updatedResume.Status.Artifact.Cleanup)
	}
}

func TestTamossResumeSchedulesTTLCleanupAfterCompletion(t *testing.T) {
	ctx := context.Background()
	scheme := hibernateTestScheme(t)
	tamoss := hibernateTamossFixture()
	tamoss.Status.Lifecycle = tamossv1alpha1.TamossLifecycleStatus{
		Phase: string(tamossv1alpha1.TamossLifecyclePhaseHibernated),
	}
	destination := hibernateDestinationFixture()
	destination.Spec.Hibernate.Retention.Mode = tamossv1alpha1.HibernationRetentionModeTTL
	destination.Spec.Hibernate.Retention.TTLSecondsAfterResume = 3600
	resume := resumeFixture()
	cluster := hibernateClusterFixture(tamoss)
	cluster.Annotations = resumeOperationAnnotations(resume)
	cluster.Status.Conditions = []metav1.Condition{{
		Type:   string(cnpgv1.ConditionClusterReady),
		Status: metav1.ConditionTrue,
		Reason: "ClusterIsReady",
	}}
	reader := &fakeHibernationManifestReader{manifest: resumeManifestFixture()}
	cleaner := &fakeHibernationArtifactCleaner{}

	reconciler := TamossResumeReconciler{
		Client: fake.NewClientBuilder().WithInterceptorFuncs(fakeApplyInterceptor()).
			WithScheme(scheme).
			WithStatusSubresource(&tamossv1alpha1.Tamoss{}, &tamossv1alpha1.TamossResume{}, &cnpgv1.Cluster{}).
			WithObjects(tamoss, destination, resume, cluster).
			Build(),
		Scheme:          scheme,
		ManifestReader:  reader,
		ArtifactCleaner: cleaner,
	}

	request := ctrl.Request{NamespacedName: types.NamespacedName{Name: resume.Name, Namespace: resume.Namespace}}
	result, err := reconciler.Reconcile(ctx, request)
	if err != nil {
		t.Fatalf("expected resume reconcile to start services, got error %v", err)
	}
	markTamossResumeServicesReady(t, ctx, &reconciler, tamoss.Name)
	result, err = reconciler.Reconcile(ctx, request)
	if err != nil {
		t.Fatalf("expected ready services to complete Resume, got error %v", err)
	}
	if result.RequeueAfter <= 0 || result.RequeueAfter > time.Hour {
		t.Fatalf("expected TTL cleanup requeue within an hour, got %#v", result)
	}
	if cleaner.calls != 0 {
		t.Fatalf("expected cleanup to wait for TTL, got %d calls", cleaner.calls)
	}

	updatedResume := &tamossv1alpha1.TamossResume{}
	if err := reconciler.Client.Get(ctx, types.NamespacedName{Name: resume.Name, Namespace: resume.Namespace}, updatedResume); err != nil {
		t.Fatalf("get updated TamossResume: %v", err)
	}
	if updatedResume.Status.Artifact.Cleanup.Phase != string(tamossv1alpha1.HibernationArtifactCleanupPhasePending) ||
		updatedResume.Status.Artifact.Cleanup.Reason != operatorstatus.ReasonArtifactCleanupPending {
		t.Fatalf("expected pending TTL cleanup status, got %#v", updatedResume.Status.Artifact.Cleanup)
	}
}

func TestCompletedTamossResumeRunsDueTTLCleanup(t *testing.T) {
	ctx := context.Background()
	scheme := hibernateTestScheme(t)
	destination := hibernateDestinationFixture()
	destination.Spec.Hibernate.Retention.Mode = tamossv1alpha1.HibernationRetentionModeTTL
	destination.Spec.Hibernate.Retention.TTLSecondsAfterResume = 1
	resume := resumeFixture()
	completedAt := metav1.NewTime(time.Now().Add(-time.Hour))
	resume.Status.Phase = string(tamossv1alpha1.TamossOperationPhaseCompleted)
	resume.Status.Reason = operatorstatus.ReasonTamossReady
	resume.Status.Message = "TAMOSS resume completed; normal reconciliation can continue"
	resume.Status.CompletedAt = &completedAt
	resume.Status.Artifact = tamossv1alpha1.HibernationArtifactStatus{
		ManifestKey: "hibernate/example/snap-1/manifest.json",
		Cleanup: tamossv1alpha1.HibernationArtifactCleanupStatus{
			Phase:  string(tamossv1alpha1.HibernationArtifactCleanupPhasePending),
			Reason: operatorstatus.ReasonArtifactCleanupPending,
		},
	}
	cleaner := &fakeHibernationArtifactCleaner{objectsDeleted: 3}

	reconciler := TamossResumeReconciler{
		Client: fake.NewClientBuilder().WithInterceptorFuncs(fakeApplyInterceptor()).
			WithScheme(scheme).
			WithStatusSubresource(&tamossv1alpha1.TamossResume{}).
			WithObjects(destination, resume).
			Build(),
		Scheme:          scheme,
		ArtifactCleaner: cleaner,
	}

	_, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: resume.Name, Namespace: resume.Namespace}})
	if err != nil {
		t.Fatalf("expected completed resume retention reconcile without error, got %v", err)
	}
	if cleaner.calls != 1 || cleaner.prefix != "hibernate/example/snap-1/" {
		t.Fatalf("expected cleanup call for due TTL artifact, got %#v", cleaner)
	}

	updatedResume := &tamossv1alpha1.TamossResume{}
	if err := reconciler.Client.Get(ctx, types.NamespacedName{Name: resume.Name, Namespace: resume.Namespace}, updatedResume); err != nil {
		t.Fatalf("get updated TamossResume: %v", err)
	}
	if updatedResume.Status.Artifact.Cleanup.Phase != string(tamossv1alpha1.HibernationArtifactCleanupPhaseCompleted) ||
		updatedResume.Status.Artifact.Cleanup.ObjectsDeleted != 3 {
		t.Fatalf("expected completed TTL cleanup status, got %#v", updatedResume.Status.Artifact.Cleanup)
	}
}

func TestHibernationArtifactRootPrefixRejectsUnsafeKeys(t *testing.T) {
	tests := []string{
		"",
		"/hibernate/example/snap-1/manifest.json",
		"hibernate/example/../snap-1/manifest.json",
		"hibernate/example/snap-1/object.json",
		"manifest.json",
	}
	for _, test := range tests {
		t.Run(test, func(t *testing.T) {
			if _, err := hibernationArtifactRootPrefix(test); err == nil {
				t.Fatalf("expected unsafe key %q to be rejected", test)
			}
		})
	}
}

func TestTamossReconcilePreservesResumeRecoveryCluster(t *testing.T) {
	ctx := context.Background()
	scheme := hibernateTestScheme(t)
	tamoss := hibernateTamossFixture()
	cluster := hibernateClusterFixture(tamoss)
	cluster.Annotations = map[string]string{
		tamossResumeOperationAnnotation:    "resume-1",
		tamossResumeOperationUIDAnnotation: "resume-uid",
	}
	cluster.Spec.Bootstrap = &cnpgv1.BootstrapConfiguration{
		Recovery: &cnpgv1.BootstrapRecovery{
			Source: "source-db",
		},
	}
	cluster.Spec.ExternalClusters = []cnpgv1.ExternalCluster{{
		Name: "source-db",
		BarmanObjectStore: &cnpgv1.BarmanObjectStoreConfiguration{
			DestinationPath: "s3://archive/hibernate/example/snap-1/cnpg",
			ServerName:      "source-db",
		},
	}}

	reconciler := TamossReconciler{
		Client: fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(cluster).
			Build(),
	}
	mutator, err := reconciler.preserveResumeRecoveryMutator(ctx, tamoss)
	if err != nil {
		t.Fatalf("expected preserve mutator without error, got %v", err)
	}
	if mutator == nil {
		t.Fatalf("expected preserve mutator for resume-created CNPG Cluster")
	}

	desired := cnpgbackend.BuildCluster(tamoss)
	if desired.Spec.Bootstrap == nil || desired.Spec.Bootstrap.InitDB == nil {
		t.Fatalf("expected normal desired cluster to use initdb before mutation, got %#v", desired.Spec.Bootstrap)
	}
	if err := mutator(desired); err != nil {
		t.Fatalf("preserve recovery mutator failed: %v", err)
	}

	if desired.Spec.Bootstrap == nil || desired.Spec.Bootstrap.Recovery == nil || desired.Spec.Bootstrap.Recovery.Source != "source-db" {
		t.Fatalf("expected recovery bootstrap to be preserved, got %#v", desired.Spec.Bootstrap)
	}
	if len(desired.Spec.ExternalClusters) != 1 ||
		desired.Spec.ExternalClusters[0].BarmanObjectStore == nil ||
		desired.Spec.ExternalClusters[0].BarmanObjectStore.DestinationPath != "s3://archive/hibernate/example/snap-1/cnpg" {
		t.Fatalf("expected recovery external cluster to be preserved, got %#v", desired.Spec.ExternalClusters)
	}
	if desired.Annotations[tamossResumeOperationAnnotation] != "resume-1" {
		t.Fatalf("expected resume annotation to be preserved, got %#v", desired.Annotations)
	}
	if desired.Annotations[tamossResumeOperationUIDAnnotation] != "resume-uid" {
		t.Fatalf("expected resume UID annotation to be preserved, got %#v", desired.Annotations)
	}
}

func TestTamossResumeFailsWhenTargetCNPGClusterAlreadyExists(t *testing.T) {
	ctx := context.Background()
	scheme := hibernateTestScheme(t)
	tamoss := hibernateTamossFixture()
	tamoss.Spec.Paused = true
	destination := hibernateDestinationFixture()
	resume := resumeFixture()
	cluster := hibernateClusterFixture(tamoss)
	reader := &fakeHibernationManifestReader{manifest: resumeManifestFixture()}

	reconciler := TamossResumeReconciler{
		Client: fake.NewClientBuilder().WithInterceptorFuncs(fakeApplyInterceptor()).
			WithScheme(scheme).
			WithStatusSubresource(&tamossv1alpha1.Tamoss{}, &tamossv1alpha1.TamossResume{}, &cnpgv1.Cluster{}).
			WithObjects(tamoss, destination, resume, cluster).
			Build(),
		Scheme:         scheme,
		ManifestReader: reader,
	}

	_, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: resume.Name, Namespace: resume.Namespace}})
	if err != nil {
		t.Fatalf("expected resume target conflict to update status without reconcile error, got %v", err)
	}

	updatedResume := &tamossv1alpha1.TamossResume{}
	if err := reconciler.Client.Get(ctx, types.NamespacedName{Name: resume.Name, Namespace: resume.Namespace}, updatedResume); err != nil {
		t.Fatalf("get updated TamossResume: %v", err)
	}
	if updatedResume.Status.Phase != string(tamossv1alpha1.TamossOperationPhaseFailed) ||
		updatedResume.Status.Reason != operatorstatus.ReasonLifecycleOperationConflict {
		t.Fatalf("expected lifecycle conflict failure, got %#v", updatedResume.Status)
	}

	updatedTamoss := &tamossv1alpha1.Tamoss{}
	if err := reconciler.Client.Get(ctx, types.NamespacedName{Name: tamoss.Name, Namespace: tamoss.Namespace}, updatedTamoss); err != nil {
		t.Fatalf("get updated Tamoss: %v", err)
	}
	if updatedTamoss.Status.Lifecycle.Phase != string(tamossv1alpha1.TamossLifecyclePhaseFailed) {
		t.Fatalf("expected Tamoss lifecycle Failed, got %#v", updatedTamoss.Status.Lifecycle)
	}
}

func TestTamossResumeRejectsClusterFromRecreatedOperation(t *testing.T) {
	ctx := context.Background()
	scheme := hibernateTestScheme(t)
	tamoss := hibernateTamossFixture()
	tamoss.Spec.Paused = true
	destination := hibernateDestinationFixture()
	resume := resumeFixture()
	cluster := hibernateClusterFixture(tamoss)
	cluster.Annotations = map[string]string{
		tamossResumeOperationAnnotation:    resume.Name,
		tamossResumeOperationUIDAnnotation: "previous-resume-uid",
	}
	reader := &fakeHibernationManifestReader{manifest: resumeManifestFixture()}

	reconciler := TamossResumeReconciler{
		Client: fake.NewClientBuilder().WithInterceptorFuncs(fakeApplyInterceptor()).
			WithScheme(scheme).
			WithStatusSubresource(&tamossv1alpha1.Tamoss{}, &tamossv1alpha1.TamossResume{}, &cnpgv1.Cluster{}).
			WithObjects(tamoss, destination, resume, cluster).
			Build(),
		Scheme:         scheme,
		ManifestReader: reader,
	}

	if _, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: resume.Name, Namespace: resume.Namespace}}); err != nil {
		t.Fatalf("expected stale resume cluster to fail without reconcile error, got %v", err)
	}

	updated := &tamossv1alpha1.TamossResume{}
	if err := reconciler.Client.Get(ctx, types.NamespacedName{Name: resume.Name, Namespace: resume.Namespace}, updated); err != nil {
		t.Fatalf("get updated TamossResume: %v", err)
	}
	if updated.Status.Phase != string(tamossv1alpha1.TamossOperationPhaseFailed) ||
		updated.Status.Reason != operatorstatus.ReasonLifecycleOperationConflict {
		t.Fatalf("expected recreated operation conflict, got %#v", updated.Status)
	}
}

func TestTamossResumeDeleteMarksActiveLifecycleFailed(t *testing.T) {
	ctx := context.Background()
	scheme := hibernateTestScheme(t)
	tamoss := hibernateTamossFixture()
	resume := resumeFixture()
	now := metav1.Now()
	resume.DeletionTimestamp = &now
	resume.Status.Phase = string(tamossv1alpha1.TamossOperationPhaseRecoveringDatabase)
	tamoss.Status.Lifecycle = tamossv1alpha1.TamossLifecycleStatus{
		Phase:              string(tamossv1alpha1.TamossLifecyclePhaseResuming),
		Reason:             operatorstatus.ReasonTamossResuming,
		LastHibernateRef:   operationObjectReference(hibernateFixture(), "TamossHibernate"),
		ActiveOperationRef: operationObjectReference(resume, "TamossResume"),
	}
	cluster := hibernateClusterFixture(tamoss)
	cluster.Annotations = resumeOperationAnnotations(resume)

	reconciler := TamossResumeReconciler{
		Client: fake.NewClientBuilder().WithInterceptorFuncs(fakeApplyInterceptor()).
			WithScheme(scheme).
			WithStatusSubresource(&tamossv1alpha1.Tamoss{}, &tamossv1alpha1.TamossResume{}, &cnpgv1.Cluster{}).
			WithObjects(tamoss, resume, cluster).
			Build(),
		Scheme: scheme,
	}

	result, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: resume.Name, Namespace: resume.Namespace}})
	if err != nil {
		t.Fatalf("expected resume finalization to start recovery cleanup without error, got %v", err)
	}
	if result.RequeueAfter <= 0 {
		t.Fatalf("expected aborted recovery cleanup to poll for cluster deletion, got %#v", result)
	}
	if err := reconciler.Client.Get(ctx, types.NamespacedName{Name: cluster.Name, Namespace: cluster.Namespace}, &cnpgv1.Cluster{}); !apierrors.IsNotFound(err) {
		t.Fatalf("expected aborted recovery CNPG Cluster to be removed, got %v", err)
	}
	updatedResume := &tamossv1alpha1.TamossResume{}
	if err := reconciler.Client.Get(ctx, types.NamespacedName{Name: resume.Name, Namespace: resume.Namespace}, updatedResume); err != nil {
		t.Fatalf("get Resume while recovery cleanup is checkpointed: %v", err)
	}
	if !hasFinalizer(updatedResume.Finalizers, tamossResumeFinalizer) {
		t.Fatalf("expected finalizer to remain until recovery cleanup is observed, got %#v", updatedResume.Finalizers)
	}
	if _, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: resume.Name, Namespace: resume.Namespace}}); err != nil {
		t.Fatalf("expected resume finalization after recovery cleanup without error, got %v", err)
	}

	updatedTamoss := &tamossv1alpha1.Tamoss{}
	if err := reconciler.Client.Get(ctx, types.NamespacedName{Name: tamoss.Name, Namespace: tamoss.Namespace}, updatedTamoss); err != nil {
		t.Fatalf("get updated Tamoss: %v", err)
	}
	if updatedTamoss.Status.Lifecycle.Phase != string(tamossv1alpha1.TamossLifecyclePhaseFailed) ||
		updatedTamoss.Status.Lifecycle.Reason != operatorstatus.ReasonLifecycleOperationDeleted {
		t.Fatalf("expected deleted resume lifecycle failure, got %#v", updatedTamoss.Status.Lifecycle)
	}

	updatedResume = &tamossv1alpha1.TamossResume{}
	if err := reconciler.Client.Get(ctx, types.NamespacedName{Name: resume.Name, Namespace: resume.Namespace}, updatedResume); err != nil {
		if apierrors.IsNotFound(err) {
			return
		}
		t.Fatalf("get updated TamossResume: %v", err)
	} else if hasFinalizer(updatedResume.Finalizers, tamossResumeFinalizer) {
		t.Fatalf("expected resume finalizer removed, got %#v", updatedResume.Finalizers)
	}
}

func TestTamossResumeRejectsChecksumMismatchFromHibernationRef(t *testing.T) {
	ctx := context.Background()
	scheme := hibernateTestScheme(t)
	tamoss := hibernateTamossFixture()
	destination := hibernateDestinationFixture()
	hibernate := hibernateFixture()
	hibernate.Status.Phase = string(tamossv1alpha1.TamossOperationPhaseCompleted)
	hibernate.Status.Artifact = tamossv1alpha1.HibernationArtifactStatus{
		Driver:      string(tamossv1alpha1.HibernationDriverCNPGPhysical),
		ManifestKey: "hibernate/example/snap-1/manifest.json",
		Checksum:    "sha256:expected",
	}
	resume := resumeFixture()
	resume.Spec.Source = tamossv1alpha1.TamossResumeSource{
		HibernationRef: &tamossv1alpha1.LocalObjectReference{Name: hibernate.Name},
	}
	reader := &fakeHibernationManifestReader{
		manifest: resumeManifestFixture(),
		checksum: "sha256:actual",
	}

	reconciler := TamossResumeReconciler{
		Client: fake.NewClientBuilder().WithInterceptorFuncs(fakeApplyInterceptor()).
			WithScheme(scheme).
			WithStatusSubresource(&tamossv1alpha1.Tamoss{}, &tamossv1alpha1.TamossResume{}).
			WithObjects(tamoss, destination, hibernate, resume).
			Build(),
		Scheme:         scheme,
		ManifestReader: reader,
	}

	_, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: resume.Name, Namespace: resume.Namespace}})
	if err != nil {
		t.Fatalf("expected checksum mismatch to update status without reconcile error, got %v", err)
	}

	updatedResume := &tamossv1alpha1.TamossResume{}
	if getErr := reconciler.Client.Get(ctx, types.NamespacedName{Name: resume.Name, Namespace: resume.Namespace}, updatedResume); getErr != nil {
		t.Fatalf("get updated TamossResume: %v", getErr)
	}
	if updatedResume.Status.Phase != string(tamossv1alpha1.TamossOperationPhaseFailed) ||
		updatedResume.Status.Reason != operatorstatus.ReasonHibernateManifestChecksumMismatch {
		t.Fatalf("expected checksum mismatch failure, got %#v", updatedResume.Status)
	}
}

func TestValidateResumeManifest(t *testing.T) {
	manifestKey := "hibernate/example/snap-1/manifest.json"
	tests := []struct {
		name    string
		mutate  func(manifest *hibernationManifest)
		wantErr string
	}{
		{
			name:   "valid manifest",
			mutate: func(*hibernationManifest) {},
		},
		{
			name:    "missing manifest kind",
			mutate:  func(m *hibernationManifest) { m.Schema.ManifestKind = "" },
			wantErr: "schema kind",
		},
		{
			name:    "missing schema version",
			mutate:  func(m *hibernationManifest) { m.Schema.Version = "" },
			wantErr: "schema.version is required",
		},
		{
			name:    "unsupported schema version",
			mutate:  func(m *hibernationManifest) { m.Schema.Version = "future" },
			wantErr: "schema version \"future\" is not supported",
		},
		{
			name:    "unsupported TAMS API version",
			mutate:  func(m *hibernationManifest) { m.Schema.TAMSAPI = "9.0" },
			wantErr: "TAMS API version \"9.0\" is not supported",
		},
		{
			name:    "unsupported driver",
			mutate:  func(m *hibernationManifest) { m.Driver = string(tamossv1alpha1.HibernationDriverLogicalDump) },
			wantErr: "is not supported for resume",
		},
		{
			name:    "manifest key mismatch",
			mutate:  func(m *hibernationManifest) { m.Artifact.ManifestKey = "hibernate/other/manifest.json" },
			wantErr: "does not match requested key",
		},
		{
			name:    "missing destination path",
			mutate:  func(m *hibernationManifest) { m.CNPG.DestinationPath = "" },
			wantErr: "missing cnpg.destinationPath",
		},
		{
			name: "missing server name",
			mutate: func(m *hibernationManifest) {
				m.CNPG.ServerName = ""
				m.Database.Cluster = ""
			},
			wantErr: "missing CNPG source server name",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			manifest := resumeManifestFixture()
			test.mutate(&manifest)
			err := validateResumeManifest(manifest, manifestKey)
			if test.wantErr == "" {
				if err != nil {
					t.Fatalf("expected valid manifest, got %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("expected error containing %q, got %v", test.wantErr, err)
			}
		})
	}
}

func TestTamossResumeRejectsUnsupportedSchemaBeforeCreatingCluster(t *testing.T) {
	ctx := context.Background()
	scheme := hibernateTestScheme(t)
	tamoss := hibernateTamossFixture()
	destination := hibernateDestinationFixture()
	resume := resumeFixture()
	manifest := resumeManifestFixture()
	manifest.Schema.Version = "future"
	reader := &fakeHibernationManifestReader{manifest: manifest}

	reconciler := TamossResumeReconciler{
		Client: fake.NewClientBuilder().WithInterceptorFuncs(fakeApplyInterceptor()).
			WithScheme(scheme).
			WithStatusSubresource(&tamossv1alpha1.Tamoss{}, &tamossv1alpha1.TamossResume{}, &cnpgv1.Cluster{}).
			WithObjects(tamoss, destination, resume).
			Build(),
		Scheme:         scheme,
		ManifestReader: reader,
	}

	if _, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: resume.Name, Namespace: resume.Namespace}}); err != nil {
		t.Fatalf("expected unsupported schema to update status without reconcile error, got %v", err)
	}

	updated := &tamossv1alpha1.TamossResume{}
	if err := reconciler.Client.Get(ctx, types.NamespacedName{Name: resume.Name, Namespace: resume.Namespace}, updated); err != nil {
		t.Fatalf("get updated TamossResume: %v", err)
	}
	if updated.Status.Phase != string(tamossv1alpha1.TamossOperationPhaseFailed) ||
		updated.Status.Reason != operatorstatus.ReasonUnsupportedSchemaVersion {
		t.Fatalf("expected unsupported schema failure, got %#v", updated.Status)
	}
	cluster := &cnpgv1.Cluster{}
	key := types.NamespacedName{Name: tamoss.ResourceName("db"), Namespace: tamoss.Namespace}
	if err := reconciler.Client.Get(ctx, key, cluster); !apierrors.IsNotFound(err) {
		t.Fatalf("expected no recovery cluster for unsupported schema, got %v", err)
	}
}

func TestTamossResumeManifestReadFailureHandling(t *testing.T) {
	tests := []struct {
		name        string
		readErr     error
		wantPhase   tamossv1alpha1.TamossOperationPhase
		wantReason  string
		wantRequeue bool
	}{
		{
			name:        "transient transport error retries",
			readErr:     fmt.Errorf("connection refused"),
			wantPhase:   tamossv1alpha1.TamossOperationPhaseResolvingSource,
			wantReason:  operatorstatus.ReasonHibernateManifestUnavailable,
			wantRequeue: true,
		},
		{
			name:       "missing object fails terminally",
			readErr:    minio.ErrorResponse{Code: "NoSuchKey", Message: "key does not exist"},
			wantPhase:  tamossv1alpha1.TamossOperationPhaseFailed,
			wantReason: operatorstatus.ReasonHibernateSourceInvalid,
		},
		{
			name:       "checksum mismatch fails terminally",
			readErr:    fmt.Errorf("read manifest: %w", errHibernationManifestChecksumMismatch),
			wantPhase:  tamossv1alpha1.TamossOperationPhaseFailed,
			wantReason: operatorstatus.ReasonHibernateManifestChecksumMismatch,
		},
		{
			name:       "corrupt manifest fails terminally",
			readErr:    fmt.Errorf("%w: parse hibernation manifest", errHibernationManifestInvalid),
			wantPhase:  tamossv1alpha1.TamossOperationPhaseFailed,
			wantReason: operatorstatus.ReasonHibernateSourceInvalid,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			scheme := hibernateTestScheme(t)
			tamoss := hibernateTamossFixture()
			destination := hibernateDestinationFixture()
			resume := resumeFixture()
			reader := &fakeHibernationManifestReader{err: test.readErr}

			reconciler := TamossResumeReconciler{
				Client: fake.NewClientBuilder().WithInterceptorFuncs(fakeApplyInterceptor()).
					WithScheme(scheme).
					WithStatusSubresource(&tamossv1alpha1.Tamoss{}, &tamossv1alpha1.TamossResume{}).
					WithObjects(tamoss, destination, resume).
					Build(),
				Scheme:         scheme,
				ManifestReader: reader,
				PollInterval:   time.Second,
			}

			result, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: resume.Name, Namespace: resume.Namespace}})
			if err != nil {
				t.Fatalf("expected read failure to update status without error, got %v", err)
			}
			if test.wantRequeue && result.RequeueAfter <= 0 {
				t.Fatalf("expected retry requeue, got %#v", result)
			}
			if !test.wantRequeue && result.RequeueAfter != 0 {
				t.Fatalf("expected terminal failure without requeue, got %#v", result)
			}

			updated := &tamossv1alpha1.TamossResume{}
			if err := reconciler.Client.Get(ctx, types.NamespacedName{Name: resume.Name, Namespace: resume.Namespace}, updated); err != nil {
				t.Fatalf("get updated TamossResume: %v", err)
			}
			if updated.Status.Phase != string(test.wantPhase) || updated.Status.Reason != test.wantReason {
				t.Fatalf("expected phase %s reason %s, got %#v", test.wantPhase, test.wantReason, updated.Status)
			}
		})
	}
}

func TestTamossResumeTerminalFailedRemainsIdempotent(t *testing.T) {
	ctx := context.Background()
	scheme := hibernateTestScheme(t)
	resume := resumeFixture()
	resume.Status.Phase = string(tamossv1alpha1.TamossOperationPhaseFailed)
	reader := &fakeHibernationManifestReader{manifest: resumeManifestFixture()}

	reconciler := TamossResumeReconciler{
		Client: fake.NewClientBuilder().WithInterceptorFuncs(fakeApplyInterceptor()).
			WithScheme(scheme).
			WithStatusSubresource(&tamossv1alpha1.TamossResume{}).
			WithObjects(resume).
			Build(),
		Scheme:         scheme,
		ManifestReader: reader,
	}

	result, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: resume.Name, Namespace: resume.Namespace}})
	if err != nil || result.RequeueAfter != 0 {
		t.Fatalf("expected failed resume to be a no-op, got result %#v, err %v", result, err)
	}
	if reader.calls != 0 {
		t.Fatalf("expected no manifest reads for failed resume, got %d", reader.calls)
	}
}

func TestTamossResumeFailsOnLifecycleConflict(t *testing.T) {
	ctx := context.Background()
	scheme := hibernateTestScheme(t)
	tamoss := hibernateTamossFixture()
	tamoss.Status.Lifecycle = tamossv1alpha1.TamossLifecycleStatus{
		Phase:  string(tamossv1alpha1.TamossLifecyclePhaseHibernating),
		Reason: operatorstatus.ReasonTamossHibernating,
		ActiveOperationRef: &corev1.ObjectReference{
			APIVersion: tamossv1alpha1.GroupVersion.String(),
			Kind:       "TamossHibernate",
			Namespace:  "media",
			Name:       "other-op",
		},
	}
	destination := hibernateDestinationFixture()
	resume := resumeFixture()
	reader := &fakeHibernationManifestReader{manifest: resumeManifestFixture()}

	reconciler := TamossResumeReconciler{
		Client: fake.NewClientBuilder().WithInterceptorFuncs(fakeApplyInterceptor()).
			WithScheme(scheme).
			WithStatusSubresource(&tamossv1alpha1.Tamoss{}, &tamossv1alpha1.TamossResume{}).
			WithObjects(tamoss, destination, resume).
			Build(),
		Scheme:         scheme,
		ManifestReader: reader,
	}

	if _, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: resume.Name, Namespace: resume.Namespace}}); err != nil {
		t.Fatalf("expected conflict to update status without error, got %v", err)
	}

	updatedResume := &tamossv1alpha1.TamossResume{}
	if err := reconciler.Client.Get(ctx, types.NamespacedName{Name: resume.Name, Namespace: resume.Namespace}, updatedResume); err != nil {
		t.Fatalf("get updated TamossResume: %v", err)
	}
	if updatedResume.Status.Phase != string(tamossv1alpha1.TamossOperationPhaseFailed) ||
		updatedResume.Status.Reason != operatorstatus.ReasonLifecycleOperationConflict {
		t.Fatalf("expected lifecycle conflict failure, got %#v", updatedResume.Status)
	}

	updatedTamoss := &tamossv1alpha1.Tamoss{}
	if err := reconciler.Client.Get(ctx, types.NamespacedName{Name: tamoss.Name, Namespace: tamoss.Namespace}, updatedTamoss); err != nil {
		t.Fatalf("get updated Tamoss: %v", err)
	}
	if updatedTamoss.Status.Lifecycle.ActiveOperationRef == nil ||
		updatedTamoss.Status.Lifecycle.ActiveOperationRef.Name != "other-op" {
		t.Fatalf("expected active operation to stay untouched, got %#v", updatedTamoss.Status.Lifecycle.ActiveOperationRef)
	}
}

func TestTamossResumeRejectsAnUnquiescedRunningTarget(t *testing.T) {
	ctx := context.Background()
	scheme := hibernateTestScheme(t)
	tamoss := hibernateTamossFixture()
	tamoss.Status.Lifecycle = tamossv1alpha1.TamossLifecycleStatus{
		Phase:  string(tamossv1alpha1.TamossLifecyclePhaseRunning),
		Reason: operatorstatus.ReasonTamossReady,
	}
	destination := hibernateDestinationFixture()
	resume := resumeFixture()
	reader := &fakeHibernationManifestReader{manifest: resumeManifestFixture()}

	reconciler := TamossResumeReconciler{
		Client: fake.NewClientBuilder().WithInterceptorFuncs(fakeApplyInterceptor()).
			WithScheme(scheme).
			WithStatusSubresource(&tamossv1alpha1.Tamoss{}, &tamossv1alpha1.TamossResume{}).
			WithObjects(tamoss, destination, resume).
			Build(),
		Scheme:         scheme,
		ManifestReader: reader,
	}

	if _, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: resume.Name, Namespace: resume.Namespace}}); err != nil {
		t.Fatalf("expected unsafe target to fail without reconcile error, got %v", err)
	}
	updated := &tamossv1alpha1.TamossResume{}
	if err := reconciler.Client.Get(ctx, types.NamespacedName{Name: resume.Name, Namespace: resume.Namespace}, updated); err != nil {
		t.Fatalf("get failed Resume: %v", err)
	}
	if updated.Status.Phase != string(tamossv1alpha1.TamossOperationPhaseFailed) || updated.Status.Reason != operatorstatus.ReasonTargetNotQuiesced {
		t.Fatalf("expected unquiesced target failure, got %#v", updated.Status)
	}
}

func TestTamossResumeDeleteAfterCompletionKeepsLifecycle(t *testing.T) {
	ctx := context.Background()
	scheme := hibernateTestScheme(t)
	tamoss := hibernateTamossFixture()
	resume := resumeFixture()
	now := metav1.Now()
	resume.DeletionTimestamp = &now
	resume.Status.Phase = string(tamossv1alpha1.TamossOperationPhaseCompleted)
	tamoss.Status.Lifecycle = tamossv1alpha1.TamossLifecycleStatus{
		Phase:         string(tamossv1alpha1.TamossLifecyclePhaseRunning),
		Reason:        operatorstatus.ReasonTamossReady,
		LastResumeRef: operationObjectReference(resume, "TamossResume"),
	}

	reconciler := TamossResumeReconciler{
		Client: fake.NewClientBuilder().WithInterceptorFuncs(fakeApplyInterceptor()).
			WithScheme(scheme).
			WithStatusSubresource(&tamossv1alpha1.Tamoss{}, &tamossv1alpha1.TamossResume{}).
			WithObjects(tamoss, resume).
			Build(),
		Scheme: scheme,
	}

	if _, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: resume.Name, Namespace: resume.Namespace}}); err != nil {
		t.Fatalf("expected completed resume finalization without error, got %v", err)
	}

	updatedTamoss := &tamossv1alpha1.Tamoss{}
	if err := reconciler.Client.Get(ctx, types.NamespacedName{Name: tamoss.Name, Namespace: tamoss.Namespace}, updatedTamoss); err != nil {
		t.Fatalf("get updated Tamoss: %v", err)
	}
	if updatedTamoss.Status.Lifecycle.Phase != string(tamossv1alpha1.TamossLifecyclePhaseRunning) {
		t.Fatalf("expected lifecycle to stay Running after housekeeping delete, got %#v", updatedTamoss.Status.Lifecycle)
	}

	updatedResume := &tamossv1alpha1.TamossResume{}
	if err := reconciler.Client.Get(ctx, types.NamespacedName{Name: resume.Name, Namespace: resume.Namespace}, updatedResume); err != nil {
		if !apierrors.IsNotFound(err) {
			t.Fatalf("get updated TamossResume: %v", err)
		}
	} else if hasFinalizer(updatedResume.Finalizers, tamossResumeFinalizer) {
		t.Fatalf("expected resume finalizer removed, got %#v", updatedResume.Finalizers)
	}
}

func TestTamossResumeResolvesHibernationRefToRecovery(t *testing.T) {
	ctx := context.Background()
	scheme := hibernateTestScheme(t)
	tamoss := hibernateTamossFixture()
	tamoss.Spec.Paused = true
	destination := hibernateDestinationFixture()
	hibernate := hibernateFixture()
	hibernate.Status.Phase = string(tamossv1alpha1.TamossOperationPhaseCompleted)
	hibernate.Status.Artifact = tamossv1alpha1.HibernationArtifactStatus{
		Driver:      string(tamossv1alpha1.HibernationDriverCNPGPhysical),
		ManifestKey: "hibernate/example/snap-1/manifest.json",
		Checksum:    testResumeManifestChecksum,
	}
	resume := resumeFixture()
	resume.Spec.Source = tamossv1alpha1.TamossResumeSource{
		HibernationRef: &tamossv1alpha1.LocalObjectReference{Name: hibernate.Name},
	}
	reader := &fakeHibernationManifestReader{manifest: resumeManifestFixture()}

	reconciler := TamossResumeReconciler{
		Client: fake.NewClientBuilder().WithInterceptorFuncs(fakeApplyInterceptor()).
			WithScheme(scheme).
			WithStatusSubresource(&tamossv1alpha1.Tamoss{}, &tamossv1alpha1.TamossResume{}, &cnpgv1.Cluster{}).
			WithObjects(tamoss, destination, hibernate, resume).
			Build(),
		Scheme:         scheme,
		ManifestReader: reader,
		PollInterval:   time.Second,
	}

	result, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: resume.Name, Namespace: resume.Namespace}})
	if err != nil {
		t.Fatalf("expected hibernationRef resume to apply recovery cluster, got %v", err)
	}
	if result.RequeueAfter != time.Second {
		t.Fatalf("expected poll requeue while recovering, got %#v", result)
	}
	if reader.key != "hibernate/example/snap-1/manifest.json" {
		t.Fatalf("expected manifest key resolved from hibernate status, got %q", reader.key)
	}

	cluster := &cnpgv1.Cluster{}
	if err := reconciler.Client.Get(ctx, types.NamespacedName{Name: tamoss.ResourceName("db"), Namespace: tamoss.Namespace}, cluster); err != nil {
		t.Fatalf("expected recovery CNPG Cluster: %v", err)
	}
	if !resumeOperationMatches(cluster, resume) {
		t.Fatalf("expected resume operation annotation, got %#v", cluster.Annotations)
	}
	if cluster.Spec.Bootstrap == nil || cluster.Spec.Bootstrap.Recovery == nil {
		t.Fatalf("expected recovery bootstrap, got %#v", cluster.Spec.Bootstrap)
	}

	updatedResume := &tamossv1alpha1.TamossResume{}
	if err := reconciler.Client.Get(ctx, types.NamespacedName{Name: resume.Name, Namespace: resume.Namespace}, updatedResume); err != nil {
		t.Fatalf("get updated TamossResume: %v", err)
	}
	if updatedResume.Status.Phase != string(tamossv1alpha1.TamossOperationPhaseRecoveringDatabase) {
		t.Fatalf("expected resume status RecoveringDatabase, got %#v", updatedResume.Status)
	}
}

func TestTamossResumeHibernationRefPhaseHandling(t *testing.T) {
	tests := []struct {
		name        string
		phase       tamossv1alpha1.TamossOperationPhase
		wantPhase   tamossv1alpha1.TamossOperationPhase
		wantRequeue bool
	}{
		{
			name:        "in-progress hibernation waits",
			phase:       tamossv1alpha1.TamossOperationPhaseCapturingDatabase,
			wantPhase:   tamossv1alpha1.TamossOperationPhaseResolvingSource,
			wantRequeue: true,
		},
		{
			name:      "failed hibernation fails the resume",
			phase:     tamossv1alpha1.TamossOperationPhaseFailed,
			wantPhase: tamossv1alpha1.TamossOperationPhaseFailed,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			scheme := hibernateTestScheme(t)
			tamoss := hibernateTamossFixture()
			destination := hibernateDestinationFixture()
			hibernate := hibernateFixture()
			hibernate.Status.Phase = string(test.phase)
			resume := resumeFixture()
			resume.Spec.Source = tamossv1alpha1.TamossResumeSource{
				HibernationRef: &tamossv1alpha1.LocalObjectReference{Name: hibernate.Name},
			}

			reconciler := TamossResumeReconciler{
				Client: fake.NewClientBuilder().WithInterceptorFuncs(fakeApplyInterceptor()).
					WithScheme(scheme).
					WithStatusSubresource(&tamossv1alpha1.Tamoss{}, &tamossv1alpha1.TamossResume{}).
					WithObjects(tamoss, destination, hibernate, resume).
					Build(),
				Scheme:       scheme,
				PollInterval: time.Second,
			}

			result, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: resume.Name, Namespace: resume.Namespace}})
			if err != nil {
				t.Fatalf("expected hibernationRef phase handling without error, got %v", err)
			}
			if test.wantRequeue && result.RequeueAfter <= 0 {
				t.Fatalf("expected wait requeue, got %#v", result)
			}
			if !test.wantRequeue && result.RequeueAfter != 0 {
				t.Fatalf("expected terminal failure without requeue, got %#v", result)
			}

			updated := &tamossv1alpha1.TamossResume{}
			if err := reconciler.Client.Get(ctx, types.NamespacedName{Name: resume.Name, Namespace: resume.Namespace}, updated); err != nil {
				t.Fatalf("get updated TamossResume: %v", err)
			}
			if updated.Status.Phase != string(test.wantPhase) ||
				updated.Status.Reason != operatorstatus.ReasonHibernateSourceInvalid {
				t.Fatalf("expected phase %s with source-invalid reason, got %#v", test.wantPhase, updated.Status)
			}
		})
	}
}

func TestTamossResumeWaitsForMissingTarget(t *testing.T) {
	ctx := context.Background()
	scheme := hibernateTestScheme(t)
	resume := resumeFixture()

	reconciler := TamossResumeReconciler{
		Client: fake.NewClientBuilder().WithInterceptorFuncs(fakeApplyInterceptor()).
			WithScheme(scheme).
			WithStatusSubresource(&tamossv1alpha1.TamossResume{}).
			WithObjects(resume).
			Build(),
		Scheme:       scheme,
		PollInterval: time.Second,
	}

	result, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: resume.Name, Namespace: resume.Namespace}})
	if err != nil {
		t.Fatalf("expected missing target to wait without error, got %v", err)
	}
	if result.RequeueAfter != time.Second {
		t.Fatalf("expected requeue while target Tamoss is missing, got %#v", result)
	}

	updated := &tamossv1alpha1.TamossResume{}
	if err := reconciler.Client.Get(ctx, types.NamespacedName{Name: resume.Name, Namespace: resume.Namespace}, updated); err != nil {
		t.Fatalf("get updated TamossResume: %v", err)
	}
	if updated.Status.Phase != string(tamossv1alpha1.TamossOperationPhasePreparingTarget) ||
		updated.Status.Reason != operatorstatus.ReasonTamossNotFound {
		t.Fatalf("expected waiting status for missing target, got %#v", updated.Status)
	}
}

func resumeFixture() *tamossv1alpha1.TamossResume {
	return &tamossv1alpha1.TamossResume{
		TypeMeta: metav1.TypeMeta{
			APIVersion: tamossv1alpha1.GroupVersion.String(),
			Kind:       "TamossResume",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:       "resume-1",
			Namespace:  "media",
			UID:        "resume-uid",
			Finalizers: []string{tamossResumeFinalizer},
		},
		Spec: tamossv1alpha1.TamossResumeSpec{
			TamossRef: tamossv1alpha1.TamossReferenceSpec{Name: "example"},
			Source: tamossv1alpha1.TamossResumeSource{
				Artifact: &tamossv1alpha1.TamossResumeArtifactSource{
					StorageBackendRef: tamossv1alpha1.LocalObjectReference{Name: "archive"},
					ManifestKey:       "hibernate/example/snap-1/manifest.json",
					Checksum:          testResumeManifestChecksum,
				},
			},
		},
	}
}

func markTamossResumeServicesReady(t *testing.T, ctx context.Context, reconciler *TamossResumeReconciler, name string) {
	t.Helper()
	tamoss := &tamossv1alpha1.Tamoss{}
	key := types.NamespacedName{Name: name, Namespace: "media"}
	if err := reconciler.Client.Get(ctx, key, tamoss); err != nil {
		t.Fatalf("get TAMOSS starting resumed services: %v", err)
	}
	if tamoss.Status.Lifecycle.Phase != string(tamossv1alpha1.TamossLifecyclePhaseRunning) || tamoss.Status.Lifecycle.ActiveOperationRef == nil {
		t.Fatalf("expected Resume to release normal reconciliation while retaining its claim, got %#v", tamoss.Status.Lifecycle)
	}
	tamoss.Status.ObservedGeneration = tamoss.Generation
	operatorstatus.SetConditionBool(
		&tamoss.Status.Conditions,
		tamoss.Generation,
		operatorstatus.ConditionReady,
		true,
		operatorstatus.ReasonAllComponentsReady,
		"All TAMOSS components are ready",
	)
	if err := reconciler.Client.Status().Update(ctx, tamoss); err != nil {
		t.Fatalf("mark resumed TAMOSS ready: %v", err)
	}
}

func resumeManifestFixture() hibernationManifest {
	return hibernationManifest{
		ManifestVersion: hibernationManifestVersion,
		Driver:          string(tamossv1alpha1.HibernationDriverCNPGPhysical),
		Schema: hibernationManifestSchema{
			Version:      schemabundle.SchemaVersion,
			TAMSAPI:      schemabundle.SupportedTAMSAPIVersion,
			Operator:     schemabundle.SchemaVersion,
			ManifestKind: "TamossHibernate",
		},
		SourceTamoss: hibernationManifestTamoss{
			Name:      "example",
			Namespace: "media",
		},
		Database: hibernationManifestDatabase{
			Provider: string(tamossv1alpha1.BackendProvidedByCNPG),
			Cluster:  "source-db",
		},
		Artifact: hibernationManifestArtifact{
			ManifestKey: "hibernate/example/snap-1/manifest.json",
			ManifestURI: "s3://archive/hibernate/example/snap-1/manifest.json",
		},
		CNPG: hibernationManifestCNPG{
			BackupName:      "snap-1",
			BackupID:        "20260707T100000",
			DestinationPath: "s3://archive/hibernate/example/snap-1/cnpg",
			ServerName:      "source-db",
			Phase:           string(cnpgv1.BackupPhaseCompleted),
		},
		StorageBackend: hibernationManifestStorageBackend{
			Name:        "archive",
			Bucket:      "archive",
			EndpointURL: "https://s3.eu-west-2.amazonaws.com",
			Region:      "eu-west-2",
		},
	}
}

type fakeHibernationManifestReader struct {
	calls    int
	key      string
	manifest hibernationManifest
	checksum string
	err      error
}

type fakeHibernationArtifactCleaner struct {
	calls          int
	namespace      string
	spec           tamossv1alpha1.StorageBackendSpec
	prefix         string
	objectsDeleted int64
	err            error
}

func hasFinalizer(finalizers []string, finalizer string) bool {
	for _, value := range finalizers {
		if value == finalizer {
			return true
		}
	}
	return false
}

func (f *fakeHibernationManifestReader) Read(_ context.Context, _ string, _ tamossv1alpha1.StorageBackendSpec, key string) (hibernationManifest, string, error) {
	f.calls++
	f.key = key
	if f.err != nil {
		return hibernationManifest{}, "", f.err
	}
	if f.manifest.ManifestVersion == "" {
		return hibernationManifest{}, "", fmt.Errorf("manifest not configured")
	}
	checksum := f.checksum
	if checksum == "" {
		checksum = testResumeManifestChecksum
	}
	return f.manifest, checksum, nil
}

func (f *fakeHibernationArtifactCleaner) DeletePrefix(_ context.Context, namespace string, spec tamossv1alpha1.StorageBackendSpec, prefix string) (int64, error) {
	f.calls++
	f.namespace = namespace
	f.spec = spec
	f.prefix = prefix
	if f.err != nil {
		return 0, f.err
	}
	return f.objectsDeleted, nil
}
