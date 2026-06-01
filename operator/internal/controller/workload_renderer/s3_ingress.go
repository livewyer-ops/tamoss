package workload_renderer

import (
	"fmt"
	"net/url"
	"strings"

	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	tamossv1alpha1 "github.com/livewyer-ops/tamoss/operator/api/v1alpha1"
)

const (
	rustFSConsolePath        = "/rustfs/console/"
	rustFSConsoleServicePort = "http-console"
	s3ServicePort            = "s3"
)

func renderS3PublicExposure(tamoss *tamossv1alpha1.Tamoss) []client.Object {
	endpoint, serviceName, ok := managedS3PublicExposure(tamoss)
	if !ok {
		return nil
	}
	parsed, ok := parseS3PublicEndpoint(endpoint.URL)
	if !ok {
		return nil
	}

	objects := []client.Object{s3PublicIngress(tamoss, endpoint, parsed, serviceName)}
	if s3TraefikCORSRequired(tamoss) {
		objects = append(objects, s3CORSMiddleware(tamoss))
	}
	return objects
}

func managedS3PublicExposure(tamoss *tamossv1alpha1.Tamoss) (tamossv1alpha1.S3PublicEndpointSpec, string, bool) {
	if !tamoss.Spec.Ingress.IsEnabled() {
		return tamossv1alpha1.S3PublicEndpointSpec{}, "", false
	}
	switch tamoss.Spec.Backends.S3.Provider() {
	case tamossv1alpha1.S3BackendProvidedByRustFSOperator:
		if tamoss.Spec.Backends.S3.RustFSOperator == nil || strings.TrimSpace(tamoss.Spec.Backends.S3.RustFSOperator.PublicEndpoint.URL) == "" {
			return tamossv1alpha1.S3PublicEndpointSpec{}, "", false
		}
		return tamoss.Spec.Backends.S3.RustFSOperator.PublicEndpoint, tamoss.ResourceName("s3"), true
	default:
		return tamossv1alpha1.S3PublicEndpointSpec{}, "", false
	}
}

func parseS3PublicEndpoint(rawURL string) (*url.URL, bool) {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || parsed.Hostname() == "" {
		return nil, false
	}
	return parsed, true
}

func s3PublicIngress(tamoss *tamossv1alpha1.Tamoss, endpoint tamossv1alpha1.S3PublicEndpointSpec, parsed *url.URL, serviceName string) client.Object {
	paths := []networkingv1.HTTPIngressPath{
		{
			Path:     rustFSConsolePath,
			PathType: pathTypePtr(networkingv1.PathTypePrefix),
			Backend: networkingv1.IngressBackend{Service: &networkingv1.IngressServiceBackend{
				Name: serviceName + "-console",
				Port: networkingv1.ServiceBackendPort{Name: rustFSConsoleServicePort},
			}},
		},
		{
			Path:     "/",
			PathType: pathTypePtr(networkingv1.PathTypePrefix),
			Backend: networkingv1.IngressBackend{Service: &networkingv1.IngressServiceBackend{
				Name: serviceName,
				Port: networkingv1.ServiceBackendPort{Name: s3ServicePort},
			}},
		},
	}
	return &networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:        tamoss.ResourceName("s3"),
			Namespace:   tamoss.Namespace,
			Labels:      labels(tamoss, "s3"),
			Annotations: s3PublicIngressAnnotations(tamoss),
		},
		Spec: networkingv1.IngressSpec{
			IngressClassName: emptyStringNil(tamoss.Spec.Ingress.ClassName),
			TLS:              s3PublicIngressTLS(tamoss, endpoint, parsed),
			Rules: []networkingv1.IngressRule{{
				Host: parsed.Hostname(),
				IngressRuleValue: networkingv1.IngressRuleValue{
					HTTP: &networkingv1.HTTPIngressRuleValue{Paths: paths},
				},
			}},
		},
	}
}

func s3PublicIngressAnnotations(tamoss *tamossv1alpha1.Tamoss) map[string]string {
	annotations := mergeStringMaps(tamoss.Spec.Ingress.Annotations)
	if s3TraefikCORSRequired(tamoss) {
		ref := tamoss.Namespace + "-" + tamoss.ResourceName("s3-cors") + "@kubernetescrd"
		annotations[traefikMiddlewareAnnotation] = appendAnnotationValue(annotations[traefikMiddlewareAnnotation], ref)
	}
	return annotations
}

func s3PublicIngressTLS(tamoss *tamossv1alpha1.Tamoss, endpoint tamossv1alpha1.S3PublicEndpointSpec, parsed *url.URL) []networkingv1.IngressTLS {
	if !strings.EqualFold(parsed.Scheme, "https") {
		return nil
	}
	secretName := strings.TrimSpace(endpoint.TLSSecretName)
	if secretName == "" && len(tamoss.Spec.Ingress.TLS) > 0 {
		secretName = tamoss.Spec.Ingress.TLS[0].SecretName
	}
	return []networkingv1.IngressTLS{{
		SecretName: secretName,
		Hosts:      []string{parsed.Hostname()},
	}}
}

func s3TraefikCORSRequired(tamoss *tamossv1alpha1.Tamoss) bool {
	return strings.EqualFold(strings.TrimSpace(tamoss.Spec.Ingress.ClassName), "traefik") &&
		strings.TrimSpace(tamoss.Spec.Ingress.UI.Web.Host) != ""
}

func s3CORSMiddleware(tamoss *tamossv1alpha1.Tamoss) client.Object {
	return newTraefikMiddleware(tamoss.ResourceName("s3-cors"), tamoss.Namespace, labels(tamoss, "s3-cors"), traefikMiddlewareSpec{
		Headers: &traefikHeadersSpec{
			AccessControlAllowMethods:    []string{"GET", "HEAD", "PUT", "POST", "OPTIONS"},
			AccessControlAllowHeaders:    []string{"*"},
			AccessControlAllowOriginList: []string{s3CORSOrigin(tamoss)},
			AccessControlExposeHeaders:   []string{"ETag"},
			AddVaryHeader:                true,
		},
	})
}

func s3CORSOrigin(tamoss *tamossv1alpha1.Tamoss) string {
	scheme := "http"
	if len(tamoss.Spec.Ingress.TLS) > 0 {
		scheme = "https"
	}
	return fmt.Sprintf("%s://%s", scheme, strings.TrimSpace(tamoss.Spec.Ingress.UI.Web.Host))
}
