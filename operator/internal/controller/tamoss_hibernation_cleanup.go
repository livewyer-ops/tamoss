package controller

import (
	"context"
	"fmt"
	"path"
	"strings"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
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

func (r *TamossResumeReconciler) reconcileCompletedResumeRetention(ctx context.Context, resume *tamossv1alpha1.TamossResume) (ctrl.Result, error) {
	if hibernationArtifactCleanupTerminal(resume.Status.Artifact.Cleanup.Phase) {
		return ctrl.Result{}, nil
	}
	source, ok, message, err := r.resolveResumeCleanupSource(ctx, resume)
	if err != nil {
		return ctrl.Result{}, err
	}
	artifact := source.Artifact
	if !ok {
		artifact = markHibernationArtifactCleanupBlocked(artifact, message)
		return ctrl.Result{}, r.updateResumeStatus(ctx, resume, tamossv1alpha1.TamossOperationPhaseCompleted, resumeCompletedReason(resume), resumeCompletedMessage(resume), artifact)
	}
	if source.StorageBackend == nil {
		artifact = markHibernationArtifactCleanupBlocked(artifact, "resume artifact cleanup requires a StorageBackend")
		return ctrl.Result{}, r.updateResumeStatus(ctx, resume, tamossv1alpha1.TamossOperationPhaseCompleted, resumeCompletedReason(resume), resumeCompletedMessage(resume), artifact)
	}
	artifact, result := r.reconcileResumeArtifactCleanup(ctx, resume, source.StorageBackend, artifact, resume.Status.CompletedAt, true)
	return result, r.updateResumeStatus(ctx, resume, tamossv1alpha1.TamossOperationPhaseCompleted, resumeCompletedReason(resume), resumeCompletedMessage(resume), artifact)
}

func (r *TamossResumeReconciler) resolveResumeCleanupSource(ctx context.Context, resume *tamossv1alpha1.TamossResume) (resumeSourceResolution, bool, string, error) {
	artifact := resume.Status.Artifact
	switch {
	case resume.Spec.Source.Artifact != nil:
		source := *resume.Spec.Source.Artifact
		if artifact.ManifestKey == "" {
			artifact.ManifestKey = source.ManifestKey
		}
		storageBackend, ok, message, err := r.resolveCleanupStorageBackend(ctx, resume.Namespace, source.StorageBackendRef.Name)
		if err != nil || !ok {
			return resumeSourceResolution{Artifact: artifact}, ok, message, err
		}
		return resumeSourceResolution{Artifact: artifact, StorageBackend: storageBackend}, true, "", nil
	case resume.Spec.Source.HibernationRef != nil && resume.Spec.Source.HibernationRef.Name != "":
		hibernate := &tamossv1alpha1.TamossHibernate{}
		key := types.NamespacedName{Name: resume.Spec.Source.HibernationRef.Name, Namespace: resume.Namespace}
		if err := r.Client.Get(ctx, key, hibernate); err != nil {
			if apierrors.IsNotFound(err) {
				return resumeSourceResolution{Artifact: artifact}, false, fmt.Sprintf("TamossHibernate %s was not found for artifact cleanup", key.Name), nil
			}
			return resumeSourceResolution{}, false, "", err
		}
		if artifact.ManifestKey == "" {
			artifact = hibernate.Status.Artifact
		}
		storageBackend, ok, message, err := r.resolveCleanupStorageBackend(ctx, resume.Namespace, hibernate.Spec.Destination.StorageBackendRef.Name)
		if err != nil || !ok {
			return resumeSourceResolution{Artifact: artifact}, ok, message, err
		}
		return resumeSourceResolution{Artifact: artifact, StorageBackend: storageBackend}, true, "", nil
	default:
		return resumeSourceResolution{Artifact: artifact}, false, "spec.source must set hibernationRef or artifact for artifact cleanup", nil
	}
}

func (r *TamossResumeReconciler) resolveCleanupStorageBackend(ctx context.Context, namespace, name string) (*tamossv1alpha1.StorageBackend, bool, string, error) {
	if name == "" {
		return nil, false, "resume artifact cleanup requires a hibernate StorageBackend reference", nil
	}
	storageBackend := &tamossv1alpha1.StorageBackend{}
	key := types.NamespacedName{Name: name, Namespace: namespace}
	if err := r.Client.Get(ctx, key, storageBackend); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, false, fmt.Sprintf("Artifact StorageBackend %s was not found for cleanup", key.Name), nil
		}
		return nil, false, "", err
	}
	spec := storageBackend.Spec
	spec.ApplyDefaults(storageBackend.Namespace, storageBackend.Name)
	if !spec.IsHibernateDestination() || !spec.IsExternalObjectStore() {
		return nil, false, fmt.Sprintf("Artifact StorageBackend %s must be an external-s3 hibernate destination for cleanup", storageBackend.Name), nil
	}
	return storageBackend, true, "", nil
}

func (r *TamossResumeReconciler) reconcileResumeArtifactCleanup(ctx context.Context, resume *tamossv1alpha1.TamossResume, storageBackend *tamossv1alpha1.StorageBackend, artifact tamossv1alpha1.HibernationArtifactStatus, completedAt *metav1.Time, allowDelete bool) (tamossv1alpha1.HibernationArtifactStatus, ctrl.Result) {
	spec := storageBackendFromDestination(storageBackend)
	switch spec.Hibernate.Retention.Mode {
	case "", tamossv1alpha1.HibernationRetentionModeRetain:
		artifact.Cleanup = tamossv1alpha1.HibernationArtifactCleanupStatus{
			Phase:   string(tamossv1alpha1.HibernationArtifactCleanupPhaseRetained),
			Reason:  operatorstatus.ReasonArtifactCleanupRetained,
			Message: "Hibernation artifact retained by StorageBackend policy",
		}
		return artifact, ctrl.Result{}
	case tamossv1alpha1.HibernationRetentionModeDeleteAfterResume:
		if !allowDelete {
			artifact.Cleanup = tamossv1alpha1.HibernationArtifactCleanupStatus{
				Phase:   string(tamossv1alpha1.HibernationArtifactCleanupPhasePending),
				Reason:  operatorstatus.ReasonArtifactCleanupPending,
				Message: "Hibernation artifact cleanup is scheduled after Resume completion",
			}
			return artifact, ctrl.Result{RequeueAfter: r.pollInterval()}
		}
		return r.deleteResumeArtifact(ctx, resume, storageBackend.Namespace, spec, artifact)
	case tamossv1alpha1.HibernationRetentionModeTTL:
		if spec.Hibernate.Retention.TTLSecondsAfterResume <= 0 {
			artifact = markHibernationArtifactCleanupBlocked(artifact, "hibernate.retention.ttlSecondsAfterResume must be greater than zero when retention mode is TTL")
			return artifact, ctrl.Result{}
		}
		if completedAt == nil {
			now := metav1.Now()
			completedAt = &now
		}
		dueAt := completedAt.Add(time.Duration(spec.Hibernate.Retention.TTLSecondsAfterResume) * time.Second)
		if wait := time.Until(dueAt); wait > 0 {
			artifact.Cleanup = tamossv1alpha1.HibernationArtifactCleanupStatus{
				Phase:   string(tamossv1alpha1.HibernationArtifactCleanupPhasePending),
				Reason:  operatorstatus.ReasonArtifactCleanupPending,
				Message: fmt.Sprintf("Hibernation artifact cleanup is scheduled for %s", dueAt.UTC().Format(time.RFC3339)),
			}
			return artifact, ctrl.Result{RequeueAfter: wait}
		}
		if !allowDelete {
			artifact.Cleanup = tamossv1alpha1.HibernationArtifactCleanupStatus{
				Phase:   string(tamossv1alpha1.HibernationArtifactCleanupPhasePending),
				Reason:  operatorstatus.ReasonArtifactCleanupPending,
				Message: "Hibernation artifact cleanup is scheduled after Resume completion",
			}
			return artifact, ctrl.Result{RequeueAfter: r.pollInterval()}
		}
		return r.deleteResumeArtifact(ctx, resume, storageBackend.Namespace, spec, artifact)
	default:
		artifact = markHibernationArtifactCleanupBlocked(artifact, fmt.Sprintf("unsupported hibernation retention mode %q", spec.Hibernate.Retention.Mode))
		return artifact, ctrl.Result{}
	}
}

func (r *TamossResumeReconciler) deleteResumeArtifact(ctx context.Context, resume *tamossv1alpha1.TamossResume, namespace string, spec tamossv1alpha1.StorageBackendSpec, artifact tamossv1alpha1.HibernationArtifactStatus) (tamossv1alpha1.HibernationArtifactStatus, ctrl.Result) {
	prefix, err := hibernationArtifactRootPrefix(artifact.ManifestKey)
	if err != nil {
		artifact = markHibernationArtifactCleanupBlocked(artifact, err.Error())
		r.emitArtifactCleanupBlockedEvent(resume, artifact.Cleanup.Message)
		return artifact, ctrl.Result{}
	}
	objectsDeleted, err := r.artifactCleaner().DeletePrefix(ctx, namespace, spec, prefix)
	if err != nil {
		message := fmt.Sprintf("Hibernation artifact cleanup for prefix %s failed, retrying: %v", prefix, err)
		r.emitArtifactCleanupRetryEvent(resume, message)
		artifact.Cleanup = tamossv1alpha1.HibernationArtifactCleanupStatus{
			Phase:   string(tamossv1alpha1.HibernationArtifactCleanupPhasePending),
			Reason:  operatorstatus.ReasonArtifactCleanupRetrying,
			Message: message,
		}
		return artifact, ctrl.Result{RequeueAfter: hibernationCleanupRetryInterval}
	}
	now := metav1.Now()
	artifact.Cleanup = tamossv1alpha1.HibernationArtifactCleanupStatus{
		Phase:          string(tamossv1alpha1.HibernationArtifactCleanupPhaseCompleted),
		Reason:         operatorstatus.ReasonArtifactCleanupComplete,
		Message:        fmt.Sprintf("Deleted hibernation artifact prefix %s", prefix),
		ObjectsDeleted: objectsDeleted,
		CompletedAt:    &now,
	}
	operatorstatus.EmitNormalEvent(r.Recorder, resume, operatorstatus.ReasonArtifactCleanupComplete, artifact.Cleanup.Message)
	return artifact, ctrl.Result{}
}

func (r *TamossResumeReconciler) artifactCleaner() HibernationArtifactCleaner {
	if r.ArtifactCleaner != nil {
		return r.ArtifactCleaner
	}
	return S3HibernationArtifactCleaner{Client: r.Client}
}

func (r *TamossResumeReconciler) emitArtifactCleanupBlockedEvent(resume *tamossv1alpha1.TamossResume, message string) {
	if resume.Status.Artifact.Cleanup.Phase == string(tamossv1alpha1.HibernationArtifactCleanupPhaseBlocked) {
		return
	}
	operatorstatus.EmitWarningEvent(r.Recorder, nil, resume, operatorstatus.ReasonArtifactCleanupBlocked, message)
}

func (r *TamossResumeReconciler) emitArtifactCleanupRetryEvent(resume *tamossv1alpha1.TamossResume, message string) {
	if resume.Status.Artifact.Cleanup.Reason == operatorstatus.ReasonArtifactCleanupRetrying {
		return
	}
	operatorstatus.EmitWarningEvent(r.Recorder, nil, resume, operatorstatus.ReasonArtifactCleanupRetrying, message)
}

func markHibernationArtifactCleanupBlocked(artifact tamossv1alpha1.HibernationArtifactStatus, message string) tamossv1alpha1.HibernationArtifactStatus {
	artifact.Cleanup = tamossv1alpha1.HibernationArtifactCleanupStatus{
		Phase:   string(tamossv1alpha1.HibernationArtifactCleanupPhaseBlocked),
		Reason:  operatorstatus.ReasonArtifactCleanupBlocked,
		Message: message,
	}
	return artifact
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

func resumeCompletedReason(resume *tamossv1alpha1.TamossResume) string {
	if resume.Status.Reason != "" {
		return resume.Status.Reason
	}
	return operatorstatus.ReasonTamossReady
}

func resumeCompletedMessage(resume *tamossv1alpha1.TamossResume) string {
	if resume.Status.Message != "" {
		return resume.Status.Message
	}
	return "TAMOSS resume completed; normal reconciliation can continue"
}
