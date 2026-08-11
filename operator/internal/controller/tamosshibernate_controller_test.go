package controller

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	appsv1 "k8s.io/api/apps/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	tamossv1alpha1 "github.com/livewyer-ops/tamoss/operator/api/v1alpha1"
	schemabundle "github.com/livewyer-ops/tamoss/operator/internal/schema"
	operatorstatus "github.com/livewyer-ops/tamoss/operator/internal/status"
)

func TestTamossHibernateLaunchesCNPGBackupAndGatesLifecycle(t *testing.T) {
	ctx := context.Background()
	scheme := hibernateTestScheme(t)
	tamoss := hibernateTamossFixture()
	destination := hibernateDestinationFixture()
	hibernate := hibernateFixture()
	cluster := hibernateClusterFixture(tamoss)
	api := hibernateDeploymentFixture(tamoss, "api", 2)
	worker := hibernateDeploymentFixture(tamoss, "worker", 1)
	ui := hibernateDeploymentFixture(tamoss, "ui", 1)
	console := hibernateDeploymentFixture(tamoss, "console", 1)
	hpa := hibernateHPAFixture(tamoss, "api")
	scheduled := hibernateScheduledBackupFixture(tamoss)
	pod := hibernatePodFixture(api)
	recorder := record.NewFakeRecorder(4)

	reconciler := TamossHibernateReconciler{
		Client: fake.NewClientBuilder().WithInterceptorFuncs(fakeApplyInterceptor()).
			WithScheme(scheme).
			WithStatusSubresource(&tamossv1alpha1.Tamoss{}, &tamossv1alpha1.TamossHibernate{}, &appsv1.Deployment{}).
			WithObjects(tamoss, destination, hibernate, cluster, api, worker, ui, console, hpa, scheduled, pod).
			Build(),
		Scheme:       scheme,
		Recorder:     recorder,
		PollInterval: time.Second,
	}

	request := ctrl.Request{NamespacedName: types.NamespacedName{Name: hibernate.Name, Namespace: hibernate.Namespace}}
	result, err := reconciler.Reconcile(ctx, request)
	if err != nil {
		t.Fatalf("expected hibernate reconcile to quiesce workloads, got error %v", err)
	}
	if result.RequeueAfter != time.Second {
		t.Fatalf("expected poll requeue, got %#v", result)
	}

	updatedTamoss := &tamossv1alpha1.Tamoss{}
	if err := reconciler.Client.Get(ctx, types.NamespacedName{Name: tamoss.Name, Namespace: tamoss.Namespace}, updatedTamoss); err != nil {
		t.Fatalf("get updated Tamoss: %v", err)
	}
	if updatedTamoss.Status.Lifecycle.Phase != string(tamossv1alpha1.TamossLifecyclePhaseHibernating) {
		t.Fatalf("expected Tamoss lifecycle Hibernating, got %#v", updatedTamoss.Status.Lifecycle)
	}
	if updatedTamoss.Status.Lifecycle.ActiveOperationRef == nil || updatedTamoss.Status.Lifecycle.ActiveOperationRef.Name != hibernate.Name {
		t.Fatalf("expected active hibernate operation ref, got %#v", updatedTamoss.Status.Lifecycle.ActiveOperationRef)
	}

	updatedCluster := &cnpgv1.Cluster{}
	if err := reconciler.Client.Get(ctx, types.NamespacedName{Name: tamoss.ResourceName("db"), Namespace: tamoss.Namespace}, updatedCluster); err != nil {
		t.Fatalf("get updated CNPG cluster: %v", err)
	}
	store := updatedCluster.Spec.Backup.BarmanObjectStore
	if store == nil {
		t.Fatal("expected CNPG barman object store backup configuration")
	}
	if store.EndpointURL != "https://s3.eu-west-2.amazonaws.com" {
		t.Fatalf("expected hibernate endpoint URL, got %q", store.EndpointURL)
	}
	if store.DestinationPath != "s3://archive/hibernate/example/snap-1/cnpg" {
		t.Fatalf("expected hibernate destination path, got %q", store.DestinationPath)
	}
	if store.AWS == nil || store.AWS.AccessKeyIDReference.Name != "archive-s3" || store.AWS.AccessKeyIDReference.Key != "accessKeyID" {
		t.Fatalf("expected CNPG S3 credential references, got %#v", store.AWS)
	}

	backup := &cnpgv1.Backup{}
	if err := reconciler.Client.Get(ctx, types.NamespacedName{Name: hibernate.Name, Namespace: hibernate.Namespace}, backup); !apierrors.IsNotFound(err) {
		t.Fatalf("expected backup creation to wait for quiescence, got %v", err)
	}

	for _, component := range []string{"api", "worker", "ui", "console"} {
		deployment := &appsv1.Deployment{}
		if err := reconciler.Client.Get(ctx, types.NamespacedName{Name: tamoss.ResourceName(component), Namespace: tamoss.Namespace}, deployment); err != nil {
			t.Fatalf("get %s deployment: %v", component, err)
		}
		if deployment.Spec.Replicas == nil || *deployment.Spec.Replicas != 0 {
			t.Fatalf("expected %s deployment scaled to zero, got %#v", component, deployment.Spec.Replicas)
		}
	}
	if err := reconciler.Client.Get(ctx, types.NamespacedName{Name: hpa.Name, Namespace: hpa.Namespace}, &autoscalingv2.HorizontalPodAutoscaler{}); !apierrors.IsNotFound(err) {
		t.Fatalf("expected managed HPA to be removed during quiescence, got %v", err)
	}
	updatedScheduled := &cnpgv1.ScheduledBackup{}
	if err := reconciler.Client.Get(ctx, types.NamespacedName{Name: scheduled.Name, Namespace: scheduled.Namespace}, updatedScheduled); err != nil {
		t.Fatalf("get suspended ScheduledBackup: %v", err)
	}
	if updatedScheduled.Spec.Suspend == nil || !*updatedScheduled.Spec.Suspend {
		t.Fatalf("expected routine CNPG backups to be suspended during hibernation, got %#v", updatedScheduled.Spec.Suspend)
	}

	updatedHibernate := &tamossv1alpha1.TamossHibernate{}
	if err := reconciler.Client.Get(ctx, types.NamespacedName{Name: hibernate.Name, Namespace: hibernate.Namespace}, updatedHibernate); err != nil {
		t.Fatalf("get updated TamossHibernate: %v", err)
	}
	if updatedHibernate.Status.Phase != string(tamossv1alpha1.TamossOperationPhaseQuiescing) {
		t.Fatalf("expected hibernate status Quiescing, got %#v", updatedHibernate.Status)
	}
	if updatedHibernate.Status.Artifact.ManifestKey != "hibernate/example/snap-1/manifest.json" {
		t.Fatalf("expected manifest key in status, got %#v", updatedHibernate.Status.Artifact)
	}

	result, err = reconciler.Reconcile(ctx, request)
	if err != nil {
		t.Fatalf("expected hibernate to keep waiting for observed replicas, got error %v", err)
	}
	if result.RequeueAfter != time.Second {
		t.Fatalf("expected quiescence poll requeue, got %#v", result)
	}
	if err := reconciler.Client.Get(ctx, types.NamespacedName{Name: hibernate.Name, Namespace: hibernate.Namespace}, backup); !apierrors.IsNotFound(err) {
		t.Fatalf("expected backup creation to wait for observed replicas, got %v", err)
	}

	for _, component := range []string{"api", "worker", "ui", "console"} {
		deployment := &appsv1.Deployment{}
		key := types.NamespacedName{Name: tamoss.ResourceName(component), Namespace: tamoss.Namespace}
		if err := reconciler.Client.Get(ctx, key, deployment); err != nil {
			t.Fatalf("get %s deployment for termination: %v", component, err)
		}
		deployment.Status = appsv1.DeploymentStatus{}
		if err := reconciler.Client.Status().Update(ctx, deployment); err != nil {
			t.Fatalf("mark %s deployment terminated: %v", component, err)
		}
	}

	result, err = reconciler.Reconcile(ctx, request)
	if err != nil {
		t.Fatalf("expected hibernate to wait for terminating pods, got error %v", err)
	}
	if err := reconciler.Client.Get(ctx, types.NamespacedName{Name: hibernate.Name, Namespace: hibernate.Namespace}, backup); !apierrors.IsNotFound(err) {
		t.Fatalf("expected backup creation to wait for live workload pods, got %v", err)
	}
	if err := reconciler.Client.Delete(ctx, pod); err != nil {
		t.Fatalf("remove terminated workload pod: %v", err)
	}

	result, err = reconciler.Reconcile(ctx, request)
	if err != nil {
		t.Fatalf("expected pod-quiesced hibernate reconcile to launch backup, got error %v", err)
	}
	if result.RequeueAfter != time.Second {
		t.Fatalf("expected backup poll requeue, got %#v", result)
	}
	if err := reconciler.Client.Get(ctx, types.NamespacedName{Name: hibernate.Name, Namespace: hibernate.Namespace}, backup); err != nil {
		t.Fatalf("expected CNPG Backup to be created after quiescence: %v", err)
	}
	if backup.Spec.Cluster.Name != tamoss.ResourceName("db") || backup.Spec.Method != cnpgv1.BackupMethodBarmanObjectStore {
		t.Fatalf("unexpected Backup spec: %#v", backup.Spec)
	}
	if err := reconciler.Client.Get(ctx, request.NamespacedName, updatedHibernate); err != nil {
		t.Fatalf("get capturing TamossHibernate: %v", err)
	}
	if updatedHibernate.Status.Phase != string(tamossv1alpha1.TamossOperationPhaseCapturingDatabase) {
		t.Fatalf("expected hibernate status CapturingDatabase, got %#v", updatedHibernate.Status)
	}
	assertEventContains(t, drainRecorder(recorder), operatorstatus.ReasonTamossHibernating)
}

func TestTamossHibernateCompletesWhenCNPGBackupCompletes(t *testing.T) {
	ctx := context.Background()
	scheme := hibernateTestScheme(t)
	tamoss := hibernateTamossFixture()
	destination := hibernateDestinationFixture()
	hibernate := hibernateFixture()
	cluster := hibernateClusterFixture(tamoss)
	backup := hibernateCompletedBackupFixture(tamoss, hibernate)
	writer := &fakeHibernationManifestWriter{checksum: "sha256:fake"}

	reconciler := TamossHibernateReconciler{
		Client: fake.NewClientBuilder().WithInterceptorFuncs(fakeApplyInterceptor()).
			WithScheme(scheme).
			WithStatusSubresource(&tamossv1alpha1.Tamoss{}, &tamossv1alpha1.TamossHibernate{}).
			WithObjects(tamoss, destination, hibernate, cluster, backup).
			Build(),
		Scheme:         scheme,
		ManifestWriter: writer,
	}

	request := ctrl.Request{NamespacedName: types.NamespacedName{Name: hibernate.Name, Namespace: hibernate.Namespace}}
	_, err := reconciler.Reconcile(ctx, request)
	if err != nil {
		t.Fatalf("expected hibernate reconcile to commit the artifact, got error %v", err)
	}
	finishHibernateSourceDeprovisioning(t, ctx, &reconciler, request)

	updatedHibernate := &tamossv1alpha1.TamossHibernate{}
	if err := reconciler.Client.Get(ctx, types.NamespacedName{Name: hibernate.Name, Namespace: hibernate.Namespace}, updatedHibernate); err != nil {
		t.Fatalf("get updated TamossHibernate: %v", err)
	}
	if updatedHibernate.Status.Phase != string(tamossv1alpha1.TamossOperationPhaseCompleted) {
		t.Fatalf("expected hibernate status Completed, got %#v", updatedHibernate.Status)
	}
	if updatedHibernate.Status.Artifact.Checksum != "sha256:fake" || updatedHibernate.Status.Artifact.CNPGBackup.BackupID != "20260707T100000" {
		t.Fatalf("expected completed artifact status, got %#v", updatedHibernate.Status.Artifact)
	}
	if updatedHibernate.Status.CompletedAt == nil {
		t.Fatal("expected hibernate completed timestamp")
	}
	if err := reconciler.Client.Get(ctx, types.NamespacedName{Name: cluster.Name, Namespace: cluster.Namespace}, &cnpgv1.Cluster{}); !apierrors.IsNotFound(err) {
		t.Fatalf("expected source CNPG Cluster to be removed before completion, got %v", err)
	}

	updatedTamoss := &tamossv1alpha1.Tamoss{}
	if err := reconciler.Client.Get(ctx, types.NamespacedName{Name: tamoss.Name, Namespace: tamoss.Namespace}, updatedTamoss); err != nil {
		t.Fatalf("get updated Tamoss: %v", err)
	}
	if updatedTamoss.Status.Lifecycle.Phase != string(tamossv1alpha1.TamossLifecyclePhaseHibernated) {
		t.Fatalf("expected Tamoss lifecycle Hibernated, got %#v", updatedTamoss.Status.Lifecycle)
	}
	if updatedTamoss.Status.Lifecycle.ActiveOperationRef != nil {
		t.Fatalf("expected active operation to be cleared, got %#v", updatedTamoss.Status.Lifecycle.ActiveOperationRef)
	}
	if updatedTamoss.Status.Lifecycle.LastHibernateRef == nil || updatedTamoss.Status.Lifecycle.LastHibernateRef.Name != hibernate.Name {
		t.Fatalf("expected last hibernate ref, got %#v", updatedTamoss.Status.Lifecycle.LastHibernateRef)
	}

	if writer.calls != 1 {
		t.Fatalf("expected one manifest upload, got %d", writer.calls)
	}
	if writer.key != "hibernate/example/snap-1/manifest.json" {
		t.Fatalf("expected manifest key, got %q", writer.key)
	}
	if writer.manifest.SourceTamoss.Name != tamoss.Name ||
		writer.manifest.CNPG.BackupID != "20260707T100000" ||
		writer.manifest.StorageBackend.Bucket != "archive" {
		t.Fatalf("unexpected manifest payload: %#v", writer.manifest)
	}
}

func TestTamossHibernateRecoversPartialCompletion(t *testing.T) {
	ctx := context.Background()
	scheme := hibernateTestScheme(t)
	tamoss := hibernateTamossFixture()
	destination := hibernateDestinationFixture()
	hibernate := hibernateFixture()
	tamoss.Status.Lifecycle = tamossv1alpha1.TamossLifecycleStatus{
		Phase:            string(tamossv1alpha1.TamossLifecyclePhaseHibernated),
		Reason:           operatorstatus.ReasonTamossHibernated,
		LastHibernateRef: operationObjectReference(hibernate, "TamossHibernate"),
	}
	hibernate.Status.Phase = string(tamossv1alpha1.TamossOperationPhaseWritingManifest)
	cluster := hibernateClusterFixture(tamoss)
	backup := hibernateCompletedBackupFixture(tamoss, hibernate)
	writer := &fakeHibernationManifestWriter{checksum: "sha256:recovered"}

	reconciler := TamossHibernateReconciler{
		Client: fake.NewClientBuilder().WithInterceptorFuncs(fakeApplyInterceptor()).
			WithScheme(scheme).
			WithStatusSubresource(&tamossv1alpha1.Tamoss{}, &tamossv1alpha1.TamossHibernate{}).
			WithObjects(tamoss, destination, hibernate, cluster, backup).
			Build(),
		Scheme:         scheme,
		ManifestWriter: writer,
	}

	request := ctrl.Request{NamespacedName: types.NamespacedName{Name: hibernate.Name, Namespace: hibernate.Namespace}}
	if _, err := reconciler.Reconcile(ctx, request); err != nil {
		t.Fatalf("expected partial completion to recover, got %v", err)
	}
	finishHibernateSourceDeprovisioning(t, ctx, &reconciler, request)

	updated := &tamossv1alpha1.TamossHibernate{}
	if err := reconciler.Client.Get(ctx, types.NamespacedName{Name: hibernate.Name, Namespace: hibernate.Namespace}, updated); err != nil {
		t.Fatalf("get updated TamossHibernate: %v", err)
	}
	if updated.Status.Phase != string(tamossv1alpha1.TamossOperationPhaseCompleted) ||
		updated.Status.Artifact.Checksum != "sha256:recovered" {
		t.Fatalf("expected recovered completion status, got %#v", updated.Status)
	}
}

func TestTamossHibernateAddsFinalizerBeforeReconciling(t *testing.T) {
	ctx := context.Background()
	scheme := hibernateTestScheme(t)
	hibernate := hibernateFixture()
	hibernate.Finalizers = nil

	reconciler := TamossHibernateReconciler{
		Client: fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(hibernate).
			Build(),
		Scheme: scheme,
	}

	_, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: hibernate.Name, Namespace: hibernate.Namespace}})
	if err != nil {
		t.Fatalf("expected finalizer reconcile without error, got %v", err)
	}

	updated := &tamossv1alpha1.TamossHibernate{}
	if err := reconciler.Client.Get(ctx, types.NamespacedName{Name: hibernate.Name, Namespace: hibernate.Namespace}, updated); err != nil {
		t.Fatalf("get updated TamossHibernate: %v", err)
	}
	if !hasFinalizer(updated.Finalizers, tamossHibernateFinalizer) {
		t.Fatalf("expected hibernate finalizer, got %#v", updated.Finalizers)
	}
	if updated.Status.Phase != "" {
		t.Fatalf("expected reconcile to stop after finalizer add, got status %#v", updated.Status)
	}
}

func TestTamossHibernateDeleteMarksActiveLifecycleFailed(t *testing.T) {
	ctx := context.Background()
	scheme := hibernateTestScheme(t)
	tamoss := hibernateTamossFixture()
	hibernate := hibernateFixture()
	now := metav1.Now()
	hibernate.DeletionTimestamp = &now
	hibernate.Status.Phase = string(tamossv1alpha1.TamossOperationPhaseCapturingDatabase)
	tamoss.Status.Lifecycle = tamossv1alpha1.TamossLifecycleStatus{
		Phase:              string(tamossv1alpha1.TamossLifecyclePhaseHibernating),
		Reason:             operatorstatus.ReasonTamossHibernating,
		ActiveOperationRef: operationObjectReference(hibernate, "TamossHibernate"),
	}

	reconciler := TamossHibernateReconciler{
		Client: fake.NewClientBuilder().WithInterceptorFuncs(fakeApplyInterceptor()).
			WithScheme(scheme).
			WithStatusSubresource(&tamossv1alpha1.Tamoss{}, &tamossv1alpha1.TamossHibernate{}).
			WithObjects(tamoss, hibernate).
			Build(),
		Scheme: scheme,
	}

	_, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: hibernate.Name, Namespace: hibernate.Namespace}})
	if err != nil {
		t.Fatalf("expected hibernate finalization without error, got %v", err)
	}

	updatedTamoss := &tamossv1alpha1.Tamoss{}
	if err := reconciler.Client.Get(ctx, types.NamespacedName{Name: tamoss.Name, Namespace: tamoss.Namespace}, updatedTamoss); err != nil {
		t.Fatalf("get updated Tamoss: %v", err)
	}
	if updatedTamoss.Status.Lifecycle.Phase != string(tamossv1alpha1.TamossLifecyclePhaseFailed) ||
		updatedTamoss.Status.Lifecycle.Reason != operatorstatus.ReasonLifecycleOperationDeleted {
		t.Fatalf("expected deleted operation lifecycle failure, got %#v", updatedTamoss.Status.Lifecycle)
	}

	updatedHibernate := &tamossv1alpha1.TamossHibernate{}
	if err := reconciler.Client.Get(ctx, types.NamespacedName{Name: hibernate.Name, Namespace: hibernate.Namespace}, updatedHibernate); err != nil {
		if apierrors.IsNotFound(err) {
			return
		}
		t.Fatalf("get updated TamossHibernate: %v", err)
	} else if hasFinalizer(updatedHibernate.Finalizers, tamossHibernateFinalizer) {
		t.Fatalf("expected hibernate finalizer removed, got %#v", updatedHibernate.Finalizers)
	}
}

func TestTamossHibernateDeleteAfterManifestCommitFinishesHibernation(t *testing.T) {
	ctx := context.Background()
	scheme := hibernateTestScheme(t)
	tamoss := hibernateTamossFixture()
	hibernate := hibernateFixture()
	now := metav1.Now()
	hibernate.DeletionTimestamp = &now
	hibernate.Status.Phase = string(tamossv1alpha1.TamossOperationPhaseDeprovisioningSource)
	hibernate.Status.Artifact = tamossv1alpha1.HibernationArtifactStatus{
		ManifestKey: "hibernate/example/snap-1/manifest.json",
		Checksum:    "sha256:committed",
	}
	tamoss.Status.Lifecycle = tamossv1alpha1.TamossLifecycleStatus{
		Phase:              string(tamossv1alpha1.TamossLifecyclePhaseHibernating),
		Reason:             operatorstatus.ReasonTamossHibernating,
		ActiveOperationRef: operationObjectReference(hibernate, "TamossHibernate"),
	}
	cluster := hibernateClusterFixture(tamoss)

	reconciler := TamossHibernateReconciler{
		Client: fake.NewClientBuilder().WithInterceptorFuncs(fakeApplyInterceptor()).
			WithScheme(scheme).
			WithStatusSubresource(&tamossv1alpha1.Tamoss{}, &tamossv1alpha1.TamossHibernate{}, &cnpgv1.Cluster{}).
			WithObjects(tamoss, hibernate, cluster).
			Build(),
		Scheme:       scheme,
		PollInterval: time.Second,
	}
	request := ctrl.Request{NamespacedName: types.NamespacedName{Name: hibernate.Name, Namespace: hibernate.Namespace}}
	result, err := reconciler.Reconcile(ctx, request)
	if err != nil {
		t.Fatalf("expected committed hibernation finalization to remove source cluster, got %v", err)
	}
	if result.RequeueAfter != time.Second {
		t.Fatalf("expected source deletion checkpoint requeue, got %#v", result)
	}
	if err := reconciler.Client.Get(ctx, types.NamespacedName{Name: cluster.Name, Namespace: cluster.Namespace}, &cnpgv1.Cluster{}); !apierrors.IsNotFound(err) {
		t.Fatalf("expected committed hibernation source cluster removed, got %v", err)
	}
	if _, err := reconciler.Reconcile(ctx, request); err != nil {
		t.Fatalf("expected committed hibernation finalization to complete, got %v", err)
	}

	updatedTamoss := &tamossv1alpha1.Tamoss{}
	if err := reconciler.Client.Get(ctx, types.NamespacedName{Name: tamoss.Name, Namespace: tamoss.Namespace}, updatedTamoss); err != nil {
		t.Fatalf("get hibernated TAMOSS: %v", err)
	}
	if updatedTamoss.Status.Lifecycle.Phase != string(tamossv1alpha1.TamossLifecyclePhaseHibernated) {
		t.Fatalf("expected committed deletion to leave TAMOSS Hibernated, got %#v", updatedTamoss.Status.Lifecycle)
	}
}

func TestTamossHibernateWaitsForMissingDestination(t *testing.T) {
	ctx := context.Background()
	scheme := hibernateTestScheme(t)
	tamoss := hibernateTamossFixture()
	hibernate := hibernateFixture()

	reconciler := TamossHibernateReconciler{
		Client: fake.NewClientBuilder().WithInterceptorFuncs(fakeApplyInterceptor()).
			WithScheme(scheme).
			WithStatusSubresource(&tamossv1alpha1.Tamoss{}, &tamossv1alpha1.TamossHibernate{}).
			WithObjects(tamoss, hibernate).
			Build(),
		Scheme:       scheme,
		PollInterval: time.Second,
	}

	result, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: hibernate.Name, Namespace: hibernate.Namespace}})
	if err != nil {
		t.Fatalf("expected missing destination to wait without error, got %v", err)
	}
	if result.RequeueAfter != time.Second {
		t.Fatalf("expected requeue while destination is missing, got %#v", result)
	}

	updated := &tamossv1alpha1.TamossHibernate{}
	if err := reconciler.Client.Get(ctx, types.NamespacedName{Name: hibernate.Name, Namespace: hibernate.Namespace}, updated); err != nil {
		t.Fatalf("get updated TamossHibernate: %v", err)
	}
	if updated.Status.Phase != string(tamossv1alpha1.TamossOperationPhasePreparingTarget) ||
		updated.Status.Reason != operatorstatus.ReasonStorageBackendNotReady {
		t.Fatalf("expected waiting status for missing destination, got %#v", updated.Status)
	}
}

func TestTamossHibernateWaitsForSourceSchemaMigration(t *testing.T) {
	ctx := context.Background()
	scheme := hibernateTestScheme(t)
	tamoss := hibernateTamossFixture()
	tamoss.Status.Conditions = nil
	tamoss.Status.SchemaVersion = ""
	destination := hibernateDestinationFixture()
	hibernate := hibernateFixture()
	cluster := hibernateClusterFixture(tamoss)

	reconciler := TamossHibernateReconciler{
		Client: fake.NewClientBuilder().WithInterceptorFuncs(fakeApplyInterceptor()).
			WithScheme(scheme).
			WithStatusSubresource(&tamossv1alpha1.Tamoss{}, &tamossv1alpha1.TamossHibernate{}).
			WithObjects(tamoss, destination, hibernate, cluster).
			Build(),
		Scheme:       scheme,
		PollInterval: time.Second,
	}

	result, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: hibernate.Name, Namespace: hibernate.Namespace}})
	if err != nil {
		t.Fatalf("expected source schema migration to wait without error, got %v", err)
	}
	if result.RequeueAfter != time.Second {
		t.Fatalf("expected source schema poll requeue, got %#v", result)
	}
	updated := &tamossv1alpha1.TamossHibernate{}
	if err := reconciler.Client.Get(ctx, types.NamespacedName{Name: hibernate.Name, Namespace: hibernate.Namespace}, updated); err != nil {
		t.Fatalf("get updated TamossHibernate: %v", err)
	}
	if updated.Status.Phase != string(tamossv1alpha1.TamossOperationPhaseResolvingSource) ||
		updated.Status.Reason != operatorstatus.ReasonSchemaNotReady {
		t.Fatalf("expected source schema waiting status, got %#v", updated.Status)
	}
}

func TestTamossHibernateRejectsUnsupportedSourceSchema(t *testing.T) {
	ctx := context.Background()
	scheme := hibernateTestScheme(t)
	tamoss := hibernateTamossFixture()
	tamoss.Status.SchemaVersion = "future"
	destination := hibernateDestinationFixture()
	hibernate := hibernateFixture()
	cluster := hibernateClusterFixture(tamoss)

	reconciler := TamossHibernateReconciler{
		Client: fake.NewClientBuilder().WithInterceptorFuncs(fakeApplyInterceptor()).
			WithScheme(scheme).
			WithStatusSubresource(&tamossv1alpha1.Tamoss{}, &tamossv1alpha1.TamossHibernate{}).
			WithObjects(tamoss, destination, hibernate, cluster).
			Build(),
		Scheme: scheme,
	}

	if _, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: hibernate.Name, Namespace: hibernate.Namespace}}); err != nil {
		t.Fatalf("expected unsupported schema to fail without reconcile error, got %v", err)
	}
	updated := &tamossv1alpha1.TamossHibernate{}
	if err := reconciler.Client.Get(ctx, types.NamespacedName{Name: hibernate.Name, Namespace: hibernate.Namespace}, updated); err != nil {
		t.Fatalf("get updated TamossHibernate: %v", err)
	}
	if updated.Status.Phase != string(tamossv1alpha1.TamossOperationPhaseFailed) ||
		updated.Status.Reason != operatorstatus.ReasonUnsupportedSchemaVersion {
		t.Fatalf("expected unsupported source schema failure, got %#v", updated.Status)
	}
}

func TestTamossHibernateFailsWhenCNPGBackupFails(t *testing.T) {
	ctx := context.Background()
	scheme := hibernateTestScheme(t)
	tamoss := hibernateTamossFixture()
	destination := hibernateDestinationFixture()
	hibernate := hibernateFixture()
	cluster := hibernateClusterFixture(tamoss)
	backup := hibernateCompletedBackupFixture(tamoss, hibernate)
	backup.Status = cnpgv1.BackupStatus{
		Phase: cnpgv1.BackupPhaseFailed,
		Error: "disk full",
	}

	reconciler := TamossHibernateReconciler{
		Client: fake.NewClientBuilder().WithInterceptorFuncs(fakeApplyInterceptor()).
			WithScheme(scheme).
			WithStatusSubresource(&tamossv1alpha1.Tamoss{}, &tamossv1alpha1.TamossHibernate{}).
			WithObjects(tamoss, destination, hibernate, cluster, backup).
			Build(),
		Scheme: scheme,
	}

	result, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: hibernate.Name, Namespace: hibernate.Namespace}})
	if err != nil {
		t.Fatalf("expected failed backup to update status without error, got %v", err)
	}
	if result.RequeueAfter != 0 {
		t.Fatalf("expected terminal backup failure without requeue, got %#v", result)
	}

	updatedHibernate := &tamossv1alpha1.TamossHibernate{}
	if err := reconciler.Client.Get(ctx, types.NamespacedName{Name: hibernate.Name, Namespace: hibernate.Namespace}, updatedHibernate); err != nil {
		t.Fatalf("get updated TamossHibernate: %v", err)
	}
	if updatedHibernate.Status.Phase != string(tamossv1alpha1.TamossOperationPhaseFailed) ||
		updatedHibernate.Status.Reason != operatorstatus.ReasonBackupPolicyFailed ||
		!strings.Contains(updatedHibernate.Status.Message, "disk full") {
		t.Fatalf("expected backup failure status, got %#v", updatedHibernate.Status)
	}

	updatedTamoss := &tamossv1alpha1.Tamoss{}
	if err := reconciler.Client.Get(ctx, types.NamespacedName{Name: tamoss.Name, Namespace: tamoss.Namespace}, updatedTamoss); err != nil {
		t.Fatalf("get updated Tamoss: %v", err)
	}
	if updatedTamoss.Status.Lifecycle.Phase != string(tamossv1alpha1.TamossLifecyclePhaseFailed) {
		t.Fatalf("expected Tamoss lifecycle Failed, got %#v", updatedTamoss.Status.Lifecycle)
	}
}

func TestTamossHibernateRetriesManifestUploadFailure(t *testing.T) {
	ctx := context.Background()
	scheme := hibernateTestScheme(t)
	tamoss := hibernateTamossFixture()
	destination := hibernateDestinationFixture()
	hibernate := hibernateFixture()
	cluster := hibernateClusterFixture(tamoss)
	backup := hibernateCompletedBackupFixture(tamoss, hibernate)
	writer := &fakeHibernationManifestWriter{checksum: "sha256:fake", err: fmt.Errorf("connection reset")}

	reconciler := TamossHibernateReconciler{
		Client: fake.NewClientBuilder().WithInterceptorFuncs(fakeApplyInterceptor()).
			WithScheme(scheme).
			WithStatusSubresource(&tamossv1alpha1.Tamoss{}, &tamossv1alpha1.TamossHibernate{}).
			WithObjects(tamoss, destination, hibernate, cluster, backup).
			Build(),
		Scheme:         scheme,
		ManifestWriter: writer,
	}

	request := ctrl.Request{NamespacedName: types.NamespacedName{Name: hibernate.Name, Namespace: hibernate.Namespace}}
	result, err := reconciler.Reconcile(ctx, request)
	if err != nil {
		t.Fatalf("expected upload failure to be retried without error, got %v", err)
	}
	if result.RequeueAfter <= 0 {
		t.Fatalf("expected requeue for manifest upload retry, got %#v", result)
	}

	updatedHibernate := &tamossv1alpha1.TamossHibernate{}
	if err := reconciler.Client.Get(ctx, request.NamespacedName, updatedHibernate); err != nil {
		t.Fatalf("get updated TamossHibernate: %v", err)
	}
	if updatedHibernate.Status.Phase != string(tamossv1alpha1.TamossOperationPhaseWritingManifest) ||
		updatedHibernate.Status.Reason != operatorstatus.ReasonHibernateManifestUploadFailed ||
		!strings.Contains(updatedHibernate.Status.Message, "connection reset") {
		t.Fatalf("expected retrying upload status, got %#v", updatedHibernate.Status)
	}
	if updatedHibernate.Status.CompletedAt != nil {
		t.Fatalf("expected no completion timestamp during upload retry, got %#v", updatedHibernate.Status.CompletedAt)
	}

	updatedTamoss := &tamossv1alpha1.Tamoss{}
	if err := reconciler.Client.Get(ctx, types.NamespacedName{Name: tamoss.Name, Namespace: tamoss.Namespace}, updatedTamoss); err != nil {
		t.Fatalf("get updated Tamoss: %v", err)
	}
	if updatedTamoss.Status.Lifecycle.Phase != string(tamossv1alpha1.TamossLifecyclePhaseHibernating) {
		t.Fatalf("expected lifecycle to stay Hibernating during upload retry, got %#v", updatedTamoss.Status.Lifecycle)
	}

	writer.err = nil
	if _, err := reconciler.Reconcile(ctx, request); err != nil {
		t.Fatalf("expected upload retry to commit the artifact, got %v", err)
	}
	finishHibernateSourceDeprovisioning(t, ctx, &reconciler, request)
	if writer.calls != 2 {
		t.Fatalf("expected a second upload attempt, got %d calls", writer.calls)
	}
	if err := reconciler.Client.Get(ctx, request.NamespacedName, updatedHibernate); err != nil {
		t.Fatalf("get updated TamossHibernate: %v", err)
	}
	if updatedHibernate.Status.Phase != string(tamossv1alpha1.TamossOperationPhaseCompleted) {
		t.Fatalf("expected hibernate to complete after retry, got %#v", updatedHibernate.Status)
	}
}

func TestTamossHibernateTerminalPhasesRemainIdempotent(t *testing.T) {
	ctx := context.Background()
	scheme := hibernateTestScheme(t)
	tamoss := hibernateTamossFixture()
	tamoss.Status.Lifecycle = tamossv1alpha1.TamossLifecycleStatus{
		Phase:  string(tamossv1alpha1.TamossLifecyclePhaseRunning),
		Reason: operatorstatus.ReasonTamossReady,
	}
	destination := hibernateDestinationFixture()
	hibernate := hibernateFixture()
	hibernate.Status.Phase = string(tamossv1alpha1.TamossOperationPhaseCompleted)
	api := hibernateDeploymentFixture(tamoss, "api", 2)

	reconciler := TamossHibernateReconciler{
		Client: fake.NewClientBuilder().WithInterceptorFuncs(fakeApplyInterceptor()).
			WithScheme(scheme).
			WithStatusSubresource(&tamossv1alpha1.Tamoss{}, &tamossv1alpha1.TamossHibernate{}).
			WithObjects(tamoss, destination, hibernate, api).
			Build(),
		Scheme: scheme,
	}

	result, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: hibernate.Name, Namespace: hibernate.Namespace}})
	if err != nil || result.RequeueAfter != 0 {
		t.Fatalf("expected completed hibernate to be a no-op, got result %#v, err %v", result, err)
	}

	backup := &cnpgv1.Backup{}
	if err := reconciler.Client.Get(ctx, types.NamespacedName{Name: hibernate.Name, Namespace: hibernate.Namespace}, backup); !apierrors.IsNotFound(err) {
		t.Fatalf("expected no backup for completed hibernate, got err %v", err)
	}

	deployment := &appsv1.Deployment{}
	if err := reconciler.Client.Get(ctx, types.NamespacedName{Name: tamoss.ResourceName("api"), Namespace: tamoss.Namespace}, deployment); err != nil {
		t.Fatalf("get api deployment: %v", err)
	}
	if deployment.Spec.Replicas == nil || *deployment.Spec.Replicas != 2 {
		t.Fatalf("expected completed hibernate to leave workloads alone, got %#v", deployment.Spec.Replicas)
	}

	updatedTamoss := &tamossv1alpha1.Tamoss{}
	if err := reconciler.Client.Get(ctx, types.NamespacedName{Name: tamoss.Name, Namespace: tamoss.Namespace}, updatedTamoss); err != nil {
		t.Fatalf("get updated Tamoss: %v", err)
	}
	if updatedTamoss.Status.Lifecycle.Phase != string(tamossv1alpha1.TamossLifecyclePhaseRunning) {
		t.Fatalf("expected lifecycle to stay Running, got %#v", updatedTamoss.Status.Lifecycle)
	}
}

func TestTamossHibernateFailsOnLifecycleConflict(t *testing.T) {
	ctx := context.Background()
	scheme := hibernateTestScheme(t)
	tamoss := hibernateTamossFixture()
	tamoss.Status.Lifecycle = tamossv1alpha1.TamossLifecycleStatus{
		Phase:  string(tamossv1alpha1.TamossLifecyclePhaseResuming),
		Reason: operatorstatus.ReasonTamossResuming,
		ActiveOperationRef: &corev1.ObjectReference{
			APIVersion: tamossv1alpha1.GroupVersion.String(),
			Kind:       "TamossHibernate",
			Namespace:  "media",
			Name:       "other-op",
		},
	}
	destination := hibernateDestinationFixture()
	hibernate := hibernateFixture()
	cluster := hibernateClusterFixture(tamoss)

	reconciler := TamossHibernateReconciler{
		Client: fake.NewClientBuilder().WithInterceptorFuncs(fakeApplyInterceptor()).
			WithScheme(scheme).
			WithStatusSubresource(&tamossv1alpha1.Tamoss{}, &tamossv1alpha1.TamossHibernate{}).
			WithObjects(tamoss, destination, hibernate, cluster).
			Build(),
		Scheme: scheme,
	}

	if _, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: hibernate.Name, Namespace: hibernate.Namespace}}); err != nil {
		t.Fatalf("expected conflict to update status without error, got %v", err)
	}

	updatedHibernate := &tamossv1alpha1.TamossHibernate{}
	if err := reconciler.Client.Get(ctx, types.NamespacedName{Name: hibernate.Name, Namespace: hibernate.Namespace}, updatedHibernate); err != nil {
		t.Fatalf("get updated TamossHibernate: %v", err)
	}
	if updatedHibernate.Status.Phase != string(tamossv1alpha1.TamossOperationPhaseFailed) ||
		updatedHibernate.Status.Reason != operatorstatus.ReasonLifecycleOperationConflict {
		t.Fatalf("expected lifecycle conflict failure, got %#v", updatedHibernate.Status)
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

func TestTamossHibernateFailsWhenTamossAlreadyHibernated(t *testing.T) {
	ctx := context.Background()
	scheme := hibernateTestScheme(t)
	tamoss := hibernateTamossFixture()
	tamoss.Status.Lifecycle = tamossv1alpha1.TamossLifecycleStatus{
		Phase:  string(tamossv1alpha1.TamossLifecyclePhaseHibernated),
		Reason: operatorstatus.ReasonTamossHibernated,
	}
	destination := hibernateDestinationFixture()
	hibernate := hibernateFixture()
	cluster := hibernateClusterFixture(tamoss)

	reconciler := TamossHibernateReconciler{
		Client: fake.NewClientBuilder().WithInterceptorFuncs(fakeApplyInterceptor()).
			WithScheme(scheme).
			WithStatusSubresource(&tamossv1alpha1.Tamoss{}, &tamossv1alpha1.TamossHibernate{}).
			WithObjects(tamoss, destination, hibernate, cluster).
			Build(),
		Scheme: scheme,
	}

	if _, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: hibernate.Name, Namespace: hibernate.Namespace}}); err != nil {
		t.Fatalf("expected already-hibernated conflict to update status without error, got %v", err)
	}

	updatedHibernate := &tamossv1alpha1.TamossHibernate{}
	if err := reconciler.Client.Get(ctx, types.NamespacedName{Name: hibernate.Name, Namespace: hibernate.Namespace}, updatedHibernate); err != nil {
		t.Fatalf("get updated TamossHibernate: %v", err)
	}
	if updatedHibernate.Status.Phase != string(tamossv1alpha1.TamossOperationPhaseFailed) ||
		updatedHibernate.Status.Reason != operatorstatus.ReasonLifecycleOperationConflict {
		t.Fatalf("expected already-hibernated failure, got %#v", updatedHibernate.Status)
	}
}

func TestTamossHibernateDeleteAfterCompletionKeepsLifecycle(t *testing.T) {
	ctx := context.Background()
	scheme := hibernateTestScheme(t)
	tamoss := hibernateTamossFixture()
	hibernate := hibernateFixture()
	now := metav1.Now()
	hibernate.DeletionTimestamp = &now
	hibernate.Status.Phase = string(tamossv1alpha1.TamossOperationPhaseCompleted)
	tamoss.Status.Lifecycle = tamossv1alpha1.TamossLifecycleStatus{
		Phase:            string(tamossv1alpha1.TamossLifecyclePhaseHibernated),
		Reason:           operatorstatus.ReasonTamossHibernated,
		LastHibernateRef: operationObjectReference(hibernate, "TamossHibernate"),
	}

	reconciler := TamossHibernateReconciler{
		Client: fake.NewClientBuilder().WithInterceptorFuncs(fakeApplyInterceptor()).
			WithScheme(scheme).
			WithStatusSubresource(&tamossv1alpha1.Tamoss{}, &tamossv1alpha1.TamossHibernate{}).
			WithObjects(tamoss, hibernate).
			Build(),
		Scheme: scheme,
	}

	if _, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: hibernate.Name, Namespace: hibernate.Namespace}}); err != nil {
		t.Fatalf("expected completed hibernate finalization without error, got %v", err)
	}

	updatedTamoss := &tamossv1alpha1.Tamoss{}
	if err := reconciler.Client.Get(ctx, types.NamespacedName{Name: tamoss.Name, Namespace: tamoss.Namespace}, updatedTamoss); err != nil {
		t.Fatalf("get updated Tamoss: %v", err)
	}
	if updatedTamoss.Status.Lifecycle.Phase != string(tamossv1alpha1.TamossLifecyclePhaseHibernated) {
		t.Fatalf("expected lifecycle to stay Hibernated after housekeeping delete, got %#v", updatedTamoss.Status.Lifecycle)
	}

	updatedHibernate := &tamossv1alpha1.TamossHibernate{}
	if err := reconciler.Client.Get(ctx, types.NamespacedName{Name: hibernate.Name, Namespace: hibernate.Namespace}, updatedHibernate); err != nil {
		if !apierrors.IsNotFound(err) {
			t.Fatalf("get updated TamossHibernate: %v", err)
		}
	} else if hasFinalizer(updatedHibernate.Finalizers, tamossHibernateFinalizer) {
		t.Fatalf("expected hibernate finalizer removed, got %#v", updatedHibernate.Finalizers)
	}
}

func TestTamossHibernateRejectsForeignBackup(t *testing.T) {
	ctx := context.Background()
	scheme := hibernateTestScheme(t)
	tamoss := hibernateTamossFixture()
	destination := hibernateDestinationFixture()
	hibernate := hibernateFixture()
	cluster := hibernateClusterFixture(tamoss)
	backup := hibernateCompletedBackupFixture(tamoss, hibernate)
	backup.OwnerReferences = nil

	reconciler := TamossHibernateReconciler{
		Client: fake.NewClientBuilder().WithInterceptorFuncs(fakeApplyInterceptor()).
			WithScheme(scheme).
			WithStatusSubresource(&tamossv1alpha1.Tamoss{}, &tamossv1alpha1.TamossHibernate{}).
			WithObjects(tamoss, destination, hibernate, cluster, backup).
			Build(),
		Scheme: scheme,
	}

	if _, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: hibernate.Name, Namespace: hibernate.Namespace}}); err != nil {
		t.Fatalf("expected foreign backup to fail hibernate without error, got %v", err)
	}

	updatedHibernate := &tamossv1alpha1.TamossHibernate{}
	if err := reconciler.Client.Get(ctx, types.NamespacedName{Name: hibernate.Name, Namespace: hibernate.Namespace}, updatedHibernate); err != nil {
		t.Fatalf("get updated TamossHibernate: %v", err)
	}
	if updatedHibernate.Status.Phase != string(tamossv1alpha1.TamossOperationPhaseFailed) ||
		updatedHibernate.Status.Reason != operatorstatus.ReasonLifecycleOperationConflict {
		t.Fatalf("expected foreign backup conflict failure, got %#v", updatedHibernate.Status)
	}

	updatedTamoss := &tamossv1alpha1.Tamoss{}
	if err := reconciler.Client.Get(ctx, types.NamespacedName{Name: tamoss.Name, Namespace: tamoss.Namespace}, updatedTamoss); err != nil {
		t.Fatalf("get updated Tamoss: %v", err)
	}
	if updatedTamoss.Status.Lifecycle.Phase != string(tamossv1alpha1.TamossLifecyclePhaseFailed) {
		t.Fatalf("expected Tamoss lifecycle Failed, got %#v", updatedTamoss.Status.Lifecycle)
	}
}

func TestTamossHibernateRejectsUnimplementedDriver(t *testing.T) {
	ctx := context.Background()
	scheme := hibernateTestScheme(t)
	tamoss := hibernateTamossFixture()
	destination := hibernateDestinationFixture()
	hibernate := hibernateFixture()
	hibernate.Spec.Driver = tamossv1alpha1.HibernationDriverLogicalDump

	reconciler := TamossHibernateReconciler{
		Client: fake.NewClientBuilder().WithInterceptorFuncs(fakeApplyInterceptor()).
			WithScheme(scheme).
			WithStatusSubresource(&tamossv1alpha1.Tamoss{}, &tamossv1alpha1.TamossHibernate{}).
			WithObjects(tamoss, destination, hibernate).
			Build(),
		Scheme: scheme,
	}

	if _, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: hibernate.Name, Namespace: hibernate.Namespace}}); err != nil {
		t.Fatalf("expected unimplemented driver to fail without error, got %v", err)
	}

	updated := &tamossv1alpha1.TamossHibernate{}
	if err := reconciler.Client.Get(ctx, types.NamespacedName{Name: hibernate.Name, Namespace: hibernate.Namespace}, updated); err != nil {
		t.Fatalf("get updated TamossHibernate: %v", err)
	}
	if updated.Status.Phase != string(tamossv1alpha1.TamossOperationPhaseFailed) ||
		updated.Status.Reason != operatorstatus.ReasonUnsupportedProvider {
		t.Fatalf("expected unsupported driver failure, got %#v", updated.Status)
	}
}

func TestValidateHibernationDestinationPrefix(t *testing.T) {
	tests := []struct {
		name    string
		prefix  string
		wantErr bool
	}{
		{name: "default when omitted", prefix: ""},
		{name: "normalized", prefix: "hibernations/example"},
		{name: "leading slash", prefix: "/hibernations/example", wantErr: true},
		{name: "trailing slash", prefix: "hibernations/example/", wantErr: true},
		{name: "duplicate slash", prefix: "hibernations//example", wantErr: true},
		{name: "parent segment", prefix: "hibernations/../example", wantErr: true},
		{name: "leading whitespace", prefix: " hibernations/example", wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := validateHibernationDestinationPrefix(test.prefix)
			if test.wantErr && err == nil {
				t.Fatalf("expected prefix %q to be rejected", test.prefix)
			}
			if !test.wantErr && err != nil {
				t.Fatalf("expected prefix %q to be accepted, got %v", test.prefix, err)
			}
		})
	}
}

func hasFinalizer(finalizers []string, finalizer string) bool {
	for _, value := range finalizers {
		if value == finalizer {
			return true
		}
	}
	return false
}

func hibernateTestScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	scheme := storageBackendTestScheme(t)
	if err := appsv1.AddToScheme(scheme); err != nil {
		t.Fatalf("add appsv1 scheme: %v", err)
	}
	if err := autoscalingv2.AddToScheme(scheme); err != nil {
		t.Fatalf("add autoscalingv2 scheme: %v", err)
	}
	if err := cnpgv1.AddToScheme(scheme); err != nil {
		t.Fatalf("add cnpg scheme: %v", err)
	}
	return scheme
}

func finishHibernateSourceDeprovisioning(t *testing.T, ctx context.Context, reconciler *TamossHibernateReconciler, request ctrl.Request) {
	t.Helper()
	for range 2 {
		if _, err := reconciler.Reconcile(ctx, request); err != nil {
			t.Fatalf("finish hibernate source deprovisioning: %v", err)
		}
	}
}

func hibernateTamossFixture() *tamossv1alpha1.Tamoss {
	tamoss := tamossFixture()
	tamoss.UID = "tamoss-uid"
	tamoss.Spec.Backends.DB = tamossv1alpha1.DBBackendSpec{
		ProvidedBy: tamossv1alpha1.BackendProvidedByCNPG,
		CNPG:       &tamossv1alpha1.DBCNPGSpec{},
	}
	tamoss.Status.SchemaVersion = schemabundle.SchemaVersion
	operatorstatus.SetConditionBool(
		&tamoss.Status.Conditions,
		tamoss.Generation,
		operatorstatus.ConditionSchemaMigrated,
		true,
		operatorstatus.ReasonSchemaApplied,
		"Schema migration completed successfully",
	)
	return tamoss
}

func hibernateDestinationFixture() *tamossv1alpha1.StorageBackend {
	destination := storageBackendFixture()
	destination.Spec = externalStorageBackendSpecFixture()
	destination.Spec.Usage = tamossv1alpha1.StorageBackendUsageHibernate
	destination.Spec.Credentials = tamossv1alpha1.SecretReferenceSpec{
		ExistingSecret: "archive-s3",
		SecretKeys: tamossv1alpha1.SecretKeySpec{
			AccessKey: "accessKeyID",
			SecretKey: "secretAccessKey",
		},
	}
	destination.Status.Phase = operatorstatus.PhaseReady
	return destination
}

func hibernateFixture() *tamossv1alpha1.TamossHibernate {
	return &tamossv1alpha1.TamossHibernate{
		TypeMeta: metav1.TypeMeta{
			APIVersion: tamossv1alpha1.GroupVersion.String(),
			Kind:       "TamossHibernate",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:       "snap-1",
			Namespace:  "media",
			UID:        "hibernate-uid",
			Finalizers: []string{tamossHibernateFinalizer},
		},
		Spec: tamossv1alpha1.TamossHibernateSpec{
			TamossRef: tamossv1alpha1.TamossReferenceSpec{Name: "example"},
			Destination: tamossv1alpha1.HibernationDestinationSpec{
				StorageBackendRef: tamossv1alpha1.LocalObjectReference{Name: "archive"},
				Prefix:            "hibernate/example",
			},
		},
	}
}

func hibernateClusterFixture(tamoss *tamossv1alpha1.Tamoss) *cnpgv1.Cluster {
	controller := true
	return &cnpgv1.Cluster{
		TypeMeta: metav1.TypeMeta{
			APIVersion: cnpgv1.SchemeGroupVersion.String(),
			Kind:       "Cluster",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      tamoss.ResourceName("db"),
			Namespace: tamoss.Namespace,
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion: tamossv1alpha1.GroupVersion.String(),
				Kind:       "Tamoss",
				Name:       tamoss.Name,
				UID:        tamoss.UID,
				Controller: &controller,
			}},
		},
	}
}

func hibernateDeploymentFixture(tamoss *tamossv1alpha1.Tamoss, component string, replicas int32) *appsv1.Deployment {
	controller := true
	labels := map[string]string{
		"app.kubernetes.io/instance":  tamoss.Name,
		"app.kubernetes.io/component": component,
	}
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      tamoss.ResourceName(component),
			Namespace: tamoss.Namespace,
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion: tamossv1alpha1.GroupVersion.String(),
				Kind:       "Tamoss",
				Name:       tamoss.Name,
				UID:        tamoss.UID,
				Controller: &controller,
			}},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{MatchLabels: labels},
			Template: corev1.PodTemplateSpec{ObjectMeta: metav1.ObjectMeta{Labels: labels}},
		},
		Status: appsv1.DeploymentStatus{
			Replicas:          replicas,
			UpdatedReplicas:   replicas,
			ReadyReplicas:     replicas,
			AvailableReplicas: replicas,
		},
	}
}

func hibernatePodFixture(deployment *appsv1.Deployment) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      deployment.Name + "-pending-termination",
			Namespace: deployment.Namespace,
			Labels:    deployment.Spec.Template.Labels,
		},
		Status: corev1.PodStatus{Phase: corev1.PodRunning},
	}
}

func hibernateScheduledBackupFixture(tamoss *tamossv1alpha1.Tamoss) *cnpgv1.ScheduledBackup {
	controller := true
	return &cnpgv1.ScheduledBackup{
		ObjectMeta: metav1.ObjectMeta{
			Name:      tamoss.ResourceName("db-backup"),
			Namespace: tamoss.Namespace,
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion: tamossv1alpha1.GroupVersion.String(),
				Kind:       "Tamoss",
				Name:       tamoss.Name,
				UID:        tamoss.UID,
				Controller: &controller,
			}},
		},
		Spec: cnpgv1.ScheduledBackupSpec{
			Schedule: "0 0 2 * * *",
			Cluster:  cnpgv1.LocalObjectReference{Name: tamoss.ResourceName("db")},
		},
	}
}

func hibernateHPAFixture(tamoss *tamossv1alpha1.Tamoss, component string) *autoscalingv2.HorizontalPodAutoscaler {
	controller := true
	return &autoscalingv2.HorizontalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{
			Name:      tamoss.ResourceName(component),
			Namespace: tamoss.Namespace,
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion: tamossv1alpha1.GroupVersion.String(),
				Kind:       "Tamoss",
				Name:       tamoss.Name,
				UID:        tamoss.UID,
				Controller: &controller,
			}},
		},
	}
}

func hibernateCompletedBackupFixture(tamoss *tamossv1alpha1.Tamoss, hibernate *tamossv1alpha1.TamossHibernate) *cnpgv1.Backup {
	controller := true
	return &cnpgv1.Backup{
		TypeMeta: metav1.TypeMeta{
			APIVersion: cnpgv1.SchemeGroupVersion.String(),
			Kind:       "Backup",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      hibernate.Name,
			Namespace: hibernate.Namespace,
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion: tamossv1alpha1.GroupVersion.String(),
				Kind:       "TamossHibernate",
				Name:       hibernate.Name,
				UID:        hibernate.UID,
				Controller: &controller,
			}},
		},
		Spec: cnpgv1.BackupSpec{
			Cluster: cnpgv1.LocalObjectReference{Name: tamoss.ResourceName("db")},
			Method:  cnpgv1.BackupMethodBarmanObjectStore,
		},
		Status: cnpgv1.BackupStatus{
			Phase:           cnpgv1.BackupPhaseCompleted,
			DestinationPath: "s3://archive/hibernate/example/snap-1/cnpg",
			BackupID:        "20260707T100000",
		},
	}
}

type fakeHibernationManifestWriter struct {
	calls    int
	checksum string
	key      string
	manifest hibernationManifest
	err      error
}

func (f *fakeHibernationManifestWriter) Write(_ context.Context, _ string, _ tamossv1alpha1.StorageBackendSpec, key string, manifest hibernationManifest) (string, error) {
	f.calls++
	f.key = key
	f.manifest = manifest
	return f.checksum, f.err
}
