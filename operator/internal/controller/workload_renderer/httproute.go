package workload_renderer

import (
	"encoding/json"
	"fmt"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	tamossv1alpha1 "github.com/livewyer-ops/tamoss/operator/api/v1alpha1"
)

func renderHTTPRoutes(tamoss *tamossv1alpha1.Tamoss) []client.Object {
	if !tamoss.Spec.HTTPRoute.Enabled {
		return nil
	}
	var objects []client.Object
	if tamoss.Spec.API.IsEnabled() {
		objects = append(objects, httpRouteFor(tamoss, "api", tamoss.Spec.HTTPRoute.API))
	}
	if tamoss.Spec.UI.IsEnabled() {
		objects = append(objects, httpRouteFor(tamoss, "ui", tamoss.Spec.HTTPRoute.UI))
	}
	return objects
}

func ValidateHTTPRouteFilters(tamoss *tamossv1alpha1.Tamoss) []string {
	if !tamoss.Spec.HTTPRoute.Enabled {
		return nil
	}
	var invalid []string
	invalid = append(invalid, invalidGatewayFilterPaths(".spec.httpRoute.api.defaultFilters", tamoss.Spec.HTTPRoute.API.DefaultFilters)...)
	invalid = append(invalid, invalidGatewayFilterPaths(".spec.httpRoute.api.filters", tamoss.Spec.HTTPRoute.API.Filters)...)
	for index, rule := range tamoss.Spec.HTTPRoute.API.Rules {
		invalid = append(invalid, invalidGatewayFilterPaths(fmt.Sprintf(".spec.httpRoute.api.rules[%d].filters", index), rule.Filters)...)
	}
	invalid = append(invalid, invalidGatewayFilterPaths(".spec.httpRoute.ui.defaultFilters", tamoss.Spec.HTTPRoute.UI.DefaultFilters)...)
	invalid = append(invalid, invalidGatewayFilterPaths(".spec.httpRoute.ui.filters", tamoss.Spec.HTTPRoute.UI.Filters)...)
	for index, rule := range tamoss.Spec.HTTPRoute.UI.Rules {
		invalid = append(invalid, invalidGatewayFilterPaths(fmt.Sprintf(".spec.httpRoute.ui.rules[%d].filters", index), rule.Filters)...)
	}
	return invalid
}

func httpRouteFor(tamoss *tamossv1alpha1.Tamoss, component string, spec tamossv1alpha1.HTTPRouteHostSpec) client.Object {
	rules := httpRouteRules(spec)
	if len(rules) == 0 {
		rules = []gatewayv1.HTTPRouteRule{defaultHTTPRouteRule(tamoss, component, spec)}
	}
	return &gatewayv1.HTTPRoute{
		TypeMeta: metav1.TypeMeta{
			APIVersion: gatewayv1.GroupVersion.String(),
			Kind:       "HTTPRoute",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        tamoss.ResourceName(component),
			Namespace:   tamoss.Namespace,
			Labels:      labels(tamoss, component),
			Annotations: tamoss.Spec.HTTPRoute.Annotations,
		},
		Spec: gatewayv1.HTTPRouteSpec{
			CommonRouteSpec: gatewayv1.CommonRouteSpec{
				ParentRefs: gatewayParentRefs(tamoss.Spec.HTTPRoute.ParentRefs),
			},
			Hostnames: gatewayHostnames(spec.Hostnames),
			Rules:     rules,
		},
	}
}

func defaultHTTPRouteRule(tamoss *tamossv1alpha1.Tamoss, component string, spec tamossv1alpha1.HTTPRouteHostSpec) gatewayv1.HTTPRouteRule {
	servicePort := firstServicePort(tamoss.Spec.Service.UI.Ports, 3000)
	if component == "api" {
		servicePort = firstServicePort(tamoss.Spec.Service.API.Ports, 8000)
	}
	pathType := gatewayv1.PathMatchPathPrefix
	pathValue := "/"
	port := gatewayv1.PortNumber(servicePort)
	weight := int32(1)
	rule := gatewayv1.HTTPRouteRule{
		Matches: []gatewayv1.HTTPRouteMatch{{
			Path: &gatewayv1.HTTPPathMatch{
				Type:  &pathType,
				Value: &pathValue,
			},
		}},
		BackendRefs: []gatewayv1.HTTPBackendRef{{
			BackendRef: gatewayv1.BackendRef{
				BackendObjectReference: gatewayv1.BackendObjectReference{
					Name: gatewayv1.ObjectName(tamoss.ResourceName(component)),
					Port: &port,
				},
				Weight: &weight,
			},
		}},
	}
	filters := append(gatewayFilters(spec.DefaultFilters), gatewayFilters(spec.Filters)...)
	if len(filters) > 0 {
		rule.Filters = filters
	}
	return rule
}

func httpRouteRules(spec tamossv1alpha1.HTTPRouteHostSpec) []gatewayv1.HTTPRouteRule {
	rules := make([]gatewayv1.HTTPRouteRule, 0, len(spec.Rules))
	baseFilters := append(gatewayFilters(spec.DefaultFilters), gatewayFilters(spec.Filters)...)
	for _, rule := range spec.Rules {
		item := gatewayv1.HTTPRouteRule{
			Matches:     gatewayMatches(rule.Matches),
			BackendRefs: gatewayBackendRefs(rule.BackendRefs),
		}
		filters := append([]gatewayv1.HTTPRouteFilter{}, baseFilters...)
		filters = append(filters, gatewayFilters(rule.Filters)...)
		if len(filters) > 0 {
			item.Filters = filters
		}
		rules = append(rules, item)
	}
	return rules
}

func gatewayParentRefs(values []tamossv1alpha1.HTTPRouteParentRef) []gatewayv1.ParentReference {
	result := make([]gatewayv1.ParentReference, 0, len(values))
	for _, value := range values {
		ref := gatewayv1.ParentReference{Name: gatewayv1.ObjectName(value.Name)}
		if value.Namespace != "" {
			namespace := gatewayv1.Namespace(value.Namespace)
			ref.Namespace = &namespace
		}
		if value.Kind != "" {
			kind := gatewayv1.Kind(value.Kind)
			ref.Kind = &kind
		}
		if value.SectionName != "" {
			sectionName := gatewayv1.SectionName(value.SectionName)
			ref.SectionName = &sectionName
		}
		result = append(result, ref)
	}
	return result
}

func gatewayHostnames(values []string) []gatewayv1.Hostname {
	result := make([]gatewayv1.Hostname, 0, len(values))
	for _, value := range values {
		result = append(result, gatewayv1.Hostname(value))
	}
	return result
}

func gatewayMatches(values []tamossv1alpha1.HTTPRouteMatch) []gatewayv1.HTTPRouteMatch {
	result := make([]gatewayv1.HTTPRouteMatch, 0, len(values))
	for _, value := range values {
		match := gatewayv1.HTTPRouteMatch{}
		if value.Path != nil {
			path := &gatewayv1.HTTPPathMatch{}
			if value.Path.Type != "" {
				pathType := gatewayv1.PathMatchType(value.Path.Type)
				path.Type = &pathType
			}
			if value.Path.Value != "" {
				pathValue := value.Path.Value
				path.Value = &pathValue
			}
			match.Path = path
		}
		result = append(result, match)
	}
	return result
}

func gatewayBackendRefs(values []tamossv1alpha1.HTTPRouteBackendRef) []gatewayv1.HTTPBackendRef {
	result := make([]gatewayv1.HTTPBackendRef, 0, len(values))
	for _, value := range values {
		ref := gatewayv1.HTTPBackendRef{
			BackendRef: gatewayv1.BackendRef{
				BackendObjectReference: gatewayv1.BackendObjectReference{
					Name: gatewayv1.ObjectName(value.Name),
				},
			},
		}
		if value.Namespace != "" {
			namespace := gatewayv1.Namespace(value.Namespace)
			ref.Namespace = &namespace
		}
		if value.Port != 0 {
			port := gatewayv1.PortNumber(value.Port)
			ref.Port = &port
		}
		if value.Weight != nil {
			weight := *value.Weight
			ref.Weight = &weight
		}
		result = append(result, ref)
	}
	return result
}

func gatewayFilters(values []apiextensionsv1.JSON) []gatewayv1.HTTPRouteFilter {
	result := make([]gatewayv1.HTTPRouteFilter, 0, len(values))
	for _, value := range values {
		var filter gatewayv1.HTTPRouteFilter
		if len(value.Raw) == 0 || json.Unmarshal(value.Raw, &filter) != nil {
			continue
		}
		result = append(result, filter)
	}
	return result
}

func invalidGatewayFilterPaths(path string, values []apiextensionsv1.JSON) []string {
	var invalid []string
	redirects := 0
	rewrites := 0
	for index, value := range values {
		filterPath := fmt.Sprintf("%s[%d]", path, index)
		var filter gatewayv1.HTTPRouteFilter
		if len(value.Raw) == 0 || json.Unmarshal(value.Raw, &filter) != nil || filter.Type == "" {
			invalid = append(invalid, filterPath)
			continue
		}
		switch filter.Type {
		case gatewayv1.HTTPRouteFilterRequestHeaderModifier, gatewayv1.HTTPRouteFilterResponseHeaderModifier, gatewayv1.HTTPRouteFilterRequestMirror:
		case gatewayv1.HTTPRouteFilterRequestRedirect:
			redirects++
			if filter.RequestRedirect == nil {
				invalid = append(invalid, filterPath+".requestRedirect")
			}
		case gatewayv1.HTTPRouteFilterURLRewrite:
			rewrites++
			if filter.URLRewrite == nil {
				invalid = append(invalid, filterPath+".urlRewrite")
			}
		case gatewayv1.HTTPRouteFilterExtensionRef:
			if filter.ExtensionRef == nil {
				invalid = append(invalid, filterPath+".extensionRef")
			}
		default:
			invalid = append(invalid, filterPath+".type")
		}
	}
	if redirects > 1 {
		invalid = append(invalid, path+".RequestRedirect")
	}
	if rewrites > 1 {
		invalid = append(invalid, path+".URLRewrite")
	}
	if redirects > 0 && rewrites > 0 {
		invalid = append(invalid, path+".RequestRedirect+URLRewrite")
	}
	return invalid
}
