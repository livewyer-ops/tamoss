package controller

import (
	"context"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const postgresContainerName = "postgres"

func (r *TamossReconciler) cnpgRolloutReady(ctx context.Context, cluster *cnpgv1.Cluster) (bool, error) {
	pods := &corev1.PodList{}
	if err := r.Client.List(ctx, pods, client.InNamespace(cluster.Namespace), client.MatchingLabels{"cnpg.io/cluster": cluster.Name}); err != nil {
		return false, err
	}
	count := 0
	for _, pod := range pods.Items {
		if !metav1.IsControlledBy(&pod, cluster) {
			continue
		}
		// Bootstrap and backup Jobs have their own owners; only database Pods count.
		if !pod.DeletionTimestamp.IsZero() {
			return false, nil
		}
		ready := false
		for _, container := range pod.Spec.Containers {
			if container.Name == postgresContainerName && container.Image == cluster.Spec.ImageName {
				for _, status := range pod.Status.ContainerStatuses {
					if status.Name == postgresContainerName && status.Ready {
						ready = true
					}
				}
			}
		}
		if !ready {
			return false, nil
		}
		count++
	}
	return count == cluster.Spec.Instances, nil
}

func (r *TamossReconciler) rustfsRolloutReady(ctx context.Context, tenant *unstructured.Unstructured) (bool, error) {
	image, _, _ := unstructured.NestedString(tenant.Object, "spec", "image")
	pools, _, _ := unstructured.NestedSlice(tenant.Object, "spec", "pools")
	expected := map[string]int64{}
	for _, value := range pools {
		pool, ok := value.(map[string]interface{})
		if !ok {
			return false, nil
		}
		name, _, _ := unstructured.NestedString(pool, "name")
		servers, _, _ := unstructured.NestedInt64(pool, "servers")
		if name == "" || servers < 1 {
			return false, nil
		}
		expected[tenant.GetName()+"-"+name] = servers
	}
	sets := &appsv1.StatefulSetList{}
	if err := r.Client.List(ctx, sets, client.InNamespace(tenant.GetNamespace())); err != nil {
		return false, err
	}
	count := 0
	for _, set := range sets.Items {
		if !metav1.IsControlledBy(&set, tenant) {
			continue
		}
		if !set.DeletionTimestamp.IsZero() || set.Status.ObservedGeneration < set.Generation ||
			set.Spec.Replicas == nil || int64(*set.Spec.Replicas) != expected[set.Name] || set.Status.ReadyReplicas != *set.Spec.Replicas ||
			set.Status.UpdatedReplicas != *set.Spec.Replicas || set.Status.CurrentRevision == "" || set.Status.CurrentRevision != set.Status.UpdateRevision {
			return false, nil
		}
		imageMatches := false
		for _, container := range set.Spec.Template.Spec.Containers {
			if container.Image == image {
				imageMatches = true
			}
		}
		if !imageMatches {
			return false, nil
		}
		count++
	}
	return count > 0 && count == len(pools), nil
}
