package controller

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	tamossv1alpha1 "github.com/livewyer-ops/tamoss/operator/api/v1alpha1"
	operatorstatus "github.com/livewyer-ops/tamoss/operator/internal/status"
)

func storageBackendWarningReason(reason string) bool {
	switch reason {
	case operatorstatus.ReasonMissingSecret,
		operatorstatus.ReasonUnsupportedProvider,
		operatorstatus.ReasonMissingProviderConfiguration,
		operatorstatus.ReasonInvalidStorageBackendTags,
		operatorstatus.ReasonBucketCreationFailed,
		operatorstatus.ReasonDatabaseRegistrationRetrying,
		operatorstatus.ReasonCORSMisconfigured,
		operatorstatus.ReasonEndpointUnreachable,
		operatorstatus.ReasonTLSValidationFailed:
		return true
	default:
		return false
	}
}

func tamossWarningReason(reason string) bool {
	switch reason {
	case operatorstatus.ReasonAPITokenRotationRejected,
		operatorstatus.ReasonArtifactCleanupBlocked,
		operatorstatus.ReasonArtifactCleanupRetrying,
		operatorstatus.ReasonAuthentikAPITokenMissing,
		operatorstatus.ReasonAuthentikManagedBlueprintApplyFailed,
		operatorstatus.ReasonAuthentikManagedBlueprintDeleteFailed,
		operatorstatus.ReasonBackupArchivingFailed,
		operatorstatus.ReasonBackupArchivingUnknown,
		operatorstatus.ReasonBackupPolicyFailed,
		operatorstatus.ReasonBackupPolicyIncomplete,
		operatorstatus.ReasonBackupPolicyMissing,
		operatorstatus.ReasonBucketCreationFailed,
		operatorstatus.ReasonCNPGSecretKeyMissing,
		operatorstatus.ReasonClusterNotReady,
		operatorstatus.ReasonCORSMisconfigured,
		operatorstatus.ReasonEndpointUnreachable,
		operatorstatus.ReasonGatewayAPIUnavailable,
		operatorstatus.ReasonHibernateDestinationInvalid,
		operatorstatus.ReasonHibernateManifestChecksumMismatch,
		operatorstatus.ReasonHibernateManifestUnavailable,
		operatorstatus.ReasonHibernateManifestUploadFailed,
		operatorstatus.ReasonHibernateSourceInvalid,
		operatorstatus.ReasonImagePullFailed,
		operatorstatus.ReasonInvalidStorageBackendTags,
		operatorstatus.ReasonIssuerUnreachable,
		operatorstatus.ReasonLifecycleBlocked,
		operatorstatus.ReasonLifecycleOperationDeleted,
		operatorstatus.ReasonLifecycleOperationConflict,
		operatorstatus.ReasonMissingDependencyOperator,
		operatorstatus.ReasonMissingProviderConfiguration,
		operatorstatus.ReasonMissingSecret,
		operatorstatus.ReasonNoRedirectURIDerivable,
		operatorstatus.ReasonPlatformNamespaceNotAllowed,
		operatorstatus.ReasonRouteRejected,
		operatorstatus.ReasonSchemaMigrationFailed,
		operatorstatus.ReasonTLSValidationFailed,
		operatorstatus.ReasonTenantFailed,
		operatorstatus.ReasonTenantNotReady,
		operatorstatus.ReasonUnsupportedHTTPRouteFilter,
		operatorstatus.ReasonUnsupportedProvider,
		operatorstatus.ReasonUnsupportedSchemaVersion,
		operatorstatus.ReasonWaitingForCNPGSecret:
		return true
	default:
		return false
	}
}

func tamossWarningConditionTypes() []string {
	return []string{
		operatorstatus.ConditionReady,
		operatorstatus.ConditionBackendsReady,
		operatorstatus.ConditionSchemaMigrated,
		operatorstatus.ConditionIdentityReady,
		operatorstatus.ConditionRoutingReady,
		operatorstatus.ConditionHostnamesReady,
		operatorstatus.ConditionBackupPolicyReady,
		operatorstatus.ConditionLifecycleReady,
	}
}

func tamossObservedReady(tamoss *tamossv1alpha1.Tamoss) bool {
	condition := meta.FindStatusCondition(tamoss.Status.Conditions, operatorstatus.ConditionReady)
	return condition != nil && condition.Status == metav1.ConditionTrue
}

func (r *TamossReconciler) emitImagePullEvents(ctx context.Context, tamoss *tamossv1alpha1.Tamoss) error {
	pods := &corev1.PodList{}
	if err := r.Client.List(ctx, pods, client.InNamespace(tamoss.Namespace), client.MatchingLabels{
		appInstanceLabel:               tamoss.Name,
		"app.kubernetes.io/managed-by": "tamoss-operator",
	}); err != nil {
		return err
	}
	for _, pod := range pods.Items {
		for _, status := range append(pod.Status.InitContainerStatuses, pod.Status.ContainerStatuses...) {
			if status.State.Waiting == nil {
				continue
			}
			reason := status.State.Waiting.Reason
			if reason != "ImagePullBackOff" && reason != "ErrImagePull" {
				continue
			}
			r.recordWarning(
				tamoss,
				operatorstatus.ReasonImagePullFailed,
				fmt.Sprintf(
					"Pod %s container %s is waiting with %s: %s",
					pod.Name,
					status.Name,
					reason,
					status.State.Waiting.Message,
				),
			)
		}
	}
	return nil
}
