package controller

import (
	"context"

	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/types"

	tamossv1alpha1 "github.com/livewyer-ops/tamoss/operator/api/v1alpha1"
)

func (r *TamossReconciler) componentReplicaStatus(ctx context.Context, tamoss *tamossv1alpha1.Tamoss, component string, enabled bool, desired int32) tamossv1alpha1.ComponentReplicaStatus {
	if !enabled {
		return tamossv1alpha1.ComponentReplicaStatus{}
	}
	status := tamossv1alpha1.ComponentReplicaStatus{Desired: desired}
	deployment := &appsv1.Deployment{}
	if err := r.Client.Get(ctx, types.NamespacedName{Name: tamossResourceName(tamoss, component), Namespace: tamoss.Namespace}, deployment); err == nil {
		status.Available = deployment.Status.AvailableReplicas
	}
	return status
}

func desiredReplicaCount(spec tamossv1alpha1.WorkloadCommonSpec) int32 {
	if spec.Autoscaling.Enabled {
		return spec.Autoscaling.MinReplicas
	}
	return spec.DesiredReplicaCount()
}

func replicasReady(status tamossv1alpha1.ComponentReplicaStatus) bool {
	return status.Desired == status.Available
}
