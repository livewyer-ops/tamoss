package controller

import (
	"context"
	"errors"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	tamossv1alpha1 "github.com/livewyer-ops/tamoss/operator/api/v1alpha1"
	"github.com/livewyer-ops/tamoss/operator/internal/controller/defaults"
	"github.com/livewyer-ops/tamoss/operator/internal/releases"
	operatorstatus "github.com/livewyer-ops/tamoss/operator/internal/status"
)

const installationDefaultsRevisionAnnotation = "tamoss.livewyer.io/installation-defaults-revision"

func (r *TamossReconciler) stampInstallationDefaults(object client.Object) {
	stamp := func(metadata metav1.Object) {
		annotations := metadata.GetAnnotations()
		if revision := r.InstanceDefaults.Revision(); revision != "" {
			if annotations == nil {
				annotations = map[string]string{}
			}
			annotations[installationDefaultsRevisionAnnotation] = revision
		} else {
			delete(annotations, installationDefaultsRevisionAnnotation)
		}
		metadata.SetAnnotations(annotations)
	}
	stamp(object)
	if deployment, ok := object.(*appsv1.Deployment); ok {
		stamp(&deployment.Spec.Template.ObjectMeta)
	}
}

func resolveTamoss(tamoss *tamossv1alpha1.Tamoss, catalogue releases.Catalogue, installation *defaults.Installation) (*tamossv1alpha1.Tamoss, error) {
	release, err := catalogue.Select(tamoss.Spec.Version, tamoss.Status.CurrentVersion, tamoss.Status.Upgrade.TargetVersion)
	if err != nil {
		return nil, err
	}
	resolved := tamoss.DeepCopy()
	if err := installation.Apply(resolved); err != nil {
		return nil, &releases.SelectionError{Reason: "InvalidInstallationDefaults", Message: err.Error()}
	}
	if resolved.Spec.Profile == "" && resolved.Spec.Backends.DB == (tamossv1alpha1.DBBackendSpec{}) && resolved.Spec.Backends.S3.ProvidedBy == "" && resolved.Spec.Backends.S3.External == nil && resolved.Spec.Backends.S3.RustFSOperator == nil {
		return nil, &releases.SelectionError{Reason: "InstallationDefaultsRequired", Message: "Configure installation defaults or set spec.profile and the instance settings explicitly"}
	}
	defaults.Apply(resolved, release.Images)
	return resolved, nil
}

func releaseErrorReason(err error) string {
	var selection *releases.SelectionError
	if errors.As(err, &selection) {
		return selection.Reason
	}
	return "VersionUnavailable"
}

// Check durable schema state before changing backends or starting lifecycle work.
func (r *TamossReconciler) validateReleaseState(ctx context.Context, tamoss *tamossv1alpha1.Tamoss) error {
	release, err := r.Releases.Select(tamoss.Spec.Version, tamoss.Status.CurrentVersion, tamoss.Status.Upgrade.TargetVersion)
	if err != nil {
		return err
	}
	if _, err := resolveTamoss(tamoss, r.Releases, r.InstanceDefaults); err != nil {
		return err
	}
	state := &corev1.ConfigMap{}
	err = r.Client.Get(ctx, client.ObjectKey{Namespace: tamoss.Namespace, Name: tamossResourceName(tamoss, "schema-state")}, state)
	if apierrors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}
	observed := state.Data[schemaStateAppliedVersionKey]
	if !release.Schema.Supports(observed) {
		return &releases.SelectionError{Reason: operatorstatus.ReasonUnsupportedSchemaVersion, Message: fmt.Sprintf("Release %q does not support installed schema %q", release.Version, observed)}
	}
	if tamoss.Status.CurrentVersion == "" && observed != "" && observed != release.Schema.Version {
		return &releases.SelectionError{Reason: "VersionAdoptionRequired", Message: fmt.Sprintf("Pin the release matching installed schema %q before selecting an upgrade", observed)}
	}
	return nil
}

func (r *TamossReconciler) blockRelease(ctx context.Context, tamoss *tamossv1alpha1.Tamoss, err error) error {
	original := tamoss.DeepCopy()
	reason := releaseErrorReason(err)
	tamoss.Status.ObservedGeneration = tamoss.Generation
	tamoss.Status.Phase = operatorstatus.PhaseDegraded
	for _, conditionType := range []string{operatorstatus.ConditionReady, operatorstatus.ConditionUpgradeable} {
		meta.SetStatusCondition(&tamoss.Status.Conditions, metav1.Condition{Type: conditionType, Status: metav1.ConditionFalse, Reason: reason, Message: err.Error(), ObservedGeneration: tamoss.Generation})
	}
	tamoss.Status.Upgrade = tamossv1alpha1.UpgradeStatus{TargetVersion: tamoss.Status.Upgrade.TargetVersion, Phase: operatorstatus.PhaseBlocked, Reason: reason, Message: err.Error()}
	return r.patchTamossStatus(ctx, tamoss, original)
}

func (r *TamossReconciler) beginRelease(ctx context.Context, tamoss *tamossv1alpha1.Tamoss) error {
	if tamoss.Status.CurrentVersion == tamoss.Spec.Version || tamoss.Status.Upgrade.TargetVersion != "" {
		return nil
	}
	original := tamoss.DeepCopy()
	statusCopy := tamoss.DeepCopy()
	statusCopy.Status.Upgrade.TargetVersion = tamoss.Spec.Version
	meta.SetStatusCondition(&statusCopy.Status.Conditions, metav1.Condition{Type: operatorstatus.ConditionReady, Status: metav1.ConditionFalse, Reason: "ReleaseProgressing", Message: "Reconciling the selected release", ObservedGeneration: tamoss.Generation})
	if err := r.patchTamossStatus(ctx, statusCopy, original); err != nil {
		return err
	}
	// A status response contains the persisted spec, not the calculated defaults.
	tamoss.Status = statusCopy.Status
	tamoss.ResourceVersion = statusCopy.ResourceVersion
	return nil
}
