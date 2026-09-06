package controller

import (
	"context"
	"fmt"
	"path"
	"strings"
	"time"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	tamossv1alpha1 "github.com/livewyer-ops/tamoss/operator/api/v1alpha1"
	"github.com/livewyer-ops/tamoss/operator/internal/controller/backend/rustfs"
	operatorstatus "github.com/livewyer-ops/tamoss/operator/internal/status"
)

// hibernationCleanupRetryInterval spaces out retries of transient artifact
// deletion failures so a broken endpoint does not produce a hot loop.
const hibernationCleanupRetryInterval = time.Minute

type HibernationArtifactCleaner interface {
	DeletePrefix(ctx context.Context, namespace string, spec tamossv1alpha1.StorageBackendSpec, prefix string) (int64, error)
}

type S3HibernationArtifactCleaner struct {
	Client       client.Client
	BucketClient rustfs.BucketClient
}

func (c S3HibernationArtifactCleaner) DeletePrefix(ctx context.Context, namespace string, spec tamossv1alpha1.StorageBackendSpec, prefix string) (int64, error) {
	creds, err := storageBackendCredentials(ctx, c.Client, namespace, spec)
	if err != nil {
		return 0, err
	}
	bucketClient := c.BucketClient
	if bucketClient == nil {
		bucketClient = rustfs.S3BucketClient{}
	}
	result, err := bucketClient.DeletePrefix(ctx, rustfs.BucketTarget{
		EndpointURL: spec.Endpoint.Default.URL,
		BucketName:  spec.BucketName,
		Region:      spec.Region,
	}, rustfs.BucketCredentials{
		AccessKey: creds.AccessKey,
		SecretKey: creds.SecretKey,
	}, prefix)
	if err != nil {
		return 0, err
	}
	return result.ObjectsDeleted, nil
}

// restoredDatabaseReady reports whether the CNPG cluster that consumed the
// restore bootstrap is ready.
func (r *TamossReconciler) restoredDatabaseReady(ctx context.Context, tamoss *tamossv1alpha1.Tamoss) (bool, error) {
	cluster := &cnpgv1.Cluster{}
	err := r.Client.Get(ctx, types.NamespacedName{Name: tamoss.ResourceName("db"), Namespace: tamoss.Namespace}, cluster)
	if apierrors.IsNotFound(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	ready := meta.FindStatusCondition(cluster.Status.Conditions, string(cnpgv1.ConditionClusterReady))
	return ready != nil && ready.Status == metav1.ConditionTrue, nil
}

// reconcileResumeArtifactRetention applies the source artifact's retention
// policy once the restored database is ready. The artifact has served its
// purpose when the database it carried runs again; application readiness is
// ordinary reconciliation and must not hold retention hostage. The restore
// completion time is recorded first so a crash between recording and
// deleting can never lose the fact that the artifact was consumed.
func (r *TamossReconciler) reconcileResumeArtifactRetention(ctx context.Context, tamoss *tamossv1alpha1.Tamoss) (ctrl.Result, error) {
	var result ctrl.Result
	if restore := tamoss.Status.Lifecycle.ResolvedRestore; restore != nil {
		var err error
		result, err = r.reconcileArtifactRetention(ctx, tamoss, &restore.HibernationArtifactRetention)
		if err != nil {
			return result, err
		}
	}
	for _, restore := range tamoss.Status.Lifecycle.PendingArtifactCleanups {
		pending, err := r.reconcileArtifactRetention(ctx, tamoss, &restore)
		if err != nil {
			return result, err
		}
		if pending.RequeueAfter > 0 && (result.RequeueAfter == 0 || pending.RequeueAfter < result.RequeueAfter) {
			result.RequeueAfter = pending.RequeueAfter
		}
	}
	return result, nil
}

func (r *TamossReconciler) reconcileArtifactRetention(ctx context.Context, tamoss *tamossv1alpha1.Tamoss, restore *tamossv1alpha1.HibernationArtifactRetention) (ctrl.Result, error) {
	if restore == nil || hibernationArtifactCleanupTerminal(restore.Cleanup.Phase) {
		return ctrl.Result{}, nil
	}
	if restore.ResumedAt == nil {
		ready, readyErr := r.restoredDatabaseReady(ctx, tamoss)
		if readyErr != nil {
			return ctrl.Result{}, readyErr
		}
		if !ready {
			return ctrl.Result{}, nil
		}
		now := metav1.Now()
		err := patchTamossLifecycleStatus(ctx, r.Client, tamoss, func(lifecycle *tamossv1alpha1.TamossLifecycleStatus) {
			if lifecycle.ResolvedRestore == nil {
				return
			}
			lifecycle.ResolvedRestore.ResumedAt = &now
			if tamossv1alpha1.TamossLifecyclePhase(lifecycle.Phase) == tamossv1alpha1.TamossLifecyclePhaseResuming {
				setLifecycleOperationState(lifecycle,
					tamossv1alpha1.TamossLifecyclePhaseRunning,
					operatorstatus.ReasonTamossReady,
					fmt.Sprintf("Restore from hibernation artifact %s completed", lifecycle.ResolvedRestore.ManifestKey),
					nil)
			}
		})
		if err != nil {
			return ctrl.Result{}, err
		}
		operatorstatus.EmitNormalEvent(r.Recorder, tamoss, operatorstatus.ReasonTamossReady,
			fmt.Sprintf("Restore from hibernation artifact %s completed", restore.ManifestKey))
		// Record first, delete on a later reconcile.
		return ctrl.Result{RequeueAfter: defaultProviderReadinessProbeInterval}, nil
	}

	storageBackend := &tamossv1alpha1.StorageBackend{}
	err := r.Client.Get(ctx, types.NamespacedName{Name: restore.StorageBackendName, Namespace: tamoss.Namespace}, storageBackend)
	switch {
	case apierrors.IsNotFound(err):
		return ctrl.Result{}, r.recordArtifactCleanup(ctx, tamoss, restore,
			blockedArtifactCleanup(fmt.Sprintf("Artifact StorageBackend %s was not found for cleanup", restore.StorageBackendName)))
	case err != nil:
		return ctrl.Result{}, err
	}
	spec := storageBackend.Spec
	spec.ApplyDefaults(storageBackend.Namespace, storageBackend.Name)
	if !spec.IsHibernateDestination() || !spec.IsExternalObjectStore() {
		return ctrl.Result{}, r.recordArtifactCleanup(ctx, tamoss, restore,
			blockedArtifactCleanup(fmt.Sprintf("Artifact StorageBackend %s must be an external-s3 hibernate destination for cleanup", storageBackend.Name)))
	}

	switch spec.Hibernate.Retention.Mode {
	case "", tamossv1alpha1.HibernationRetentionModeRetain:
		return ctrl.Result{}, r.recordArtifactCleanup(ctx, tamoss, restore, tamossv1alpha1.HibernationArtifactCleanupStatus{
			Phase:   string(tamossv1alpha1.HibernationArtifactCleanupPhaseRetained),
			Reason:  operatorstatus.ReasonArtifactCleanupRetained,
			Message: "Hibernation artifact retained by StorageBackend policy",
		})
	case tamossv1alpha1.HibernationRetentionModeDeleteAfterResume:
		return r.deleteResumeArtifact(ctx, tamoss, storageBackend.Namespace, spec, restore)
	case tamossv1alpha1.HibernationRetentionModeTTL:
		if spec.Hibernate.Retention.TTLSecondsAfterResume <= 0 {
			return ctrl.Result{}, r.recordArtifactCleanup(ctx, tamoss, restore,
				blockedArtifactCleanup("hibernate.retention.ttlSecondsAfterResume must be greater than zero when retention mode is TTL"))
		}
		dueAt := restore.ResumedAt.Add(time.Duration(spec.Hibernate.Retention.TTLSecondsAfterResume) * time.Second)
		if wait := time.Until(dueAt); wait > 0 {
			err := r.recordArtifactCleanup(ctx, tamoss, restore, tamossv1alpha1.HibernationArtifactCleanupStatus{
				Phase:   string(tamossv1alpha1.HibernationArtifactCleanupPhasePending),
				Reason:  operatorstatus.ReasonArtifactCleanupPending,
				Message: fmt.Sprintf("Hibernation artifact cleanup is scheduled for %s", dueAt.UTC().Format(time.RFC3339)),
			})
			return ctrl.Result{RequeueAfter: wait}, err
		}
		return r.deleteResumeArtifact(ctx, tamoss, storageBackend.Namespace, spec, restore)
	default:
		return ctrl.Result{}, r.recordArtifactCleanup(ctx, tamoss, restore,
			blockedArtifactCleanup(fmt.Sprintf("unsupported hibernation retention mode %q", spec.Hibernate.Retention.Mode)))
	}
}

func (r *TamossReconciler) deleteResumeArtifact(ctx context.Context, tamoss *tamossv1alpha1.Tamoss, namespace string, spec tamossv1alpha1.StorageBackendSpec, restore *tamossv1alpha1.HibernationArtifactRetention) (ctrl.Result, error) {
	prefix, err := hibernationArtifactRootPrefix(restore.ManifestKey)
	if err != nil {
		return ctrl.Result{}, r.recordArtifactCleanup(ctx, tamoss, restore, blockedArtifactCleanup(err.Error()))
	}
	objectsDeleted, err := r.artifactCleaner().DeletePrefix(ctx, namespace, spec, prefix)
	if err != nil {
		message := fmt.Sprintf("Hibernation artifact cleanup for prefix %s failed, retrying: %v", prefix, err)
		if recordErr := r.recordArtifactCleanup(ctx, tamoss, restore, tamossv1alpha1.HibernationArtifactCleanupStatus{
			Phase:   string(tamossv1alpha1.HibernationArtifactCleanupPhasePending),
			Reason:  operatorstatus.ReasonArtifactCleanupRetrying,
			Message: message,
		}); recordErr != nil {
			return ctrl.Result{}, recordErr
		}
		return ctrl.Result{RequeueAfter: hibernationCleanupRetryInterval}, nil
	}
	now := metav1.Now()
	cleanup := tamossv1alpha1.HibernationArtifactCleanupStatus{
		Phase:          string(tamossv1alpha1.HibernationArtifactCleanupPhaseCompleted),
		Reason:         operatorstatus.ReasonArtifactCleanupComplete,
		Message:        fmt.Sprintf("Deleted hibernation artifact prefix %s", prefix),
		ObjectsDeleted: objectsDeleted,
		CompletedAt:    &now,
	}
	if err := r.recordArtifactCleanup(ctx, tamoss, restore, cleanup); err != nil {
		return ctrl.Result{}, err
	}
	operatorstatus.EmitNormalEvent(r.Recorder, tamoss, operatorstatus.ReasonArtifactCleanupComplete, cleanup.Message)
	return ctrl.Result{}, nil
}

// recordArtifactCleanup persists the cleanup state and emits a warning event
// once per distinct blocked or retrying condition.
func (r *TamossReconciler) recordArtifactCleanup(ctx context.Context, tamoss *tamossv1alpha1.Tamoss, restore *tamossv1alpha1.HibernationArtifactRetention, cleanup tamossv1alpha1.HibernationArtifactCleanupStatus) error {
	previous := restore.Cleanup
	if err := patchTamossLifecycleStatus(ctx, r.Client, tamoss, func(lifecycle *tamossv1alpha1.TamossLifecycleStatus) {
		current := lifecycle.ResolvedRestore
		if current != nil && current.ManifestKey == restore.ManifestKey && current.StorageBackendName == restore.StorageBackendName {
			current.Cleanup = cleanup
			return
		}
		for i := range lifecycle.PendingArtifactCleanups {
			pending := &lifecycle.PendingArtifactCleanups[i]
			if pending.ManifestKey != restore.ManifestKey || pending.StorageBackendName != restore.StorageBackendName {
				continue
			}
			if hibernationArtifactCleanupFinished(cleanup.Phase) {
				lifecycle.PendingArtifactCleanups = append(lifecycle.PendingArtifactCleanups[:i], lifecycle.PendingArtifactCleanups[i+1:]...)
			} else {
				pending.Cleanup = cleanup
			}
			return
		}
	}); err != nil {
		return err
	}
	warning := cleanup.Reason == operatorstatus.ReasonArtifactCleanupBlocked || cleanup.Reason == operatorstatus.ReasonArtifactCleanupRetrying
	if warning && (previous.Reason != cleanup.Reason || previous.Message != cleanup.Message) {
		r.recordWarning(tamoss, cleanup.Reason, cleanup.Message)
	}
	return nil
}

func (r *TamossReconciler) artifactCleaner() HibernationArtifactCleaner {
	if r.ArtifactCleaner != nil {
		return r.ArtifactCleaner
	}
	return S3HibernationArtifactCleaner{Client: r.Client}
}

func blockedArtifactCleanup(message string) tamossv1alpha1.HibernationArtifactCleanupStatus {
	return tamossv1alpha1.HibernationArtifactCleanupStatus{
		Phase:   string(tamossv1alpha1.HibernationArtifactCleanupPhaseBlocked),
		Reason:  operatorstatus.ReasonArtifactCleanupBlocked,
		Message: message,
	}
}

func hibernationArtifactCleanupTerminal(phase string) bool {
	switch phase {
	case string(tamossv1alpha1.HibernationArtifactCleanupPhaseRetained),
		string(tamossv1alpha1.HibernationArtifactCleanupPhaseCompleted),
		string(tamossv1alpha1.HibernationArtifactCleanupPhaseBlocked):
		return true
	default:
		return false
	}
}

func hibernationArtifactCleanupFinished(phase string) bool {
	return phase == string(tamossv1alpha1.HibernationArtifactCleanupPhaseRetained) ||
		phase == string(tamossv1alpha1.HibernationArtifactCleanupPhaseCompleted)
}

func hibernationArtifactRootPrefix(manifestKey string) (string, error) {
	key := strings.TrimSpace(manifestKey)
	if key == "" {
		return "", fmt.Errorf("resume artifact cleanup requires a manifest key")
	}
	if strings.HasPrefix(key, "/") {
		return "", fmt.Errorf("resume artifact manifest key %q must be relative", manifestKey)
	}
	if path.Clean(key) != key {
		return "", fmt.Errorf("resume artifact manifest key %q must be normalized", manifestKey)
	}
	if !strings.HasSuffix(key, "/manifest.json") {
		return "", fmt.Errorf("resume artifact manifest key %q must end with /manifest.json", manifestKey)
	}
	root := strings.TrimSuffix(key, "/manifest.json")
	if root == "" || root == "." || root == "/" {
		return "", fmt.Errorf("resume artifact manifest key %q does not identify an artifact prefix", manifestKey)
	}
	for _, part := range strings.Split(root, "/") {
		if part == "" || part == "." || part == ".." {
			return "", fmt.Errorf("resume artifact manifest key %q contains an unsafe path segment", manifestKey)
		}
	}
	return root + "/", nil
}
