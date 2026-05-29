package workload_renderer

import (
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	tamossv1alpha1 "github.com/livewyer-ops/tamoss/operator/api/v1alpha1"
)

func renderHPAs(tamoss *tamossv1alpha1.Tamoss) []client.Object {
	var objects []client.Object
	if tamoss.Spec.API.IsEnabled() && tamoss.Spec.API.Autoscaling.Enabled {
		objects = append(objects, hpaFor(tamoss, "api", tamoss.ResourceName("api"), tamoss.Spec.API.Autoscaling))
	}
	if tamoss.Spec.UI.IsEnabled() && tamoss.Spec.UI.Autoscaling.Enabled {
		objects = append(objects, hpaFor(tamoss, "ui", tamoss.ResourceName("ui"), tamoss.Spec.UI.Autoscaling))
	}
	return objects
}

func hpaFor(tamoss *tamossv1alpha1.Tamoss, component, targetName string, spec tamossv1alpha1.AutoscalingSpec) client.Object {
	metrics := []autoscalingv2.MetricSpec{}
	if spec.TargetCPUUtilizationPercentage != nil {
		metrics = append(metrics, resourceMetric(corev1.ResourceCPU, *spec.TargetCPUUtilizationPercentage))
	}
	if spec.TargetMemoryUtilizationPercentage != nil {
		metrics = append(metrics, resourceMetric(corev1.ResourceMemory, *spec.TargetMemoryUtilizationPercentage))
	}
	return &autoscalingv2.HorizontalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{
			Name:      tamoss.ResourceName(component),
			Namespace: tamoss.Namespace,
			Labels:    labels(tamoss, component),
		},
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{
				APIVersion: "apps/v1",
				Kind:       "Deployment",
				Name:       targetName,
			},
			MinReplicas: &spec.MinReplicas,
			MaxReplicas: spec.MaxReplicas,
			Metrics:     metrics,
		},
	}
}

func resourceMetric(name corev1.ResourceName, averageUtilization int32) autoscalingv2.MetricSpec {
	return autoscalingv2.MetricSpec{
		Type: autoscalingv2.ResourceMetricSourceType,
		Resource: &autoscalingv2.ResourceMetricSource{
			Name: name,
			Target: autoscalingv2.MetricTarget{
				Type:               autoscalingv2.UtilizationMetricType,
				AverageUtilization: &averageUtilization,
			},
		},
	}
}
