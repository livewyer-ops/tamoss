package controller

import (
	"context"
	"testing"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestCNPGReadinessDoesNotAcceptPreviousImage(t *testing.T) {
	ctx := context.Background()
	cluster := &cnpgv1.Cluster{ObjectMeta: metav1.ObjectMeta{Name: "database", Namespace: "media", UID: "database"}, Spec: cnpgv1.ClusterSpec{Instances: 1, ImageName: "postgres:next"}}
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "database-1", Namespace: "media", Labels: map[string]string{"cnpg.io/cluster": cluster.Name}, OwnerReferences: []metav1.OwnerReference{{UID: cluster.UID, Controller: ptr.To(true)}}}, Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: postgresContainerName, Image: "postgres:previous"}}}, Status: corev1.PodStatus{ContainerStatuses: []corev1.ContainerStatus{{Name: postgresContainerName, Ready: true}}}}
	c := fake.NewClientBuilder().WithScheme(hibernateTestScheme(t)).WithObjects(pod).Build()
	r := &TamossReconciler{Client: c}
	if ready, err := r.cnpgRolloutReady(ctx, cluster); err != nil || ready {
		t.Fatalf("previous Pod was accepted: ready=%v, err=%v", ready, err)
	}
	pod.Spec.Containers[0].Image = cluster.Spec.ImageName
	if err := c.Update(ctx, pod); err != nil {
		t.Fatal(err)
	}
	if ready, err := r.cnpgRolloutReady(ctx, cluster); err != nil || !ready {
		t.Fatalf("requested Pod was rejected: ready=%v, err=%v", ready, err)
	}
}

func TestRustFSReadinessWaitsForStatefulSetRevisionAndReplicas(t *testing.T) {
	ctx := context.Background()
	tenant := &unstructured.Unstructured{Object: map[string]interface{}{"spec": map[string]interface{}{"image": "rustfs:next", "pools": []interface{}{map[string]interface{}{"name": "pool-0", "servers": int64(1)}}}}}
	tenant.SetName("store")
	tenant.SetNamespace("media")
	tenant.SetUID("store")
	set := &appsv1.StatefulSet{ObjectMeta: metav1.ObjectMeta{Name: "store-pool-0", Namespace: "media", Generation: 2, OwnerReferences: []metav1.OwnerReference{{UID: tenant.GetUID(), Controller: ptr.To(true)}}}, Spec: appsv1.StatefulSetSpec{Replicas: ptr.To(int32(1)), Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "rustfs", Image: "rustfs:next"}}}}}, Status: appsv1.StatefulSetStatus{ObservedGeneration: 2, ReadyReplicas: 1, UpdatedReplicas: 1, CurrentRevision: "previous", UpdateRevision: "next"}}
	c := fake.NewClientBuilder().WithScheme(hibernateTestScheme(t)).WithStatusSubresource(set).WithObjects(set).Build()
	r := &TamossReconciler{Client: c}
	if ready, err := r.rustfsRolloutReady(ctx, tenant); err != nil || ready {
		t.Fatalf("unfinished rollout was accepted: ready=%v, err=%v", ready, err)
	}
	set.Status.CurrentRevision = "next"
	if err := c.Status().Update(ctx, set); err != nil {
		t.Fatal(err)
	}
	if ready, err := r.rustfsRolloutReady(ctx, tenant); err != nil || !ready {
		t.Fatalf("completed rollout was rejected: ready=%v, err=%v", ready, err)
	}
	tenant.Object["spec"].(map[string]interface{})["image"] = "rustfs:later"
	if ready, err := r.rustfsRolloutReady(ctx, tenant); err != nil || ready {
		t.Fatalf("previous image was accepted: ready=%v, err=%v", ready, err)
	}
}
