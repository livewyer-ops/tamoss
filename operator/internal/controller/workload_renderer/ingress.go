package workload_renderer

import (
	"strings"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	tamossv1alpha1 "github.com/livewyer-ops/tamoss/operator/api/v1alpha1"
	"github.com/livewyer-ops/tamoss/operator/internal/controller/auth/authentik"
)

const (
	traefikMiddlewareAnnotation = "traefik.ingress.kubernetes.io/router.middlewares"
)

func renderIngresses(tamoss *tamossv1alpha1.Tamoss) []client.Object {
	if !tamoss.Spec.Ingress.IsEnabled() {
		return nil
	}
	var objects []client.Object
	if tamoss.Spec.API.IsEnabled() {
		objects = append(objects, ingressFor(tamoss, "api", tamoss.ResourceName("api"), tamoss.Spec.Ingress.API, 8000))
	}
	if tamoss.Spec.UI.IsEnabled() {
		objects = append(objects, ingressFor(tamoss, "ui", tamoss.ResourceName("ui"), tamoss.Spec.Ingress.UI.Web, 3000))
		objects = append(objects, renderAuthentikForwardAuth(tamoss)...)
	}
	objects = append(objects, renderS3PublicExposure(tamoss)...)
	return objects
}

func ingressFor(tamoss *tamossv1alpha1.Tamoss, component, serviceName string, spec tamossv1alpha1.IngressHostSpec, defaultPort int32) client.Object {
	paths := spec.Paths
	if len(paths) == 0 {
		paths = []networkingv1.HTTPIngressPath{{
			Path:     "/",
			PathType: pathTypePtr(networkingv1.PathTypePrefix),
		}}
	}
	for i := range paths {
		if paths[i].Backend.Service == nil {
			paths[i].Backend.Service = &networkingv1.IngressServiceBackend{
				Name: serviceName,
				Port: networkingv1.ServiceBackendPort{
					Name: "http",
				},
			}
		}
		if paths[i].Backend.Service.Port.Name == "" && paths[i].Backend.Service.Port.Number == 0 {
			paths[i].Backend.Service.Port.Number = defaultPort
		}
		if paths[i].PathType == nil {
			paths[i].PathType = pathTypePtr(networkingv1.PathTypePrefix)
		}
	}
	ingress := &networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:        tamoss.ResourceName(component),
			Namespace:   tamoss.Namespace,
			Labels:      labels(tamoss, component),
			Annotations: ingressAnnotations(tamoss, component),
		},
		Spec: networkingv1.IngressSpec{
			IngressClassName: emptyStringNil(tamoss.Spec.Ingress.ClassName),
			TLS:              tamoss.Spec.Ingress.TLS,
			Rules: []networkingv1.IngressRule{{
				Host: spec.Host,
				IngressRuleValue: networkingv1.IngressRuleValue{
					HTTP: &networkingv1.HTTPIngressRuleValue{Paths: paths},
				},
			}},
		},
	}
	return ingress
}

func ingressAnnotations(tamoss *tamossv1alpha1.Tamoss, component string) map[string]string {
	annotations := mergeStringMaps(tamoss.Spec.Ingress.Annotations)
	if component != "ui" || !authentik.ForwardAuthRequired(tamoss) {
		return annotations
	}
	middleware := tamoss.ResourceName("authentik")
	ref := tamoss.Namespace + "-" + middleware + "@kubernetescrd"
	annotations[traefikMiddlewareAnnotation] = appendAnnotationValue(annotations[traefikMiddlewareAnnotation], ref)
	return annotations
}

func renderAuthentikForwardAuth(tamoss *tamossv1alpha1.Tamoss) []client.Object {
	if !authentik.ForwardAuthRequired(tamoss) {
		return nil
	}
	return []client.Object{
		authentikForwardAuthMiddleware(tamoss),
		authentikOutpostService(tamoss),
		authentikOutpostIngress(tamoss),
	}
}

func authentikForwardAuthMiddleware(tamoss *tamossv1alpha1.Tamoss) client.Object {
	name := tamoss.ResourceName("authentik")
	return newTraefikMiddleware(name, tamoss.Namespace, labels(tamoss, "authentik"), traefikMiddlewareSpec{
		ForwardAuth: &traefikForwardAuthSpec{
			Address:            authentik.OutpostForwardAuthAddress(tamoss),
			TrustForwardHeader: true,
			AuthResponseHeaders: []string{
				"X-authentik-username",
				"X-authentik-groups",
				"X-authentik-entitlements",
				"X-authentik-email",
				"X-authentik-name",
				"X-authentik-uid",
				"X-authentik-jwt",
				"X-authentik-meta-jwks",
				"X-authentik-meta-outpost",
				"X-authentik-meta-provider",
				"X-authentik-meta-app",
				"X-authentik-meta-version",
			},
		},
	})
}

func authentikOutpostService(tamoss *tamossv1alpha1.Tamoss) client.Object {
	host, port := authentik.OutpostExternalService(tamoss)
	servicePort := authentik.OutpostServicePort(port)
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      tamoss.ResourceName("authentik-outpost"),
			Namespace: tamoss.Namespace,
			Labels:    labels(tamoss, "authentik-outpost"),
		},
		Spec: corev1.ServiceSpec{
			Type:         corev1.ServiceTypeExternalName,
			ExternalName: host,
			Ports:        []corev1.ServicePort{servicePort},
		},
	}
}

func authentikOutpostIngress(tamoss *tamossv1alpha1.Tamoss) client.Object {
	_, port := authentik.OutpostExternalService(tamoss)
	servicePort := authentik.OutpostServicePort(port)
	path := networkingv1.HTTPIngressPath{
		Path:     "/outpost.goauthentik.io/",
		PathType: pathTypePtr(networkingv1.PathTypePrefix),
		Backend: networkingv1.IngressBackend{Service: &networkingv1.IngressServiceBackend{
			Name: tamoss.ResourceName("authentik-outpost"),
			Port: networkingv1.ServiceBackendPort{Name: servicePort.Name},
		}},
	}
	if servicePort.Name == "" {
		path.Backend.Service.Port = networkingv1.ServiceBackendPort{Number: servicePort.Port}
	}
	return &networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:        tamoss.ResourceName("authentik-outpost"),
			Namespace:   tamoss.Namespace,
			Labels:      labels(tamoss, "authentik-outpost"),
			Annotations: outpostIngressAnnotations(tamoss),
		},
		Spec: networkingv1.IngressSpec{
			IngressClassName: emptyStringNil(tamoss.Spec.Ingress.ClassName),
			TLS:              tamoss.Spec.Ingress.TLS,
			Rules: []networkingv1.IngressRule{{
				Host: tamoss.Spec.Ingress.UI.Web.Host,
				IngressRuleValue: networkingv1.IngressRuleValue{
					HTTP: &networkingv1.HTTPIngressRuleValue{Paths: []networkingv1.HTTPIngressPath{path}},
				},
			}},
		},
	}
}

func outpostIngressAnnotations(tamoss *tamossv1alpha1.Tamoss) map[string]string {
	annotations := mergeStringMaps(tamoss.Spec.Ingress.Annotations)
	delete(annotations, traefikMiddlewareAnnotation)
	return annotations
}

func appendAnnotationValue(existing, value string) string {
	if existing == "" {
		return value
	}
	for _, item := range strings.Split(existing, ",") {
		if strings.TrimSpace(item) == value {
			return existing
		}
	}
	return existing + "," + value
}

func pathTypePtr(pathType networkingv1.PathType) *networkingv1.PathType {
	return &pathType
}

func emptyStringNil(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}
