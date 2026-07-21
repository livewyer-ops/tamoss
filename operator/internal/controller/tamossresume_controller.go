package controller

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
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
	cnpgbackend "github.com/livewyer-ops/tamoss/operator/internal/controller/backend/cnpg"
	"github.com/livewyer-ops/tamoss/operator/internal/controller/defaults"
	schemabundle "github.com/livewyer-ops/tamoss/operator/internal/schema"
	operatorstatus "github.com/livewyer-ops/tamoss/operator/internal/status"
)

type TamossResumeReconciler struct {
	Client          client.Client
	Scheme          *runtime.Scheme
	Recorder        record.EventRecorder
	WatchNamespaces WatchNamespaceSet
	ManifestReader  HibernationManifestReader
	ArtifactCleaner HibernationArtifactCleaner
	PollInterval    time.Duration
}

const (
	tamossResumeOperationAnnotation    = "tamoss.livewyer.io/resume-operation"
	tamossResumeOperationUIDAnnotation = "tamoss.livewyer.io/resume-operation-uid"
	tamossResumeFinalizer              = "tamossresume.tamoss.livewyer.io/finalizer"
)

var errHibernationManifestSchemaUnsupported = errors.New("hibernation manifest schema is unsupported")

//+kubebuilder:rbac:groups=tamoss.livewyer.io,resources=tamossresumes,verbs=get;list;watch;update;patch
//+kubebuilder:rbac:groups=tamoss.livewyer.io,resources=tamossresumes/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=tamoss.livewyer.io,resources=tamossresumes/finalizers,verbs=update
//+kubebuilder:rbac:groups=tamoss.livewyer.io,resources=tamosses,verbs=get;list;watch
//+kubebuilder:rbac:groups=tamoss.livewyer.io,resources=tamosses/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=tamoss.livewyer.io,resources=tamosshibernations,verbs=get;list;watch
//+kubebuilder:rbac:groups=tamoss.livewyer.io,resources=storagebackends,verbs=get;list;watch
//+kubebuilder:rbac:groups="",namespace=system,resources=secrets,verbs=get;list;watch
//+kubebuilder:rbac:groups=postgresql.cnpg.io,namespace=system,resources=clusters,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=postgresql.cnpg.io,namespace=system,resources=scheduledbackups,verbs=get;list;watch;create;update;patch

func (r *TamossResumeReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	start := time.Now()
	var result ctrl.Result
	var err error
	defer func() {
		recordControllerReconcile("tamossresume", result, err, time.Since(start))
	}()

	resume := &tamossv1alpha1.TamossResume{}
	if err = r.Client.Get(ctx, req.NamespacedName, resume); err != nil {
		if apierrors.IsNotFound(err) {
			err = nil
		}
		return result, err
	}
	if !r.WatchNamespaces.Allows(resume.Namespace) {
		log.FromContext(ctx).Info("ignoring TamossResume outside configured watch scope", "namespace", resume.Namespace)
		return result, nil
	}
	if !resume.DeletionTimestamp.IsZero() {
		result, err = r.finalizeResume(ctx, resume)
		return result, err
	}
	if !controllerutil.ContainsFinalizer(resume, tamossResumeFinalizer) {
		original := resume.DeepCopy()
		controllerutil.AddFinalizer(resume, tamossResumeFinalizer)
		err = r.Client.Patch(ctx, resume, client.MergeFrom(original))
		return result, err
	}
	if resume.Status.Phase == string(tamossv1alpha1.TamossOperationPhaseCompleted) {
		result, err = r.reconcileCompletedResumeRetention(ctx, resume)
		return result, err
	}
	if resume.Status.Phase == string(tamossv1alpha1.TamossOperationPhaseFailed) {
		return result, nil
	}

	tamoss, ok, err := r.resolveResumeTamoss(ctx, resume)
	if err != nil || !ok {
		if err == nil {
			result = operationWaitResult(resume.Status.Phase, r.pollInterval())
		}
		return result, err
	}
	source, ok, err := r.resolveResumeSource(ctx, resume)
	if err != nil || !ok {
		if err == nil {
			result = operationWaitResult(resume.Status.Phase, r.pollInterval())
		}
		return result, err
	}
	artifact := source.Artifact
	if tamoss.Spec.Backends.DB.Provider() != tamossv1alpha1.BackendProvidedByCNPG {
		return result, r.updateResumeStatus(ctx, resume, tamossv1alpha1.TamossOperationPhaseFailed, operatorstatus.ReasonUnsupportedProvider, "Resume currently requires a managed CNPG database backend", artifact)
	}
	if tamoss.Spec.Backends.S3.Provider() != tamossv1alpha1.S3BackendProvidedByExternal {
		message := "portable resume requires an external S3 media backend; managed RustFS media is not included in the hibernation artifact"
		return result, r.updateResumeStatus(ctx, resume, tamossv1alpha1.TamossOperationPhaseFailed, operatorstatus.ReasonUnsupportedProvider, message, artifact)
	}

	manifest, checksum, ok, err := r.readResumeManifest(ctx, resume, source)
	if err != nil || !ok {
		if err == nil {
			result = operationWaitResult(resume.Status.Phase, r.pollInterval())
		}
		return result, err
	}
	artifact = artifactFromResumeManifest(source, manifest)
	artifact.Checksum = checksum
	if resume.Status.Phase == string(tamossv1alpha1.TamossOperationPhaseStartingServices) ||
		resume.Status.Phase == string(tamossv1alpha1.TamossOperationPhaseVerifying) {
		result, err = r.reconcileResumeServices(ctx, tamoss, resume, source.StorageBackend, artifact)
		return result, err
	}
	if ok, err := r.acquireResumeLifecycle(ctx, tamoss, resume); err != nil || !ok {
		return result, err
	}

	if ok, err := r.validateResumeCNPGTarget(ctx, tamoss, resume, artifact); err != nil || !ok {
		return result, err
	}
	condition, err := r.ensureResumeCNPGCluster(ctx, tamoss, resume, source.StorageBackend, manifest)
	if err != nil {
		return result, err
	}
	if condition.Status != metav1.ConditionTrue {
		result = ctrl.Result{RequeueAfter: r.pollInterval()}
		message := condition.Message
		if message == "" {
			message = fmt.Sprintf("CNPG Cluster %s is recovering", tamoss.ResourceName("db"))
		}
		return result, r.updateResumeStatus(ctx, resume, tamossv1alpha1.TamossOperationPhaseRecoveringDatabase, condition.Reason, message, artifact)
	}

	if err := r.startResumeServicesLifecycle(ctx, tamoss, resume); err != nil {
		return result, err
	}
	result = ctrl.Result{RequeueAfter: r.pollInterval()}
	return result, r.updateResumeStatus(ctx, resume, tamossv1alpha1.TamossOperationPhaseStartingServices, operatorstatus.ReasonTamossResuming, "Database recovery completed; waiting for TAMOSS services to become ready", artifact)
}

func (r *TamossResumeReconciler) finalizeResume(ctx context.Context, resume *tamossv1alpha1.TamossResume) (ctrl.Result, error) {
	if !controllerutil.ContainsFinalizer(resume, tamossResumeFinalizer) {
		return ctrl.Result{}, nil
	}
	if resume.Status.Phase != string(tamossv1alpha1.TamossOperationPhaseCompleted) {
		clean, err := r.cleanupAbortedResumeCluster(ctx, resume)
		if err != nil {
			return ctrl.Result{}, err
		}
		if !clean {
			return ctrl.Result{RequeueAfter: r.pollInterval()}, nil
		}
		message := fmt.Sprintf("TamossResume %s was deleted before completion", resume.Name)
		if err := r.failActiveResumeLifecycle(ctx, resume, operatorstatus.ReasonLifecycleOperationDeleted, message); err != nil {
			return ctrl.Result{}, err
		}
		if resume.Status.Phase != string(tamossv1alpha1.TamossOperationPhaseFailed) {
			if err := r.updateResumeStatus(ctx, resume, tamossv1alpha1.TamossOperationPhaseFailed, operatorstatus.ReasonLifecycleOperationDeleted, message, resume.Status.Artifact); err != nil {
				return ctrl.Result{}, err
			}
		}
	}
	original := resume.DeepCopy()
	controllerutil.RemoveFinalizer(resume, tamossResumeFinalizer)
	return ctrl.Result{}, r.Client.Patch(ctx, resume, client.MergeFrom(original))
}

func (r *TamossResumeReconciler) cleanupAbortedResumeCluster(ctx context.Context, resume *tamossv1alpha1.TamossResume) (bool, error) {
	if resume.Spec.TamossRef.Name == "" {
		return true, nil
	}
	tamoss := &tamossv1alpha1.Tamoss{}
	tamossKey := types.NamespacedName{Name: resume.Spec.TamossRef.Name, Namespace: resume.Namespace}
	if err := r.Client.Get(ctx, tamossKey, tamoss); err != nil {
		if apierrors.IsNotFound(err) {
			return true, nil
		}
		return false, err
	}
	cluster := &cnpgv1.Cluster{}
	clusterKey := types.NamespacedName{Name: tamoss.ResourceName("db"), Namespace: tamoss.Namespace}
	if err := r.Client.Get(ctx, clusterKey, cluster); err != nil {
		if apierrors.IsNotFound(err) {
			return true, nil
		}
		return false, err
	}
	if !resumeOperationMatches(cluster, resume) {
		return true, nil
	}
	if cluster.DeletionTimestamp.IsZero() {
		propagation := metav1.DeletePropagationForeground
		if err := r.Client.Delete(ctx, cluster, client.PropagationPolicy(propagation)); err != nil && !apierrors.IsNotFound(err) {
			return false, err
		}
	}
	return false, nil
}

func (r *TamossResumeReconciler) failActiveResumeLifecycle(ctx context.Context, resume *tamossv1alpha1.TamossResume, reason, message string) error {
	if resume.Spec.TamossRef.Name == "" {
		return nil
	}
	tamoss := &tamossv1alpha1.Tamoss{}
	key := types.NamespacedName{Name: resume.Spec.TamossRef.Name, Namespace: resume.Namespace}
	if err := r.Client.Get(ctx, key, tamoss); err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return err
	}
	if !operationRefMatches(tamoss.Status.Lifecycle.ActiveOperationRef, resume, "TamossResume") {
		return nil
	}
	return patchTamossLifecycleStatus(ctx, r.Client, tamoss, func(lifecycle *tamossv1alpha1.TamossLifecycleStatus) {
		setLifecycleOperationState(lifecycle, tamossv1alpha1.TamossLifecyclePhaseFailed, reason, message, nil)
		lifecycle.LastResumeRef = operationObjectReference(resume, "TamossResume")
	})
}

func (r *TamossResumeReconciler) resolveResumeTamoss(ctx context.Context, resume *tamossv1alpha1.TamossResume) (*tamossv1alpha1.Tamoss, bool, error) {
	if resume.Spec.TamossRef.Name == "" {
		return nil, false, r.updateResumeStatus(ctx, resume, tamossv1alpha1.TamossOperationPhaseFailed, operatorstatus.ReasonTamossNotFound, "spec.tamossRef.name is required", tamossv1alpha1.HibernationArtifactStatus{})
	}
	tamoss := &tamossv1alpha1.Tamoss{}
	key := types.NamespacedName{Name: resume.Spec.TamossRef.Name, Namespace: resume.Namespace}
	if err := r.Client.Get(ctx, key, tamoss); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, false, r.updateResumeStatus(ctx, resume, tamossv1alpha1.TamossOperationPhasePreparingTarget, operatorstatus.ReasonTamossNotFound, fmt.Sprintf("Target Tamoss %s was not found", key.Name), tamossv1alpha1.HibernationArtifactStatus{})
		}
		return nil, false, err
	}
	resolved := tamoss.DeepCopy()
	defaults.Apply(resolved)
	return resolved, true, nil
}

type resumeSourceResolution struct {
	Artifact       tamossv1alpha1.HibernationArtifactStatus
	StorageBackend *tamossv1alpha1.StorageBackend
}

func (r *TamossResumeReconciler) resolveResumeSource(ctx context.Context, resume *tamossv1alpha1.TamossResume) (resumeSourceResolution, bool, error) {
	switch {
	case resume.Spec.Source.HibernationRef != nil && resume.Spec.Source.HibernationRef.Name != "":
		return r.resolveResumeHibernationRef(ctx, resume, resume.Spec.Source.HibernationRef.Name)
	case resume.Spec.Source.Artifact != nil:
		return r.resolveResumeArtifact(ctx, resume, *resume.Spec.Source.Artifact)
	default:
		err := r.updateResumeStatus(ctx, resume, tamossv1alpha1.TamossOperationPhaseFailed, operatorstatus.ReasonHibernateSourceInvalid, "spec.source must set hibernationRef or artifact", tamossv1alpha1.HibernationArtifactStatus{})
		return resumeSourceResolution{}, false, err
	}
}

func (r *TamossResumeReconciler) resolveResumeHibernationRef(ctx context.Context, resume *tamossv1alpha1.TamossResume, name string) (resumeSourceResolution, bool, error) {
	hibernate := &tamossv1alpha1.TamossHibernate{}
	key := types.NamespacedName{Name: name, Namespace: resume.Namespace}
	if err := r.Client.Get(ctx, key, hibernate); err != nil {
		if apierrors.IsNotFound(err) {
			err := r.updateResumeStatus(ctx, resume, tamossv1alpha1.TamossOperationPhaseResolvingSource, operatorstatus.ReasonHibernateSourceInvalid, fmt.Sprintf("TamossHibernate %s was not found", name), tamossv1alpha1.HibernationArtifactStatus{})
			return resumeSourceResolution{}, false, err
		}
		return resumeSourceResolution{}, false, err
	}
	if hibernate.Status.Phase == string(tamossv1alpha1.TamossOperationPhaseFailed) {
		message := fmt.Sprintf("TamossHibernate %s has failed and cannot be resumed; create a new hibernation", name)
		err := r.updateResumeStatus(ctx, resume, tamossv1alpha1.TamossOperationPhaseFailed, operatorstatus.ReasonHibernateSourceInvalid, message, hibernate.Status.Artifact)
		return resumeSourceResolution{}, false, err
	}
	if hibernate.Status.Phase != string(tamossv1alpha1.TamossOperationPhaseCompleted) {
		message := fmt.Sprintf("TamossHibernate %s is %q, not Completed", name, hibernate.Status.Phase)
		err := r.updateResumeStatus(ctx, resume, tamossv1alpha1.TamossOperationPhaseResolvingSource, operatorstatus.ReasonHibernateSourceInvalid, message, hibernate.Status.Artifact)
		return resumeSourceResolution{}, false, err
	}
	artifact := hibernate.Status.Artifact
	if artifact.ManifestKey == "" {
		artifact.ManifestKey = hibernateManifestKey(hibernate)
	}
	if artifact.Checksum == "" {
		message := fmt.Sprintf("TamossHibernate %s has no trusted manifest checksum", name)
		err := r.updateResumeStatus(ctx, resume, tamossv1alpha1.TamossOperationPhaseFailed, operatorstatus.ReasonHibernateSourceInvalid, message, artifact)
		return resumeSourceResolution{}, false, err
	}
	storageBackend, ok, err := r.resolveResumeStorageBackend(ctx, resume, hibernate.Spec.Destination.StorageBackendRef.Name, artifact)
	if err != nil || !ok {
		return resumeSourceResolution{}, ok, err
	}
	return resumeSourceResolution{Artifact: artifact, StorageBackend: storageBackend}, true, nil
}

func (r *TamossResumeReconciler) resolveResumeArtifact(ctx context.Context, resume *tamossv1alpha1.TamossResume, source tamossv1alpha1.TamossResumeArtifactSource) (resumeSourceResolution, bool, error) {
	artifact := tamossv1alpha1.HibernationArtifactStatus{
		ManifestKey: source.ManifestKey,
		Checksum:    source.Checksum,
	}
	if source.StorageBackendRef.Name == "" || source.ManifestKey == "" || source.Checksum == "" {
		err := r.updateResumeStatus(ctx, resume, tamossv1alpha1.TamossOperationPhaseFailed, operatorstatus.ReasonHibernateSourceInvalid, "artifact source requires storageBackendRef.name, manifestKey, and checksum", artifact)
		return resumeSourceResolution{}, false, err
	}
	storageBackend, ok, err := r.resolveResumeStorageBackend(ctx, resume, source.StorageBackendRef.Name, artifact)
	if err != nil || !ok {
		return resumeSourceResolution{}, ok, err
	}
	return resumeSourceResolution{Artifact: artifact, StorageBackend: storageBackend}, true, nil
}

func (r *TamossResumeReconciler) resolveResumeStorageBackend(ctx context.Context, resume *tamossv1alpha1.TamossResume, name string, artifact tamossv1alpha1.HibernationArtifactStatus) (*tamossv1alpha1.StorageBackend, bool, error) {
	if name == "" {
		err := r.updateResumeStatus(ctx, resume, tamossv1alpha1.TamossOperationPhaseFailed, operatorstatus.ReasonHibernateSourceInvalid, "resume source requires a hibernate StorageBackend reference", artifact)
		return nil, false, err
	}
	storageBackend := &tamossv1alpha1.StorageBackend{}
	key := types.NamespacedName{Name: name, Namespace: resume.Namespace}
	if err := r.Client.Get(ctx, key, storageBackend); err != nil {
		if apierrors.IsNotFound(err) {
			err := r.updateResumeStatus(ctx, resume, tamossv1alpha1.TamossOperationPhaseResolvingSource, operatorstatus.ReasonStorageBackendNotReady, fmt.Sprintf("Artifact StorageBackend %s was not found", key.Name), artifact)
			return nil, false, err
		}
		return nil, false, err
	}
	spec := storageBackend.Spec
	spec.ApplyDefaults(storageBackend.Namespace, storageBackend.Name)
	if !spec.IsHibernateDestination() || !spec.IsExternalObjectStore() {
		err := r.updateResumeStatus(ctx, resume, tamossv1alpha1.TamossOperationPhaseFailed, operatorstatus.ReasonHibernateSourceInvalid, fmt.Sprintf("Artifact StorageBackend %s must be an external-s3 hibernate destination", storageBackend.Name), artifact)
		return nil, false, err
	}
	if storageBackend.Status.Phase != operatorstatus.PhaseReady {
		err := r.updateResumeStatus(ctx, resume, tamossv1alpha1.TamossOperationPhaseResolvingSource, operatorstatus.ReasonStorageBackendNotReady, fmt.Sprintf("Artifact StorageBackend %s is not ready", storageBackend.Name), artifact)
		return nil, false, err
	}
	return storageBackend, true, nil
}

func (r *TamossResumeReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&tamossv1alpha1.TamossResume{}, builder.WithPredicates(primaryResourcePredicate(tamossResumeFinalizer, nil))).
		Complete(r)
}

func (r *TamossResumeReconciler) readResumeManifest(ctx context.Context, resume *tamossv1alpha1.TamossResume, source resumeSourceResolution) (hibernationManifest, string, bool, error) {
	spec := storageBackendFromDestination(source.StorageBackend)
	artifact := source.Artifact
	if artifact.ManifestURI == "" && spec.BucketName != "" && artifact.ManifestKey != "" {
		artifact.ManifestURI = fmt.Sprintf("s3://%s/%s", spec.BucketName, artifact.ManifestKey)
	}
	manifest, checksum, err := r.manifestReader().Read(ctx, source.StorageBackend.Namespace, spec, source.Artifact.ManifestKey)
	if err != nil {
		phase := tamossv1alpha1.TamossOperationPhaseFailed
		reason := resumeManifestReadFailureReason(err)
		message := err.Error()
		if !isPermanentHibernationManifestReadError(err) {
			phase = tamossv1alpha1.TamossOperationPhaseResolvingSource
			reason = operatorstatus.ReasonHibernateManifestUnavailable
			message = fmt.Sprintf("Hibernation manifest read failed, retrying: %v", err)
		}
		statusErr := r.updateResumeStatus(ctx, resume, phase, reason, message, artifact)
		if statusErr != nil {
			return hibernationManifest{}, "", false, statusErr
		}
		return hibernationManifest{}, "", false, nil
	}
	if checksum == "" {
		message := "hibernation manifest reader did not return a SHA-256 checksum"
		statusErr := r.updateResumeStatus(ctx, resume, tamossv1alpha1.TamossOperationPhaseFailed, operatorstatus.ReasonHibernateManifestChecksumMismatch, message, artifact)
		if statusErr != nil {
			return hibernationManifest{}, "", false, statusErr
		}
		return hibernationManifest{}, "", false, nil
	}
	if source.Artifact.Checksum != checksum {
		message := fmt.Sprintf("hibernation manifest checksum mismatch: source %s, computed %s", source.Artifact.Checksum, checksum)
		artifact.Checksum = checksum
		statusErr := r.updateResumeStatus(ctx, resume, tamossv1alpha1.TamossOperationPhaseFailed, operatorstatus.ReasonHibernateManifestChecksumMismatch, message, artifact)
		if statusErr != nil {
			return hibernationManifest{}, "", false, statusErr
		}
		return hibernationManifest{}, "", false, nil
	}
	if err := validateResumeManifest(manifest, source.Artifact.ManifestKey); err != nil {
		reason := operatorstatus.ReasonHibernateSourceInvalid
		if errors.Is(err, errHibernationManifestSchemaUnsupported) {
			reason = operatorstatus.ReasonUnsupportedSchemaVersion
		}
		statusErr := r.updateResumeStatus(ctx, resume, tamossv1alpha1.TamossOperationPhaseFailed, reason, err.Error(), artifact)
		if statusErr != nil {
			return hibernationManifest{}, "", false, statusErr
		}
		return hibernationManifest{}, "", false, nil
	}
	return manifest, checksum, true, nil
}

func resumeManifestReadFailureReason(err error) string {
	if errors.Is(err, errHibernationManifestChecksumMismatch) {
		return operatorstatus.ReasonHibernateManifestChecksumMismatch
	}
	return operatorstatus.ReasonHibernateSourceInvalid
}

func validateResumeManifest(manifest hibernationManifest, manifestKey string) error {
	if manifest.Schema.ManifestKind != "TamossHibernate" {
		return fmt.Errorf("hibernation manifest schema kind %q is not supported", manifest.Schema.ManifestKind)
	}
	schemaVersion := strings.TrimSpace(manifest.Schema.Version)
	if schemaVersion == "" {
		return fmt.Errorf("%w: schema.version is required", errHibernationManifestSchemaUnsupported)
	}
	if !schemabundle.IsSupportedStartingVersion(schemaVersion) {
		return fmt.Errorf("%w: schema version %q is not supported by this operator", errHibernationManifestSchemaUnsupported, schemaVersion)
	}
	tamsAPI := strings.TrimSpace(manifest.Schema.TAMSAPI)
	if tamsAPI != schemabundle.SupportedTAMSAPIVersion {
		return fmt.Errorf("%w: TAMS API version %q is not supported; expected %q", errHibernationManifestSchemaUnsupported, tamsAPI, schemabundle.SupportedTAMSAPIVersion)
	}
	if manifest.Driver != string(tamossv1alpha1.HibernationDriverCNPGPhysical) {
		return fmt.Errorf("hibernation manifest driver %q is not supported for resume", manifest.Driver)
	}
	if manifest.Artifact.ManifestKey != "" && manifest.Artifact.ManifestKey != manifestKey {
		return fmt.Errorf("hibernation manifest key %q does not match requested key %q", manifest.Artifact.ManifestKey, manifestKey)
	}
	if manifest.CNPG.DestinationPath == "" {
		return fmt.Errorf("hibernation manifest is missing cnpg.destinationPath")
	}
	if manifest.CNPG.BackupID == "" {
		return fmt.Errorf("hibernation manifest is missing cnpg.backupID")
	}
	if manifest.CNPG.Phase != string(cnpgv1.BackupPhaseCompleted) {
		return fmt.Errorf("hibernation manifest CNPG backup phase %q is not completed", manifest.CNPG.Phase)
	}
	if resumeCNPGServerName(manifest) == "" {
		return fmt.Errorf("hibernation manifest is missing CNPG source server name")
	}
	return nil
}

func artifactFromResumeManifest(source resumeSourceResolution, manifest hibernationManifest) tamossv1alpha1.HibernationArtifactStatus {
	artifact := source.Artifact
	artifact.Driver = manifest.Driver
	artifact.ManifestKey = manifest.Artifact.ManifestKey
	if artifact.ManifestKey == "" {
		artifact.ManifestKey = source.Artifact.ManifestKey
	}
	artifact.ManifestURI = manifest.Artifact.ManifestURI
	if artifact.ManifestURI == "" {
		spec := storageBackendFromDestination(source.StorageBackend)
		artifact.ManifestURI = fmt.Sprintf("s3://%s/%s", spec.BucketName, artifact.ManifestKey)
	}
	artifact.CNPGBackup = tamossv1alpha1.HibernationCNPGBackupStatus{
		Name:            manifest.CNPG.BackupName,
		Phase:           manifest.CNPG.Phase,
		DestinationPath: manifest.CNPG.DestinationPath,
		BackupID:        manifest.CNPG.BackupID,
	}
	return artifact
}

func (r *TamossResumeReconciler) acquireResumeLifecycle(ctx context.Context, tamoss *tamossv1alpha1.Tamoss, resume *tamossv1alpha1.TamossResume) (bool, error) {
	if tamoss.Status.Lifecycle.ActiveOperationRef != nil && !operationRefMatches(tamoss.Status.Lifecycle.ActiveOperationRef, resume, "TamossResume") {
		message := fmt.Sprintf("Tamoss %s already has active lifecycle operation %s/%s", tamoss.Name, tamoss.Status.Lifecycle.ActiveOperationRef.Kind, tamoss.Status.Lifecycle.ActiveOperationRef.Name)
		return false, r.updateResumeStatus(ctx, resume, tamossv1alpha1.TamossOperationPhaseFailed, operatorstatus.ReasonLifecycleOperationConflict, message, tamossv1alpha1.HibernationArtifactStatus{})
	}
	if tamoss.Status.Lifecycle.ActiveOperationRef == nil &&
		!tamoss.Spec.Paused &&
		tamoss.Status.Lifecycle.Phase != string(tamossv1alpha1.TamossLifecyclePhaseHibernated) {
		message := fmt.Sprintf("Target Tamoss %s must be paused or hibernated before Resume can replace its database", tamoss.Name)
		return false, r.updateResumeStatus(ctx, resume, tamossv1alpha1.TamossOperationPhaseFailed, operatorstatus.ReasonTargetNotQuiesced, message, tamossv1alpha1.HibernationArtifactStatus{})
	}
	if err := patchTamossLifecycleStatus(ctx, r.Client, tamoss, func(lifecycle *tamossv1alpha1.TamossLifecycleStatus) {
		setLifecycleOperationState(lifecycle,
			tamossv1alpha1.TamossLifecyclePhaseResuming,
			operatorstatus.ReasonTamossResuming,
			fmt.Sprintf("TamossResume %s is restoring the managed database", resume.Name),
			operationObjectReference(resume, "TamossResume"))
	}); err != nil {
		return false, err
	}
	return true, nil
}

func (r *TamossResumeReconciler) validateResumeCNPGTarget(ctx context.Context, tamoss *tamossv1alpha1.Tamoss, resume *tamossv1alpha1.TamossResume, artifact tamossv1alpha1.HibernationArtifactStatus) (bool, error) {
	cluster := &cnpgv1.Cluster{}
	key := types.NamespacedName{Name: tamoss.ResourceName("db"), Namespace: tamoss.Namespace}
	if err := r.Client.Get(ctx, key, cluster); err != nil {
		if apierrors.IsNotFound(err) {
			return true, nil
		}
		return false, err
	}
	if resumeOperationMatches(cluster, resume) {
		return true, nil
	}
	message := fmt.Sprintf("CNPG Cluster %s already exists and is not owned by TamossResume %s", key.Name, resume.Name)
	if err := r.setResumeLifecycleFailed(ctx, tamoss, resume, operatorstatus.ReasonLifecycleOperationConflict, message); err != nil {
		return false, err
	}
	return false, r.updateResumeStatus(ctx, resume, tamossv1alpha1.TamossOperationPhaseFailed, operatorstatus.ReasonLifecycleOperationConflict, message, artifact)
}

func (r *TamossResumeReconciler) ensureResumeCNPGCluster(ctx context.Context, tamoss *tamossv1alpha1.Tamoss, resume *tamossv1alpha1.TamossResume, storageBackend *tamossv1alpha1.StorageBackend, manifest hibernationManifest) (metav1.Condition, error) {
	target := tamoss.DeepCopy()
	if target.Spec.Backends.DB.CNPG == nil {
		target.Spec.Backends.DB.CNPG = &tamossv1alpha1.DBCNPGSpec{}
	}
	spec := storageBackendFromDestination(storageBackend)
	target.Spec.Backends.DB.CNPG.Restore = tamossv1alpha1.DBCNPGRestoreSpec{
		Enabled:  true,
		Source:   resumeCNPGServerName(manifest),
		BackupID: manifest.CNPG.BackupID,
		ObjectStore: tamossv1alpha1.DBCNPGObjectStoreSpec{
			EndpointURL:     spec.Endpoint.Default.URL,
			Bucket:          spec.BucketName,
			DestinationPath: manifest.CNPG.DestinationPath,
			ServerName:      resumeCNPGServerName(manifest),
			ExistingSecret:  spec.Credentials.ExistingSecret,
			SecretKeys:      spec.Credentials.SecretKeys,
		},
	}
	if err := cnpgbackend.Reconcile(ctx, r.Client, target, markResumeCNPGCluster(resume)); err != nil {
		return metav1.Condition{}, err
	}
	cluster := &cnpgv1.Cluster{}
	key := types.NamespacedName{Name: target.ResourceName("db"), Namespace: target.Namespace}
	if err := r.Client.Get(ctx, key, cluster); err != nil {
		if apierrors.IsNotFound(err) {
			return metav1.Condition{
				Status:  metav1.ConditionFalse,
				Reason:  operatorstatus.ReasonClusterNotReady,
				Message: fmt.Sprintf("CNPG Cluster %s has not been observed yet", key.Name),
			}, nil
		}
		return metav1.Condition{}, err
	}
	condition, _ := cnpgbackend.RollupStatus(cluster)
	return condition, nil
}

func markResumeCNPGCluster(resume *tamossv1alpha1.TamossResume) cnpgbackend.ObjectMutator {
	return func(obj client.Object) error {
		if _, ok := obj.(*cnpgv1.Cluster); !ok {
			return nil
		}
		annotations := obj.GetAnnotations()
		if annotations == nil {
			annotations = map[string]string{}
		}
		for key, value := range resumeOperationAnnotations(resume) {
			annotations[key] = value
		}
		obj.SetAnnotations(annotations)
		return nil
	}
}

func resumeOperationMatches(cluster *cnpgv1.Cluster, resume *tamossv1alpha1.TamossResume) bool {
	return cluster.Annotations[tamossResumeOperationAnnotation] == resume.Name &&
		cluster.Annotations[tamossResumeOperationUIDAnnotation] == string(resume.UID)
}

func resumeOperationAnnotations(resume *tamossv1alpha1.TamossResume) map[string]string {
	return map[string]string{
		tamossResumeOperationAnnotation:    resume.Name,
		tamossResumeOperationUIDAnnotation: string(resume.UID),
	}
}

func resumeCNPGServerName(manifest hibernationManifest) string {
	if manifest.CNPG.ServerName != "" {
		return manifest.CNPG.ServerName
	}
	return manifest.Database.Cluster
}

func (r *TamossResumeReconciler) startResumeServicesLifecycle(ctx context.Context, tamoss *tamossv1alpha1.Tamoss, resume *tamossv1alpha1.TamossResume) error {
	original := tamoss.DeepCopy()
	tamoss.Status.Lifecycle = tamossv1alpha1.TamossLifecycleStatus{
		Phase:              string(tamossv1alpha1.TamossLifecyclePhaseRunning),
		Reason:             operatorstatus.ReasonTamossResuming,
		Message:            fmt.Sprintf("TamossResume %s restored the database; services are starting", resume.Name),
		ActiveOperationRef: operationObjectReference(resume, "TamossResume"),
		LastHibernateRef:   tamoss.Status.Lifecycle.LastHibernateRef,
	}
	operatorstatus.SetConditionBool(
		&tamoss.Status.Conditions,
		tamoss.Generation,
		operatorstatus.ConditionReady,
		false,
		operatorstatus.ReasonTamossResuming,
		"Database recovery completed; TAMOSS services are starting",
	)
	if tamossStatusSemanticEqual(original.Status, tamoss.Status) {
		return nil
	}
	return r.Client.Status().Patch(ctx, tamoss, client.MergeFromWithOptions(original, client.MergeFromWithOptimisticLock{}))
}

func (r *TamossResumeReconciler) reconcileResumeServices(ctx context.Context, tamoss *tamossv1alpha1.Tamoss, resume *tamossv1alpha1.TamossResume, storageBackend *tamossv1alpha1.StorageBackend, artifact tamossv1alpha1.HibernationArtifactStatus) (ctrl.Result, error) {
	if !operationRefMatches(tamoss.Status.Lifecycle.ActiveOperationRef, resume, "TamossResume") {
		message := fmt.Sprintf("Tamoss %s no longer holds the active Resume operation", tamoss.Name)
		return ctrl.Result{}, r.updateResumeStatus(ctx, resume, tamossv1alpha1.TamossOperationPhaseFailed, operatorstatus.ReasonLifecycleOperationConflict, message, artifact)
	}

	ready := meta.FindStatusCondition(tamoss.Status.Conditions, operatorstatus.ConditionReady)
	currentAndReady := !tamoss.Spec.Paused &&
		tamoss.Status.Lifecycle.Phase == string(tamossv1alpha1.TamossLifecyclePhaseRunning) &&
		tamoss.Status.ObservedGeneration == tamoss.Generation &&
		ready != nil && ready.ObservedGeneration == tamoss.Generation && ready.Status == metav1.ConditionTrue
	if !currentAndReady {
		result := ctrl.Result{RequeueAfter: r.pollInterval()}
		message := "Waiting for the normal TAMOSS reconciliation to restore all services"
		if tamoss.Spec.Paused {
			message = "Target TAMOSS is paused; set spec.paused=false to start restored services"
		}
		return result, r.updateResumeStatus(ctx, resume, tamossv1alpha1.TamossOperationPhaseStartingServices, operatorstatus.ReasonTamossResuming, message, artifact)
	}

	if err := r.completeResumeLifecycle(ctx, tamoss, resume); err != nil {
		return ctrl.Result{}, err
	}
	completedAt := metav1.Now()
	artifact, result := r.reconcileResumeArtifactCleanup(ctx, resume, storageBackend, artifact, &completedAt, false)
	return result, r.updateResumeStatus(ctx, resume, tamossv1alpha1.TamossOperationPhaseCompleted, operatorstatus.ReasonTamossReady, "TAMOSS resume completed and all services are ready", artifact)
}

func (r *TamossResumeReconciler) completeResumeLifecycle(ctx context.Context, tamoss *tamossv1alpha1.Tamoss, resume *tamossv1alpha1.TamossResume) error {
	return patchTamossLifecycleStatus(ctx, r.Client, tamoss, func(lifecycle *tamossv1alpha1.TamossLifecycleStatus) {
		setLifecycleOperationState(lifecycle,
			tamossv1alpha1.TamossLifecyclePhaseRunning,
			operatorstatus.ReasonTamossReady,
			fmt.Sprintf("TamossResume %s completed", resume.Name),
			nil)
		lifecycle.LastResumeRef = operationObjectReference(resume, "TamossResume")
	})
}

func (r *TamossResumeReconciler) setResumeLifecycleFailed(ctx context.Context, tamoss *tamossv1alpha1.Tamoss, resume *tamossv1alpha1.TamossResume, reason, message string) error {
	return patchTamossLifecycleStatus(ctx, r.Client, tamoss, func(lifecycle *tamossv1alpha1.TamossLifecycleStatus) {
		setLifecycleOperationState(lifecycle, tamossv1alpha1.TamossLifecyclePhaseFailed, reason, message, nil)
		lifecycle.LastResumeRef = operationObjectReference(resume, "TamossResume")
	})
}

func (r *TamossResumeReconciler) manifestReader() HibernationManifestReader {
	if r.ManifestReader != nil {
		return r.ManifestReader
	}
	return S3HibernationManifestReader{Client: r.Client}
}

func (r *TamossResumeReconciler) pollInterval() time.Duration {
	if r.PollInterval > 0 {
		return r.PollInterval
	}
	return defaultProviderReadinessProbeInterval
}
