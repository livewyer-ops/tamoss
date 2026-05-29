package workload_renderer

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	tamossv1alpha1 "github.com/livewyer-ops/tamoss/operator/api/v1alpha1"
)

func renderServices(tamoss *tamossv1alpha1.Tamoss) []client.Object {
	if !tamoss.Spec.Service.Enabled {
		return nil
	}
	var objects []client.Object
	if tamoss.Spec.API.IsEnabled() {
		objects = append(objects, serviceFor(tamoss, "api", tamoss.ResourceName("api"), servicePorts(tamoss.Spec.Service.API.Ports, 8000)))
	}
	if tamoss.Spec.UI.IsEnabled() {
		objects = append(objects, serviceFor(tamoss, "ui", tamoss.ResourceName("ui"), servicePorts(tamoss.Spec.Service.UI.Ports, 3000)))
	}
	return objects
}

func serviceFor(tamoss *tamossv1alpha1.Tamoss, component, name string, ports []corev1.ServicePort) client.Object {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: tamoss.Namespace,
			Labels:    labels(tamoss, component),
		},
		Spec: corev1.ServiceSpec{
			Type:     tamoss.Spec.Service.Type,
			Ports:    ports,
			Selector: selectorLabels(tamoss, component),
		},
	}
}

func servicePorts(ports []corev1.ServicePort, defaultPort int32) []corev1.ServicePort {
	if len(ports) > 0 {
		return ports
	}
	return []corev1.ServicePort{{
		Name:       "http",
		Port:       defaultPort,
		TargetPort: intstr.FromString("http"),
		Protocol:   corev1.ProtocolTCP,
	}}
}
