package workload_renderer

import (
	"strconv"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	tamossv1alpha1 "github.com/livewyer-ops/tamoss/operator/api/v1alpha1"
)

const (
	metricsPathAnnotation            = "tamoss.livewyer.io/metrics-path"
	metricsPortAnnotation            = "tamoss.livewyer.io/metrics-port"
	prometheusScrapeAnnotation       = "prometheus.io/scrape"
	prometheusPathAnnotation         = "prometheus.io/path"
	prometheusPortAnnotation         = "prometheus.io/port"
	metricsPortName                  = "metrics"
	apiMetricsPath                   = "/metrics"
	defaultAPIServicePort      int32 = 8000
	defaultUIServicePort       int32 = 3000
	apiMetricsPort             int32 = 9090
)

func renderServices(tamoss *tamossv1alpha1.Tamoss) []client.Object {
	if !tamoss.Spec.Service.Enabled {
		return nil
	}
	var objects []client.Object
	if tamoss.Spec.API.IsEnabled() {
		objects = append(objects, serviceFor(
			tamoss,
			"api",
			tamoss.ResourceName("api"),
			servicePorts(tamoss.Spec.Service.API.Ports, defaultAPIServicePort),
			nil,
		))
		objects = append(objects, serviceForSelector(
			tamoss,
			"api-metrics",
			"api",
			tamoss.ResourceName("api-metrics"),
			[]corev1.ServicePort{{
				Name:       metricsPortName,
				Port:       apiMetricsPort,
				TargetPort: intstr.FromString(metricsPortName),
				Protocol:   corev1.ProtocolTCP,
			}},
			corev1.ServiceTypeClusterIP,
			apiMetricsAnnotations(apiMetricsPort),
		))
	}
	if tamoss.Spec.UI.IsEnabled() {
		objects = append(objects, serviceFor(
			tamoss,
			"ui",
			tamoss.ResourceName("ui"),
			servicePorts(tamoss.Spec.Service.UI.Ports, defaultUIServicePort),
			nil,
		))
	}
	return objects
}

func serviceFor(
	tamoss *tamossv1alpha1.Tamoss,
	component string,
	name string,
	ports []corev1.ServicePort,
	annotations map[string]string,
) client.Object {
	return serviceForSelector(tamoss, component, component, name, ports, tamoss.Spec.Service.Type, annotations)
}

func serviceForSelector(
	tamoss *tamossv1alpha1.Tamoss,
	component string,
	selectorComponent string,
	name string,
	ports []corev1.ServicePort,
	serviceType corev1.ServiceType,
	annotations map[string]string,
) client.Object {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Namespace:   tamoss.Namespace,
			Labels:      labels(tamoss, component),
			Annotations: annotations,
		},
		Spec: corev1.ServiceSpec{
			Type:     serviceType,
			Ports:    ports,
			Selector: selectorLabels(tamoss, selectorComponent),
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

func apiMetricsAnnotations(port int32) map[string]string {
	portValue := strconv.Itoa(int(port))
	return map[string]string{
		metricsPathAnnotation:      apiMetricsPath,
		metricsPortAnnotation:      portValue,
		prometheusScrapeAnnotation: "true",
		prometheusPathAnnotation:   apiMetricsPath,
		prometheusPortAnnotation:   portValue,
	}
}
