package controller

import (
	"context"

	"sigs.k8s.io/controller-runtime/pkg/client"

	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/types"

	tamossv1alpha1 "github.com/livewyer-ops/tamoss/operator/api/v1alpha1"
	"github.com/livewyer-ops/tamoss/operator/internal/controller/workload_renderer"
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

func (r *TamossReconciler) workloadRolloutsReady(ctx context.Context, tamoss *tamossv1alpha1.Tamoss) bool {
	for _, object := range workload_renderer.Render(tamoss) {
		desired, ok := object.(*appsv1.Deployment)
		if !ok {
			continue
		}
		if err := applyAdvancedResourcePatches(tamoss, desired); err != nil {
			return false
		}
		deployment := &appsv1.Deployment{}
		if err := r.Client.Get(ctx, client.ObjectKeyFromObject(desired), deployment); err != nil {
			return false
		}
		for _, expected := range desired.Spec.Template.Spec.Containers {
			found := false
			for _, actual := range deployment.Spec.Template.Spec.Containers {
				if actual.Name == expected.Name && actual.Image == expected.Image {
					found = true
				}
			}
			if !found {
				return false
			}
		}
		if deployment.Status.ObservedGeneration < deployment.Generation || deployment.Spec.Replicas == nil ||
			deployment.Status.UpdatedReplicas != *deployment.Spec.Replicas || deployment.Status.Replicas != *deployment.Spec.Replicas ||
			deployment.Status.AvailableReplicas < *deployment.Spec.Replicas {
			return false
		}
	}
	return true
}
