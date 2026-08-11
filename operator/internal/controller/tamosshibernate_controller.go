package controller

import (
	"context"
	"fmt"
	"path"
	"strings"
	"time"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	appsv1 "k8s.io/api/apps/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	tamossv1alpha1 "github.com/livewyer-ops/tamoss/operator/api/v1alpha1"
	"github.com/livewyer-ops/tamoss/operator/internal/controller/defaults"
	schemabundle "github.com/livewyer-ops/tamoss/operator/internal/schema"
	operatorstatus "github.com/livewyer-ops/tamoss/operator/internal/status"
)

type TamossHibernateReconciler struct {
	Client          client.Client
	Scheme          *runtime.Scheme
	Recorder        record.EventRecorder
	WatchNamespaces WatchNamespaceSet
	ManifestWriter  HibernationManifestWriter
	PollInterval    time.Duration
}

const tamossHibernateFinalizer = "tamosshibernate.tamoss.livewyer.io/finalizer"

//+kubebuilder:rbac:groups=tamoss.livewyer.io,resources=tamosshibernations,verbs=get;list;watch;update;patch
//+kubebuilder:rbac:groups=tamoss.livewyer.io,resources=tamosshibernations/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=tamoss.livewyer.io,resources=tamosshibernations/finalizers,verbs=update
//+kubebuilder:rbac:groups=tamoss.livewyer.io,resources=tamosses,verbs=get;list;watch
//+kubebuilder:rbac:groups=tamoss.livewyer.io,resources=tamosses/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=tamoss.livewyer.io,resources=storagebackends,verbs=get;list;watch
//+kubebuilder:rbac:groups="",namespace=system,resources=pods;secrets,verbs=get;list;watch
//+kubebuilder:rbac:groups=apps,namespace=system,resources=deployments,verbs=get;list;watch;update;patch
//+kubebuilder:rbac:groups=postgresql.cnpg.io,namespace=system,resources=clusters,verbs=get;list;watch;update;patch;delete
//+kubebuilder:rbac:groups=postgresql.cnpg.io,namespace=system,resources=backups,verbs=get;list;watch;create;update;patch
//+kubebuilder:rbac:groups=postgresql.cnpg.io,namespace=system,resources=scheduledbackups,verbs=get;list;watch;update;patch

func (r *TamossHibernateReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	start := time.Now()
	var result ctrl.Result
	var err error
	defer func() {
		recordControllerReconcile("tamosshibernate", result, err, time.Since(start))
	}()

	hibernate := &tamossv1alpha1.TamossHibernate{}
	if err = r.Client.Get(ctx, req.NamespacedName, hibernate); err != nil {
		if apierrors.IsNotFound(err) {
			err = nil
		}
		return result, err
	}
	if !r.WatchNamespaces.Allows(hibernate.Namespace) {
		log.FromContext(ctx).Info("ignoring TamossHibernate outside configured watch scope", "namespace", hibernate.Namespace)
		return result, nil
	}
	if !hibernate.DeletionTimestamp.IsZero() {
		result, err = r.finalizeHibernate(ctx, hibernate)
		return result, err
	}
	if !controllerutil.ContainsFinalizer(hibernate, tamossHibernateFinalizer) {
		original := hibernate.DeepCopy()
		controllerutil.AddFinalizer(hibernate, tamossHibernateFinalizer)
		err = r.Client.Patch(ctx, hibernate, client.MergeFrom(original))
		return result, err
	}
	if accepted, err := r.acceptOperationRetry(ctx, hibernate); err != nil {
		return result, err
	} else if accepted {
		result = ctrl.Result{RequeueAfter: r.pollInterval()}
		return result, nil
	}
	if hibernate.Status.Phase == string(tamossv1alpha1.TamossOperationPhaseCompleted) ||
		hibernate.Status.Phase == string(tamossv1alpha1.TamossOperationPhaseFailed) {
		return result, nil
	}

	tamoss, ok, err := r.resolveHibernateTamoss(ctx, hibernate)
	if err != nil || !ok {
		if err == nil {
			result = operationWaitResult(hibernate.Status.Phase, r.pollInterval())
		}
		return result, err
	}
	destination, ok, err := r.resolveHibernateDestination(ctx, hibernate)
	if err != nil || !ok {
		if err == nil {
			result = operationWaitResult(hibernate.Status.Phase, r.pollInterval())
		}
		return result, err
	}

	artifact, artifactErr := buildHibernateArtifact(hibernate, destination)
	if artifactErr != nil {
		return result, r.updateHibernateStatus(ctx, hibernate, tamossv1alpha1.TamossOperationPhaseFailed, operatorstatus.ReasonHibernateDestinationInvalid, artifactErr.Error(), artifact)
	}
	if compatibilityErr := hibernateCompatibilityError(tamoss, hibernate); compatibilityErr != nil {
		return result, r.updateHibernateStatus(ctx, hibernate, tamossv1alpha1.TamossOperationPhaseFailed, operatorstatus.ReasonUnsupportedProvider, compatibilityErr.Error(), artifact)
	}
	ready, statusErr := r.validateHibernateSourceSchema(ctx, tamoss, hibernate, artifact)
	if statusErr != nil || !ready {
		result = operationWaitResult(hibernate.Status.Phase, r.pollInterval())
		return result, statusErr
	}

	if hibernate.Status.Phase == string(tamossv1alpha1.TamossOperationPhaseDeprovisioningSource) {
		return r.reconcileHibernateSourceDeprovisioning(ctx, tamoss, hibernate)
	}

	cluster, ok, err := r.loadHibernateCNPGCluster(ctx, tamoss, hibernate, artifact)
	if err != nil || !ok {
		if err == nil {
			result = operationWaitResult(hibernate.Status.Phase, r.pollInterval())
		}
		return result, err
	}
	if ok, err := r.acquireHibernateLifecycle(ctx, tamoss, hibernate); err != nil || !ok {
		return result, err
	}
	if err := r.suspendHibernateScheduledBackup(ctx, tamoss); err != nil {
		return result, err
	}
	if err := r.ensureHibernateCNPGBackupConfiguration(ctx, cluster, hibernate, destination); err != nil {
		return result, err
	}
	quiesced, err := r.quiesceTamossWorkloads(ctx, tamoss)
	if err != nil {
		return result, err
	}
	if !quiesced {
		message := "Waiting for TAMOSS workloads to terminate before database capture"
		result = ctrl.Result{RequeueAfter: r.pollInterval()}
		return result, r.updateHibernateStatus(ctx, hibernate, tamossv1alpha1.TamossOperationPhaseQuiescing, operatorstatus.ReasonTamossHibernating, message, artifact)
	}

	result, err = r.reconcileHibernateCapture(ctx, tamoss, hibernate, cluster, destination, artifact)
	return result, err
}

func (r *TamossHibernateReconciler) reconcileHibernateCapture(ctx context.Context, tamoss *tamossv1alpha1.Tamoss, hibernate *tamossv1alpha1.TamossHibernate, cluster *cnpgv1.Cluster, destination *tamossv1alpha1.StorageBackend, artifact tamossv1alpha1.HibernationArtifactStatus) (ctrl.Result, error) {
	var result ctrl.Result
	backup, created, err := r.ensureHibernateCNPGBackup(ctx, hibernate, tamoss)
	if err != nil {
		return result, err
	}
	if !created && !metav1.IsControlledBy(backup, hibernate) {
		message := fmt.Sprintf("CNPG Backup %s already exists and is not owned by TamossHibernate %s", backup.Name, hibernate.Name)
		if err := r.setHibernateLifecycleFailed(ctx, tamoss, hibernate, operatorstatus.ReasonLifecycleOperationConflict, message); err != nil {
			return result, err
		}
		return result, r.updateHibernateStatus(ctx, hibernate, tamossv1alpha1.TamossOperationPhaseFailed, operatorstatus.ReasonLifecycleOperationConflict, message, artifact)
	}
	artifact.CNPGBackup = hibernateCNPGBackupStatus(backup)
	if artifact.CNPGBackup.DestinationPath == "" && cluster.Spec.Backup != nil && cluster.Spec.Backup.BarmanObjectStore != nil {
		artifact.CNPGBackup.DestinationPath = cluster.Spec.Backup.BarmanObjectStore.DestinationPath
	}
	if created {
		message := fmt.Sprintf("CNPG Backup %s was launched", backup.Name)
		result = ctrl.Result{RequeueAfter: r.pollInterval()}
		return result, r.updateHibernateStatus(ctx, hibernate, tamossv1alpha1.TamossOperationPhaseCapturingDatabase, operatorstatus.ReasonTamossHibernating, message, artifact)
	}
	switch backup.Status.Phase {
	case cnpgv1.BackupPhaseCompleted:
		written, writeErr := r.writeHibernateManifest(ctx, tamoss, destination, artifact)
		if writeErr != nil {
			message := fmt.Sprintf("Hibernation manifest upload failed, retrying: %v", writeErr)
			result = ctrl.Result{RequeueAfter: r.pollInterval()}
			return result, r.updateHibernateStatus(ctx, hibernate, tamossv1alpha1.TamossOperationPhaseWritingManifest, operatorstatus.ReasonHibernateManifestUploadFailed, message, artifact)
		}
		artifact = written
		result = ctrl.Result{RequeueAfter: r.pollInterval()}
		return result, r.updateHibernateStatus(ctx, hibernate, tamossv1alpha1.TamossOperationPhaseDeprovisioningSource, operatorstatus.ReasonSourceDeprovisioning, "Hibernation artifact is committed; source database compute will be removed", artifact)
	case cnpgv1.BackupPhaseFailed:
		message := hibernateCNPGBackupFailureMessage(backup)
		if err := r.setHibernateLifecycleFailed(ctx, tamoss, hibernate, operatorstatus.ReasonBackupPolicyFailed, message); err != nil {
			return result, err
		}
		return result, r.updateHibernateStatus(ctx, hibernate, tamossv1alpha1.TamossOperationPhaseFailed, operatorstatus.ReasonBackupPolicyFailed, message, artifact)
	default:
		message := fmt.Sprintf("CNPG Backup %s is %s", backup.Name, backupPhaseOrPending(backup))
		result = ctrl.Result{RequeueAfter: r.pollInterval()}
		return result, r.updateHibernateStatus(ctx, hibernate, tamossv1alpha1.TamossOperationPhaseCapturingDatabase, operatorstatus.ReasonTamossHibernating, message, artifact)
	}
}

func buildHibernateArtifact(hibernate *tamossv1alpha1.TamossHibernate, destination *tamossv1alpha1.StorageBackend) (tamossv1alpha1.HibernationArtifactStatus, error) {
	artifact := tamossv1alpha1.HibernationArtifactStatus{
		Driver:      string(hibernateDriver(hibernate)),
		ManifestKey: hibernateManifestKey(hibernate),
	}
	if err := validateHibernationDestinationPrefix(hibernate.Spec.Destination.Prefix); err != nil {
		return artifact, err
	}
	destinationSpec := storageBackendFromDestination(destination)
	if destinationSpec.BucketName != "" {
		artifact.ManifestURI = fmt.Sprintf("s3://%s/%s", destinationSpec.BucketName, artifact.ManifestKey)
	}
	return artifact, nil
}

func hibernateCompatibilityError(tamoss *tamossv1alpha1.Tamoss, hibernate *tamossv1alpha1.TamossHibernate) error {
	if hibernateDriver(hibernate) != tamossv1alpha1.HibernationDriverCNPGPhysical {
		return fmt.Errorf("hibernate driver %q is not supported yet", hibernateDriver(hibernate))
	}
	if tamoss.Spec.Backends.DB.Provider() != tamossv1alpha1.BackendProvidedByCNPG {
		return fmt.Errorf("cnpgPhysical hibernate requires a managed CNPG database backend")
	}
	if tamoss.Spec.Backends.S3.Provider() != tamossv1alpha1.S3BackendProvidedByExternal {
		return fmt.Errorf("portable hibernation requires an external S3 media backend; managed RustFS media is not included in the database artifact")
	}
	return nil
}

func (r *TamossHibernateReconciler) reconcileHibernateSourceDeprovisioning(ctx context.Context, tamoss *tamossv1alpha1.Tamoss, hibernate *tamossv1alpha1.TamossHibernate) (ctrl.Result, error) {
	artifact := hibernate.Status.Artifact
	deprovisioned, err := r.deprovisionHibernateSource(ctx, tamoss)
	if err != nil {
		return ctrl.Result{}, err
	}
	if !deprovisioned {
		result := ctrl.Result{RequeueAfter: r.pollInterval()}
		return result, r.updateHibernateStatus(ctx, hibernate, tamossv1alpha1.TamossOperationPhaseDeprovisioningSource, operatorstatus.ReasonSourceDeprovisioning, "Waiting for the source CNPG cluster to be removed", artifact)
	}
	if err := r.completeHibernateLifecycle(ctx, tamoss, hibernate); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, r.updateHibernateStatus(ctx, hibernate, tamossv1alpha1.TamossOperationPhaseCompleted, operatorstatus.ReasonTamossHibernated, "TAMOSS hibernation artifact is committed and source database compute is removed", artifact)
}

func (r *TamossHibernateReconciler) finalizeHibernate(ctx context.Context, hibernate *tamossv1alpha1.TamossHibernate) (ctrl.Result, error) {
	if !controllerutil.ContainsFinalizer(hibernate, tamossHibernateFinalizer) {
		return ctrl.Result{}, nil
	}
	committed := hibernate.Status.Phase == string(tamossv1alpha1.TamossOperationPhaseCompleted)
	if hibernate.Status.Phase == string(tamossv1alpha1.TamossOperationPhaseDeprovisioningSource) && hibernate.Status.Artifact.Checksum != "" {
		tamoss := &tamossv1alpha1.Tamoss{}
		key := types.NamespacedName{Name: hibernate.Spec.TamossRef.Name, Namespace: hibernate.Namespace}
		if err := r.Client.Get(ctx, key, tamoss); err != nil {
			if !apierrors.IsNotFound(err) {
				return ctrl.Result{}, err
			}
		} else {
			if !hibernateLifecycleOwnedBy(tamoss, hibernate) {
				return ctrl.Result{}, fmt.Errorf("tamoss %s no longer holds committed hibernation %s", tamoss.Name, hibernate.Name)
			}
			deprovisioned, err := r.deprovisionHibernateSource(ctx, tamoss)
			if err != nil {
				return ctrl.Result{}, err
			}
			if !deprovisioned {
				return ctrl.Result{RequeueAfter: r.pollInterval()}, nil
			}
			if err := r.completeHibernateLifecycle(ctx, tamoss, hibernate); err != nil {
				return ctrl.Result{}, err
			}
		}
		committed = true
	}
	if !committed {
		message := fmt.Sprintf("TamossHibernate %s was deleted before completion", hibernate.Name)
		if err := r.failActiveHibernateLifecycle(ctx, hibernate, operatorstatus.ReasonLifecycleOperationDeleted, message); err != nil {
			return ctrl.Result{}, err
		}
		if hibernate.Status.Phase != string(tamossv1alpha1.TamossOperationPhaseFailed) {
			if err := r.updateHibernateStatus(ctx, hibernate, tamossv1alpha1.TamossOperationPhaseFailed, operatorstatus.ReasonLifecycleOperationDeleted, message, hibernate.Status.Artifact); err != nil {
				return ctrl.Result{}, err
			}
		}
	}
	original := hibernate.DeepCopy()
	controllerutil.RemoveFinalizer(hibernate, tamossHibernateFinalizer)
	return ctrl.Result{}, r.Client.Patch(ctx, hibernate, client.MergeFrom(original))
}

func (r *TamossHibernateReconciler) failActiveHibernateLifecycle(ctx context.Context, hibernate *tamossv1alpha1.TamossHibernate, reason, message string) error {
	if hibernate.Spec.TamossRef.Name == "" {
		return nil
	}
	tamoss := &tamossv1alpha1.Tamoss{}
	key := types.NamespacedName{Name: hibernate.Spec.TamossRef.Name, Namespace: hibernate.Namespace}
	if err := r.Client.Get(ctx, key, tamoss); err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return err
	}
	if !operationRefMatches(tamoss.Status.Lifecycle.ActiveOperationRef, hibernate, "TamossHibernate") {
		return nil
	}
	return patchTamossLifecycleStatus(ctx, r.Client, tamoss, func(lifecycle *tamossv1alpha1.TamossLifecycleStatus) {
		setLifecycleOperationState(lifecycle, tamossv1alpha1.TamossLifecyclePhaseFailed, reason, message, nil)
		lifecycle.LastHibernateRef = operationObjectReference(hibernate, "TamossHibernate")
	})
}

func (r *TamossHibernateReconciler) resolveHibernateTamoss(ctx context.Context, hibernate *tamossv1alpha1.TamossHibernate) (*tamossv1alpha1.Tamoss, bool, error) {
	if hibernate.Spec.TamossRef.Name == "" {
		return nil, false, r.updateHibernateStatus(ctx, hibernate, tamossv1alpha1.TamossOperationPhaseFailed, operatorstatus.ReasonTamossNotFound, "spec.tamossRef.name is required", tamossv1alpha1.HibernationArtifactStatus{})
	}
	tamoss := &tamossv1alpha1.Tamoss{}
	key := types.NamespacedName{Name: hibernate.Spec.TamossRef.Name, Namespace: hibernate.Namespace}
	if err := r.Client.Get(ctx, key, tamoss); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, false, r.updateHibernateStatus(ctx, hibernate, tamossv1alpha1.TamossOperationPhaseResolvingSource, operatorstatus.ReasonTamossNotFound, fmt.Sprintf("Referenced Tamoss %s was not found", key.Name), tamossv1alpha1.HibernationArtifactStatus{})
		}
		return nil, false, err
	}
	resolved := tamoss.DeepCopy()
	defaults.Apply(resolved)
	return resolved, true, nil
}

func (r *TamossHibernateReconciler) resolveHibernateDestination(ctx context.Context, hibernate *tamossv1alpha1.TamossHibernate) (*tamossv1alpha1.StorageBackend, bool, error) {
	if hibernate.Spec.Destination.StorageBackendRef.Name == "" {
		return nil, false, r.updateHibernateStatus(ctx, hibernate, tamossv1alpha1.TamossOperationPhaseFailed, operatorstatus.ReasonHibernateDestinationInvalid, "spec.destination.storageBackendRef.name is required", tamossv1alpha1.HibernationArtifactStatus{})
	}
	storageBackend := &tamossv1alpha1.StorageBackend{}
	key := types.NamespacedName{Name: hibernate.Spec.Destination.StorageBackendRef.Name, Namespace: hibernate.Namespace}
	if err := r.Client.Get(ctx, key, storageBackend); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, false, r.updateHibernateStatus(ctx, hibernate, tamossv1alpha1.TamossOperationPhasePreparingTarget, operatorstatus.ReasonStorageBackendNotReady, fmt.Sprintf("Hibernate destination StorageBackend %s was not found", key.Name), tamossv1alpha1.HibernationArtifactStatus{})
		}
		return nil, false, err
	}
	spec := storageBackend.Spec
	spec.ApplyDefaults(storageBackend.Namespace, storageBackend.Name)
	if !spec.IsHibernateDestination() {
		return nil, false, r.updateHibernateStatus(ctx, hibernate, tamossv1alpha1.TamossOperationPhaseFailed, operatorstatus.ReasonHibernateDestinationInvalid, fmt.Sprintf("StorageBackend %s must set spec.usage=hibernate", storageBackend.Name), tamossv1alpha1.HibernationArtifactStatus{})
	}
	if !spec.IsExternalObjectStore() {
		return nil, false, r.updateHibernateStatus(ctx, hibernate, tamossv1alpha1.TamossOperationPhaseFailed, operatorstatus.ReasonHibernateDestinationInvalid, fmt.Sprintf("StorageBackend %s must use provider external-s3", storageBackend.Name), tamossv1alpha1.HibernationArtifactStatus{})
	}
	if storageBackend.Status.Phase != operatorstatus.PhaseReady {
		return nil, false, r.updateHibernateStatus(ctx, hibernate, tamossv1alpha1.TamossOperationPhasePreparingTarget, operatorstatus.ReasonStorageBackendNotReady, fmt.Sprintf("Hibernate destination StorageBackend %s is not ready", storageBackend.Name), tamossv1alpha1.HibernationArtifactStatus{})
	}
	return storageBackend, true, nil
}

func hibernateDriver(hibernate *tamossv1alpha1.TamossHibernate) tamossv1alpha1.HibernationDriver {
	if hibernate.Spec.Driver == "" {
		return tamossv1alpha1.HibernationDriverCNPGPhysical
	}
	return hibernate.Spec.Driver
}

func hibernateManifestKey(hibernate *tamossv1alpha1.TamossHibernate) string {
	return path.Join(hibernateDestinationPrefix(hibernate), hibernate.Name, "manifest.json")
}

func hibernateDestinationPrefix(hibernate *tamossv1alpha1.TamossHibernate) string {
	if hibernate.Spec.Destination.Prefix != "" {
		return hibernate.Spec.Destination.Prefix
	}
	return "hibernations/" + hibernate.Spec.TamossRef.Name
}

func validateHibernationDestinationPrefix(prefix string) error {
	trimmed := strings.TrimSpace(prefix)
	if trimmed != prefix {
		return fmt.Errorf("spec.destination.prefix must not have leading or trailing whitespace")
	}
	prefix = trimmed
	if prefix == "" {
		return nil
	}
	if strings.HasPrefix(prefix, "/") || strings.HasSuffix(prefix, "/") || path.Clean(prefix) != prefix {
		return fmt.Errorf("spec.destination.prefix %q must be a normalized relative object-key prefix", prefix)
	}
	for _, segment := range strings.Split(prefix, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return fmt.Errorf("spec.destination.prefix %q contains an unsafe path segment", prefix)
		}
	}
	return nil
}

func (r *TamossHibernateReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&tamossv1alpha1.TamossHibernate{}, builder.WithPredicates(primaryResourcePredicate(tamossHibernateFinalizer, []string{AnnotationOperationRetry}))).
		Complete(r)
}

// acceptOperationRetry re-arms a Failed operation when the operation-retry
// annotation carries a value that has not been honoured yet, mirroring the
// schema-retry recovery flow.
func (r *TamossHibernateReconciler) acceptOperationRetry(ctx context.Context, hibernate *tamossv1alpha1.TamossHibernate) (bool, error) {
	value := strings.TrimSpace(hibernate.Annotations[AnnotationOperationRetry])
	if value == "" || hibernate.Status.Phase != string(tamossv1alpha1.TamossOperationPhaseFailed) {
		return false, nil
	}
	if hibernate.Status.AcceptedRetry == value {
		return false, nil
	}
	original := hibernate.DeepCopy()
	hibernate.Status.Phase = ""
	hibernate.Status.Reason = operatorstatus.ReasonReconciling
	hibernate.Status.Message = fmt.Sprintf("Retry %q accepted; the operation will run again", value)
	hibernate.Status.CompletedAt = nil
	hibernate.Status.StartedAt = nil
	hibernate.Status.AcceptedRetry = value
	if err := r.Client.Status().Patch(ctx, hibernate, client.MergeFromWithOptions(original, client.MergeFromWithOptimisticLock{})); err != nil {
		return false, err
	}
	operatorstatus.EmitNormalEvent(r.Recorder, hibernate, operatorstatus.ReasonReconciling, hibernate.Status.Message)
	return true, nil
}

func (r *TamossHibernateReconciler) loadHibernateCNPGCluster(ctx context.Context, tamoss *tamossv1alpha1.Tamoss, hibernate *tamossv1alpha1.TamossHibernate, artifact tamossv1alpha1.HibernationArtifactStatus) (*cnpgv1.Cluster, bool, error) {
	cluster := &cnpgv1.Cluster{}
	key := types.NamespacedName{Name: tamoss.ResourceName("db"), Namespace: tamoss.Namespace}
	if err := r.Client.Get(ctx, key, cluster); err != nil {
		if apierrors.IsNotFound(err) {
			statusErr := r.updateHibernateStatus(ctx, hibernate, tamossv1alpha1.TamossOperationPhasePreparingTarget, operatorstatus.ReasonClusterNotReady, fmt.Sprintf("CNPG Cluster %s was not found", key.Name), artifact)
			return nil, false, statusErr
		}
		return nil, false, err
	}
	return cluster, true, nil
}

func (r *TamossHibernateReconciler) acquireHibernateLifecycle(ctx context.Context, tamoss *tamossv1alpha1.Tamoss, hibernate *tamossv1alpha1.TamossHibernate) (bool, error) {
	if tamoss.Status.Lifecycle.ActiveOperationRef != nil && !operationRefMatches(tamoss.Status.Lifecycle.ActiveOperationRef, hibernate, "TamossHibernate") {
		message := fmt.Sprintf("Tamoss %s already has active lifecycle operation %s/%s", tamoss.Name, tamoss.Status.Lifecycle.ActiveOperationRef.Kind, tamoss.Status.Lifecycle.ActiveOperationRef.Name)
		return false, r.updateHibernateStatus(ctx, hibernate, tamossv1alpha1.TamossOperationPhaseFailed, operatorstatus.ReasonLifecycleOperationConflict, message, tamossv1alpha1.HibernationArtifactStatus{})
	}
	if tamoss.Status.Lifecycle.Phase == string(tamossv1alpha1.TamossLifecyclePhaseHibernated) {
		if operationRefMatches(tamoss.Status.Lifecycle.LastHibernateRef, hibernate, "TamossHibernate") {
			return true, nil
		}
		message := fmt.Sprintf("Tamoss %s is already hibernated", tamoss.Name)
		return false, r.updateHibernateStatus(ctx, hibernate, tamossv1alpha1.TamossOperationPhaseFailed, operatorstatus.ReasonLifecycleOperationConflict, message, tamossv1alpha1.HibernationArtifactStatus{})
	}
	if err := patchTamossLifecycleStatus(ctx, r.Client, tamoss, func(lifecycle *tamossv1alpha1.TamossLifecycleStatus) {
		setLifecycleOperationState(lifecycle,
			tamossv1alpha1.TamossLifecyclePhaseHibernating,
			operatorstatus.ReasonTamossHibernating,
			fmt.Sprintf("TamossHibernate %s is creating a hibernation artifact", hibernate.Name),
			operationObjectReference(hibernate, "TamossHibernate"))
	}); err != nil {
		return false, err
	}
	return true, nil
}

func hibernateLifecycleOwnedBy(tamoss *tamossv1alpha1.Tamoss, hibernate *tamossv1alpha1.TamossHibernate) bool {
	if operationRefMatches(tamoss.Status.Lifecycle.ActiveOperationRef, hibernate, "TamossHibernate") {
		return true
	}
	return tamoss.Status.Lifecycle.Phase == string(tamossv1alpha1.TamossLifecyclePhaseHibernated) &&
		operationRefMatches(tamoss.Status.Lifecycle.LastHibernateRef, hibernate, "TamossHibernate")
}

func (r *TamossHibernateReconciler) validateHibernateSourceSchema(ctx context.Context, tamoss *tamossv1alpha1.Tamoss, hibernate *tamossv1alpha1.TamossHibernate, artifact tamossv1alpha1.HibernationArtifactStatus) (bool, error) {
	if hibernateLifecycleOwnedBy(tamoss, hibernate) {
		return true, nil
	}
	condition := meta.FindStatusCondition(tamoss.Status.Conditions, operatorstatus.ConditionSchemaMigrated)
	if condition == nil || condition.Status != metav1.ConditionTrue {
		message := fmt.Sprintf("Tamoss %s is waiting for schema migration before hibernation", tamoss.Name)
		return false, r.updateHibernateStatus(ctx, hibernate, tamossv1alpha1.TamossOperationPhaseResolvingSource, operatorstatus.ReasonSchemaNotReady, message, artifact)
	}
	version := strings.TrimSpace(tamoss.Status.SchemaVersion)
	if version == "" || !schemabundle.IsSupportedStartingVersion(version) {
		message := fmt.Sprintf("Tamoss %s schema version %q is not supported for hibernation", tamoss.Name, version)
		return false, r.updateHibernateStatus(ctx, hibernate, tamossv1alpha1.TamossOperationPhaseFailed, operatorstatus.ReasonUnsupportedSchemaVersion, message, artifact)
	}
	return true, nil
}

func (r *TamossHibernateReconciler) ensureHibernateCNPGBackupConfiguration(ctx context.Context, cluster *cnpgv1.Cluster, hibernate *tamossv1alpha1.TamossHibernate, destination *tamossv1alpha1.StorageBackend) error {
	spec := storageBackendFromDestination(destination)
	original := cluster.DeepCopy()
	store := hibernateBarmanObjectStore(cluster.Name, hibernate, spec)
	cluster.Spec.Backup = &cnpgv1.BackupConfiguration{
		BarmanObjectStore: &store,
	}
	return r.Client.Patch(ctx, cluster, client.MergeFrom(original))
}

func (r *TamossHibernateReconciler) suspendHibernateScheduledBackup(ctx context.Context, tamoss *tamossv1alpha1.Tamoss) error {
	scheduled := &cnpgv1.ScheduledBackup{}
	key := types.NamespacedName{Name: tamoss.ResourceName("db-backup"), Namespace: tamoss.Namespace}
	if err := r.Client.Get(ctx, key, scheduled); err != nil {
		return client.IgnoreNotFound(err)
	}
	if !metav1.IsControlledBy(scheduled, tamoss) {
		return fmt.Errorf("CNPG ScheduledBackup %s is not owned by Tamoss %s", scheduled.Name, tamoss.Name)
	}
	if scheduled.Spec.Suspend != nil && *scheduled.Spec.Suspend {
		return nil
	}
	original := scheduled.DeepCopy()
	suspended := true
	scheduled.Spec.Suspend = &suspended
	return r.Client.Patch(ctx, scheduled, client.MergeFrom(original))
}

func hibernateBarmanObjectStore(serverName string, hibernate *tamossv1alpha1.TamossHibernate, spec tamossv1alpha1.StorageBackendSpec) cnpgv1.BarmanObjectStoreConfiguration {
	return cnpgv1.BarmanObjectStoreConfiguration{
		EndpointURL:     spec.Endpoint.Default.URL,
		DestinationPath: fmt.Sprintf("s3://%s/%s", spec.BucketName, path.Join(hibernateDestinationPrefix(hibernate), hibernate.Name, "cnpg")),
		ServerName:      serverName,
		BarmanCredentials: cnpgv1.BarmanCredentials{
			AWS: &cnpgv1.S3Credentials{
				AccessKeyIDReference:     hibernateSecretKey(spec.Credentials.ExistingSecret, storageBackendAccessKey(spec)),
				SecretAccessKeyReference: hibernateSecretKey(spec.Credentials.ExistingSecret, storageBackendSecretKey(spec)),
			},
		},
	}
}

func hibernateSecretKey(secretName, key string) *cnpgv1.SecretKeySelector {
	return &cnpgv1.SecretKeySelector{
		LocalObjectReference: cnpgv1.LocalObjectReference{Name: secretName},
		Key:                  key,
	}
}

func (r *TamossHibernateReconciler) quiesceTamossWorkloads(ctx context.Context, tamoss *tamossv1alpha1.Tamoss) (bool, error) {
	quiesced := true
	for _, component := range []string{"worker", componentAPI, "ui", "console"} {
		autoscaler := &autoscalingv2.HorizontalPodAutoscaler{}
		key := types.NamespacedName{Name: tamoss.ResourceName(component), Namespace: tamoss.Namespace}
		if err := r.Client.Get(ctx, key, autoscaler); err == nil {
			if !metav1.IsControlledBy(autoscaler, tamoss) {
				return false, fmt.Errorf("HorizontalPodAutoscaler %s is not owned by Tamoss %s", autoscaler.Name, tamoss.Name)
			}
			if err := r.Client.Delete(ctx, autoscaler); err != nil && !apierrors.IsNotFound(err) {
				return false, err
			}
			quiesced = false
		} else if !apierrors.IsNotFound(err) {
			return false, err
		}

		deployment := &appsv1.Deployment{}
		if err := r.Client.Get(ctx, key, deployment); err != nil {
			if apierrors.IsNotFound(err) {
				continue
			}
			return false, err
		}
		if !metav1.IsControlledBy(deployment, tamoss) {
			return false, fmt.Errorf("deployment %s is not owned by Tamoss %s", deployment.Name, tamoss.Name)
		}
		current := int32(1)
		if deployment.Spec.Replicas != nil {
			current = *deployment.Spec.Replicas
		}
		if current != 0 {
			original := deployment.DeepCopy()
			deployment.Spec.Replicas = new(int32)
			if err := r.Client.Patch(ctx, deployment, client.MergeFrom(original)); err != nil {
				return false, err
			}
			quiesced = false
		}
		if deployment.Status.Replicas != 0 || deployment.Status.ReadyReplicas != 0 ||
			deployment.Status.AvailableReplicas != 0 || deployment.Status.UpdatedReplicas != 0 ||
			(deployment.Status.TerminatingReplicas != nil && *deployment.Status.TerminatingReplicas != 0) {
			quiesced = false
		}
		selector, err := metav1.LabelSelectorAsSelector(deployment.Spec.Selector)
		if err != nil {
			return false, fmt.Errorf("resolve Deployment %s pod selector: %w", deployment.Name, err)
		}
		pods := &corev1.PodList{}
		if err := r.Client.List(ctx, pods, client.InNamespace(tamoss.Namespace), client.MatchingLabelsSelector{Selector: selector}); err != nil {
			return false, err
		}
		for i := range pods.Items {
			switch pods.Items[i].Status.Phase {
			case corev1.PodSucceeded, corev1.PodFailed:
			default:
				quiesced = false
			}
		}
	}
	return quiesced, nil
}

func (r *TamossHibernateReconciler) deprovisionHibernateSource(ctx context.Context, tamoss *tamossv1alpha1.Tamoss) (bool, error) {
	cluster := &cnpgv1.Cluster{}
	key := types.NamespacedName{Name: tamoss.ResourceName("db"), Namespace: tamoss.Namespace}
	if err := r.Client.Get(ctx, key, cluster); err != nil {
		if apierrors.IsNotFound(err) {
			return true, nil
		}
		return false, err
	}
	if !metav1.IsControlledBy(cluster, tamoss) {
		return false, fmt.Errorf("CNPG Cluster %s is not owned by Tamoss %s", cluster.Name, tamoss.Name)
	}
	if cluster.DeletionTimestamp.IsZero() {
		propagation := metav1.DeletePropagationForeground
		if err := r.Client.Delete(ctx, cluster, client.PropagationPolicy(propagation)); err != nil && !apierrors.IsNotFound(err) {
			return false, err
		}
	}
	return false, nil
}

func (r *TamossHibernateReconciler) ensureHibernateCNPGBackup(ctx context.Context, hibernate *tamossv1alpha1.TamossHibernate, tamoss *tamossv1alpha1.Tamoss) (*cnpgv1.Backup, bool, error) {
	backup := &cnpgv1.Backup{}
	key := types.NamespacedName{Name: hibernate.Name, Namespace: hibernate.Namespace}
	if err := r.Client.Get(ctx, key, backup); err != nil {
		if !apierrors.IsNotFound(err) {
			return nil, false, err
		}
		backup = &cnpgv1.Backup{
			TypeMeta: metav1.TypeMeta{
				APIVersion: cnpgv1.SchemeGroupVersion.String(),
				Kind:       "Backup",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      hibernate.Name,
				Namespace: hibernate.Namespace,
				Labels: map[string]string{
					"app.kubernetes.io/name":                    tamossAppName,
					appInstanceLabel:                            tamoss.Name,
					appComponentLabel:                           "hibernate",
					"app.kubernetes.io/managed-by":              "tamoss-operator",
					"tamoss.livewyer.io/tamosshibernate":        hibernate.Name,
					"tamoss.livewyer.io/tamosshibernate-driver": string(hibernateDriver(hibernate)),
				},
			},
			Spec: cnpgv1.BackupSpec{
				Cluster: cnpgv1.LocalObjectReference{Name: tamoss.ResourceName("db")},
				Method:  cnpgv1.BackupMethodBarmanObjectStore,
			},
		}
		if err := controllerutil.SetControllerReference(hibernate, backup, r.Scheme); err != nil {
			return nil, false, err
		}
		if err := r.Client.Create(ctx, backup); err != nil {
			return nil, false, err
		}
		return backup, true, nil
	}
	return backup, false, nil
}

func hibernateCNPGBackupStatus(backup *cnpgv1.Backup) tamossv1alpha1.HibernationCNPGBackupStatus {
	return tamossv1alpha1.HibernationCNPGBackupStatus{
		Name:            backup.Name,
		Namespace:       backup.Namespace,
		Phase:           string(backup.Status.Phase),
		DestinationPath: backup.Status.DestinationPath,
		BackupID:        backup.Status.BackupID,
		Error:           backup.Status.Error,
	}
}

func (r *TamossHibernateReconciler) writeHibernateManifest(ctx context.Context, tamoss *tamossv1alpha1.Tamoss, storageBackend *tamossv1alpha1.StorageBackend, artifact tamossv1alpha1.HibernationArtifactStatus) (tamossv1alpha1.HibernationArtifactStatus, error) {
	spec := storageBackendFromDestination(storageBackend)
	manifest := buildHibernationManifest(tamoss, storageBackend, spec, artifact, tamoss.ResourceName("db"))
	checksum, err := r.manifestWriter().Write(ctx, storageBackend.Namespace, spec, artifact.ManifestKey, manifest)
	if err != nil {
		return artifact, err
	}
	artifact.Checksum = checksum
	return artifact, nil
}

func (r *TamossHibernateReconciler) completeHibernateLifecycle(ctx context.Context, tamoss *tamossv1alpha1.Tamoss, hibernate *tamossv1alpha1.TamossHibernate) error {
	return patchTamossLifecycleStatus(ctx, r.Client, tamoss, func(lifecycle *tamossv1alpha1.TamossLifecycleStatus) {
		setLifecycleOperationState(lifecycle,
			tamossv1alpha1.TamossLifecyclePhaseHibernated,
			operatorstatus.ReasonTamossHibernated,
			fmt.Sprintf("TamossHibernate %s completed and TAMOSS is hibernated", hibernate.Name),
			nil)
		lifecycle.LastHibernateRef = operationObjectReference(hibernate, "TamossHibernate")
	})
}

func (r *TamossHibernateReconciler) setHibernateLifecycleFailed(ctx context.Context, tamoss *tamossv1alpha1.Tamoss, hibernate *tamossv1alpha1.TamossHibernate, reason, message string) error {
	return patchTamossLifecycleStatus(ctx, r.Client, tamoss, func(lifecycle *tamossv1alpha1.TamossLifecycleStatus) {
		setLifecycleOperationState(lifecycle, tamossv1alpha1.TamossLifecyclePhaseFailed, reason, message, nil)
		lifecycle.LastHibernateRef = operationObjectReference(hibernate, "TamossHibernate")
	})
}

func (r *TamossHibernateReconciler) manifestWriter() HibernationManifestWriter {
	if r.ManifestWriter != nil {
		return r.ManifestWriter
	}
	return S3HibernationManifestWriter{Client: r.Client}
}

func (r *TamossHibernateReconciler) pollInterval() time.Duration {
	if r.PollInterval > 0 {
		return r.PollInterval
	}
	return defaultProviderReadinessProbeInterval
}

func storageBackendFromDestination(storageBackend *tamossv1alpha1.StorageBackend) tamossv1alpha1.StorageBackendSpec {
	spec := storageBackend.Spec
	spec.ApplyDefaults(storageBackend.Namespace, storageBackend.Name)
	return spec
}

func backupPhaseOrPending(backup *cnpgv1.Backup) string {
	if backup.Status.Phase == "" {
		return string(cnpgv1.BackupPhasePending)
	}
	return string(backup.Status.Phase)
}

func hibernateCNPGBackupFailureMessage(backup *cnpgv1.Backup) string {
	if backup.Status.Error != "" {
		return fmt.Sprintf("CNPG Backup %s failed: %s", backup.Name, backup.Status.Error)
	}
	if backup.Status.CommandError != "" {
		return fmt.Sprintf("CNPG Backup %s failed: %s", backup.Name, backup.Status.CommandError)
	}
	return fmt.Sprintf("CNPG Backup %s failed", backup.Name)
}
