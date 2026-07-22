package controller

import (
	"context"
	"errors"
	"fmt"
	"strings"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log"

	tamossv1alpha1 "github.com/livewyer-ops/tamoss/operator/api/v1alpha1"
	schemabundle "github.com/livewyer-ops/tamoss/operator/internal/schema"
	operatorstatus "github.com/livewyer-ops/tamoss/operator/internal/status"
)

var errHibernationManifestSchemaUnsupported = errors.New("hibernation manifest schema is unsupported")

// resumeBootstrapOutcome reports how far bootstrap-source resolution got.
// Exactly one of resolved, waiting, or failure is set.
type resumeBootstrapOutcome struct {
	resolved *tamossv1alpha1.TamossResolvedRestore
	waiting  string
	failure  string
	reason   string
}

func bootstrapWaiting(reason, message string) resumeBootstrapOutcome {
	return resumeBootstrapOutcome{waiting: message, reason: reason}
}

func bootstrapFailure(reason, message string) resumeBootstrapOutcome {
	return resumeBootstrapOutcome{failure: message, reason: reason}
}

// reconcileResumeBootstrap resolves spec.hibernation.resumeFrom (or, when a
// hibernated instance is woken, the artifact of its most recent hibernation)
// into a persisted database restore source. It follows CNPG bootstrap
// semantics: once the database cluster exists the source is ignored.
func (r *TamossReconciler) reconcileResumeBootstrap(ctx context.Context, tamoss *tamossv1alpha1.Tamoss) (reconcileControl, error) {
	source, waking := r.resumeBootstrapSource(tamoss)
	if source == nil || tamoss.Status.Lifecycle.ResolvedRestore != nil {
		return continueReconcile(), nil
	}
	if tamoss.Spec.Backends.DB.Provider() != tamossv1alpha1.BackendProvidedByCNPG {
		r.recordWarning(tamoss, operatorstatus.ReasonUnsupportedProvider, "spec.hibernation.resumeFrom requires a managed CNPG database backend")
		return continueReconcile(), nil
	}
	cluster := &cnpgv1.Cluster{}
	err := r.Client.Get(ctx, types.NamespacedName{Name: tamoss.ResourceName("db"), Namespace: tamoss.Namespace}, cluster)
	switch {
	case err == nil:
		// The database already exists; like CNPG bootstrap, the restore
		// source only applies at creation time.
		r.recordWarning(tamoss, operatorstatus.ReasonResumeSourceIgnored,
			fmt.Sprintf("spec.hibernation.resumeFrom is ignored because CNPG Cluster %s already exists", cluster.Name))
		return continueReconcile(), nil
	case !apierrors.IsNotFound(err):
		return stopReconcile(ctrl.Result{}), err
	}

	outcome := r.resolveResumeBootstrapSource(ctx, tamoss, source)
	switch {
	case outcome.failure != "":
		err := patchTamossLifecycleStatus(ctx, r.Client, tamoss, func(lifecycle *tamossv1alpha1.TamossLifecycleStatus) {
			setLifecycleOperationState(lifecycle, tamossv1alpha1.TamossLifecyclePhaseFailed, outcome.reason, outcome.failure, nil)
		})
		if err != nil {
			return stopReconcile(ctrl.Result{}), err
		}
		r.recordWarning(tamoss, outcome.reason, outcome.failure)
		return stopReconcile(ctrl.Result{}), nil
	case outcome.waiting != "":
		err := patchTamossLifecycleStatus(ctx, r.Client, tamoss, func(lifecycle *tamossv1alpha1.TamossLifecycleStatus) {
			lifecycle.Reason = outcome.reason
			lifecycle.Message = outcome.waiting
		})
		if err != nil {
			return stopReconcile(ctrl.Result{}), err
		}
		return stopReconcile(ctrl.Result{RequeueAfter: defaultProviderReadinessProbeInterval}), nil
	}

	log.FromContext(ctx).Info("resolved database bootstrap source from hibernation artifact",
		"tamoss", tamoss.Name, "manifestKey", outcome.resolved.ManifestKey, "waking", waking)
	operatorstatus.EmitNormalEvent(r.Recorder, tamoss, operatorstatus.ReasonTamossResuming,
		fmt.Sprintf("Bootstrapping the managed database from hibernation artifact %s", outcome.resolved.ManifestKey))
	err = patchTamossLifecycleStatus(ctx, r.Client, tamoss, func(lifecycle *tamossv1alpha1.TamossLifecycleStatus) {
		lifecycle.ResolvedRestore = outcome.resolved
		setLifecycleOperationState(lifecycle,
			tamossv1alpha1.TamossLifecyclePhaseResuming,
			operatorstatus.ReasonTamossResuming,
			fmt.Sprintf("Restoring the managed database from hibernation artifact %s", outcome.resolved.ManifestKey),
			nil)
	})
	if err != nil {
		return stopReconcile(ctrl.Result{}), err
	}
	tamoss.Status.Lifecycle.ResolvedRestore = outcome.resolved
	return continueReconcile(), nil
}

// resumeBootstrapSource returns the declared bootstrap source, or an implicit
// one derived from the last hibernation when a hibernated instance is woken.
func (r *TamossReconciler) resumeBootstrapSource(tamoss *tamossv1alpha1.Tamoss) (*tamossv1alpha1.TamossResumeSource, bool) {
	if tamoss.Spec.Hibernation.Enabled {
		return nil, false
	}
	if tamoss.Spec.Hibernation.ResumeFrom != nil {
		return tamoss.Spec.Hibernation.ResumeFrom, false
	}
	if tamossv1alpha1.TamossLifecyclePhase(tamoss.Status.Lifecycle.Phase) == tamossv1alpha1.TamossLifecyclePhaseHibernated &&
		tamoss.Status.Lifecycle.LastHibernateRef != nil {
		return &tamossv1alpha1.TamossResumeSource{
			HibernationRef: &tamossv1alpha1.LocalObjectReference{Name: tamoss.Status.Lifecycle.LastHibernateRef.Name},
		}, true
	}
	return nil, false
}

func (r *TamossReconciler) resolveResumeBootstrapSource(ctx context.Context, tamoss *tamossv1alpha1.Tamoss, source *tamossv1alpha1.TamossResumeSource) resumeBootstrapOutcome {
	var (
		storageBackendName string
		manifestKey        string
		trustedChecksum    string
	)
	switch {
	case source.HibernationRef != nil && source.HibernationRef.Name != "":
		hibernate := &tamossv1alpha1.TamossHibernate{}
		key := types.NamespacedName{Name: source.HibernationRef.Name, Namespace: tamoss.Namespace}
		if err := r.Client.Get(ctx, key, hibernate); err != nil {
			if apierrors.IsNotFound(err) {
				return bootstrapWaiting(operatorstatus.ReasonHibernateSourceInvalid, fmt.Sprintf("TamossHibernate %s was not found", key.Name))
			}
			return bootstrapWaiting(operatorstatus.ReasonHibernateSourceInvalid, fmt.Sprintf("TamossHibernate %s could not be read: %v", key.Name, err))
		}
		if hibernate.Status.Phase == string(tamossv1alpha1.TamossOperationPhaseFailed) {
			return bootstrapFailure(operatorstatus.ReasonHibernateSourceInvalid, fmt.Sprintf("TamossHibernate %s has failed and cannot be resumed; create a new hibernation", hibernate.Name))
		}
		if hibernate.Status.Phase != string(tamossv1alpha1.TamossOperationPhaseCompleted) {
			return bootstrapWaiting(operatorstatus.ReasonHibernateSourceInvalid, fmt.Sprintf("TamossHibernate %s is %q, not Completed", hibernate.Name, hibernate.Status.Phase))
		}
		manifestKey = hibernate.Status.Artifact.ManifestKey
		if manifestKey == "" {
			manifestKey = hibernateManifestKey(hibernate)
		}
		trustedChecksum = hibernate.Status.Artifact.Checksum
		if trustedChecksum == "" {
			return bootstrapFailure(operatorstatus.ReasonHibernateSourceInvalid, fmt.Sprintf("TamossHibernate %s has no trusted manifest checksum", hibernate.Name))
		}
		storageBackendName = hibernate.Spec.Destination.StorageBackendRef.Name
	case source.Artifact != nil:
		if source.Artifact.StorageBackendRef.Name == "" || source.Artifact.ManifestKey == "" || source.Artifact.Checksum == "" {
			return bootstrapFailure(operatorstatus.ReasonHibernateSourceInvalid, "resumeFrom.artifact requires storageBackendRef.name, manifestKey, and checksum")
		}
		storageBackendName = source.Artifact.StorageBackendRef.Name
		manifestKey = source.Artifact.ManifestKey
		trustedChecksum = source.Artifact.Checksum
	default:
		return bootstrapFailure(operatorstatus.ReasonHibernateSourceInvalid, "spec.hibernation.resumeFrom must set hibernationRef or artifact")
	}

	storageBackend := &tamossv1alpha1.StorageBackend{}
	if err := r.Client.Get(ctx, types.NamespacedName{Name: storageBackendName, Namespace: tamoss.Namespace}, storageBackend); err != nil {
		if apierrors.IsNotFound(err) {
			return bootstrapWaiting(operatorstatus.ReasonStorageBackendNotReady, fmt.Sprintf("Artifact StorageBackend %s was not found", storageBackendName))
		}
		return bootstrapWaiting(operatorstatus.ReasonStorageBackendNotReady, fmt.Sprintf("Artifact StorageBackend %s could not be read: %v", storageBackendName, err))
	}
	spec := storageBackend.Spec
	spec.ApplyDefaults(storageBackend.Namespace, storageBackend.Name)
	if !spec.IsHibernateDestination() || !spec.IsExternalObjectStore() {
		return bootstrapFailure(operatorstatus.ReasonHibernateSourceInvalid, fmt.Sprintf("Artifact StorageBackend %s must be an external-s3 hibernate destination", storageBackend.Name))
	}
	if storageBackend.Status.Phase != operatorstatus.PhaseReady {
		return bootstrapWaiting(operatorstatus.ReasonStorageBackendNotReady, fmt.Sprintf("Artifact StorageBackend %s is not ready", storageBackend.Name))
	}

	manifest, checksum, err := r.manifestReader().Read(ctx, storageBackend.Namespace, spec, manifestKey)
	if err != nil {
		if isPermanentHibernationManifestReadError(err) {
			return bootstrapFailure(resumeManifestReadFailureReason(err), err.Error())
		}
		return bootstrapWaiting(operatorstatus.ReasonHibernateManifestUnavailable, fmt.Sprintf("Hibernation manifest read failed, retrying: %v", err))
	}
	if checksum == "" {
		return bootstrapFailure(operatorstatus.ReasonHibernateManifestChecksumMismatch, "hibernation manifest reader did not return a SHA-256 checksum")
	}
	if trustedChecksum != checksum {
		return bootstrapFailure(operatorstatus.ReasonHibernateManifestChecksumMismatch, fmt.Sprintf("hibernation manifest checksum mismatch: source %s, computed %s", trustedChecksum, checksum))
	}
	if err := validateResumeManifest(manifest, manifestKey); err != nil {
		reason := operatorstatus.ReasonHibernateSourceInvalid
		if errors.Is(err, errHibernationManifestSchemaUnsupported) {
			reason = operatorstatus.ReasonUnsupportedSchemaVersion
		}
		return bootstrapFailure(reason, err.Error())
	}

	return resumeBootstrapOutcome{resolved: &tamossv1alpha1.TamossResolvedRestore{
		Restore: tamossv1alpha1.DBCNPGRestoreSpec{
			Enabled: true,
			Source:  resumeCNPGServerName(manifest),
			ObjectStore: tamossv1alpha1.DBCNPGObjectStoreSpec{
				EndpointURL:     spec.Endpoint.Default.URL,
				Bucket:          spec.BucketName,
				DestinationPath: manifest.CNPG.DestinationPath,
				ServerName:      resumeCNPGServerName(manifest),
				ExistingSecret:  spec.Credentials.ExistingSecret,
				SecretKeys:      spec.Credentials.SecretKeys,
			},
		},
		StorageBackendName: storageBackend.Name,
		ManifestKey:        manifestKey,
		Checksum:           checksum,
	}}
}

func (r *TamossReconciler) manifestReader() HibernationManifestReader {
	if r.ManifestReader != nil {
		return r.ManifestReader
	}
	return S3HibernationManifestReader{Client: r.Client}
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

func resumeCNPGServerName(manifest hibernationManifest) string {
	if manifest.CNPG.ServerName != "" {
		return manifest.CNPG.ServerName
	}
	return manifest.Database.Cluster
}
