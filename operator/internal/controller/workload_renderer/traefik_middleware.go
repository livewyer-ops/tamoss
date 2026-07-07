package workload_renderer

import (
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Traefik does not expose a small, stable Kubernetes API module for Middleware
// resources. Keep unstructured ownership here and render from local typed
// structs so callers do not build provider-shaped maps.
var traefikMiddlewareGVK = schema.GroupVersionKind{
	Group:   "traefik.io",
	Version: "v1alpha1",
	Kind:    "Middleware",
}

type traefikMiddlewareSpec struct {
	ForwardAuth *traefikForwardAuthSpec `json:"forwardAuth,omitempty"`
	Headers     *traefikHeadersSpec     `json:"headers,omitempty"`
}

type traefikForwardAuthSpec struct {
	Address             string   `json:"address,omitempty"`
	TrustForwardHeader  bool     `json:"trustForwardHeader,omitempty"`
	AuthResponseHeaders []string `json:"authResponseHeaders,omitempty"`
}

type traefikHeadersSpec struct {
	AccessControlAllowMethods         []string `json:"accessControlAllowMethods,omitempty"`
	AccessControlAllowHeaders         []string `json:"accessControlAllowHeaders,omitempty"`
	AccessControlAllowOriginList      []string `json:"accessControlAllowOriginList,omitempty"`
	AccessControlAllowOriginListRegex []string `json:"accessControlAllowOriginListRegex,omitempty"`
	AccessControlExposeHeaders        []string `json:"accessControlExposeHeaders,omitempty"`
	AddVaryHeader                     bool     `json:"addVaryHeader,omitempty"`
}

func newTraefikMiddleware(name, namespace string, labels map[string]string, spec traefikMiddlewareSpec) client.Object {
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(traefikMiddlewareGVK)
	obj.SetName(name)
	obj.SetNamespace(namespace)
	obj.SetLabels(labels)
	obj.Object["spec"] = traefikMiddlewareSpecToUnstructured(spec)
	return obj
}

func traefikMiddlewareSpecToUnstructured(spec traefikMiddlewareSpec) map[string]interface{} {
	result, err := runtime.DefaultUnstructuredConverter.ToUnstructured(&spec)
	if err != nil {
		return map[string]interface{}{}
	}
	return result
}
