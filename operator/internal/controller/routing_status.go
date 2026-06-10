package controller

import (
	"context"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	tamossv1alpha1 "github.com/livewyer-ops/tamoss/operator/api/v1alpha1"
	operatorstatus "github.com/livewyer-ops/tamoss/operator/internal/status"
)

var httpRouteGVK = gatewayv1.SchemeGroupVersion.WithKind("HTTPRoute")

type routingStatusResult struct {
	Ready           bool
	Reason          string
	Message         string
	HostnameStatus  metav1.ConditionStatus
	HostnameReason  string
	HostnameMessage string
}

func (r *TamossReconciler) routingStatus(ctx context.Context, tamoss *tamossv1alpha1.Tamoss) (routingStatusResult, error) {
	if tamoss.Spec.HTTPRoute.Enabled {
		return r.httpRouteRoutingStatus(ctx, tamoss)
	}
	if tamoss.Spec.Ingress.IsEnabled() {
		return routingStatusResult{
			Ready:           true,
			Reason:          operatorstatus.ReasonIngressConfigured,
			Message:         "Ingress routing is configured",
			HostnameStatus:  metav1.ConditionTrue,
			HostnameReason:  operatorstatus.ReasonIngressHostnamesConfigured,
			HostnameMessage: "Ingress hostnames are configured",
		}, nil
	}
	return routingStatusResult{
		Ready:           true,
		Reason:          operatorstatus.ReasonExternalRouting,
		Message:         "Routing is managed outside the Tamoss CR",
		HostnameStatus:  metav1.ConditionTrue,
		HostnameReason:  operatorstatus.ReasonExternalRouting,
		HostnameMessage: "Hostname management is external to the Tamoss CR",
	}, nil
}

func (r *TamossReconciler) httpRouteRoutingStatus(ctx context.Context, tamoss *tamossv1alpha1.Tamoss) (routingStatusResult, error) {
	routeNames := expectedHTTPRouteNames(tamoss)
	if len(routeNames) == 0 {
		return routingStatusResult{
			Ready:           true,
			Reason:          operatorstatus.ReasonNoRoutesRequired,
			Message:         "No HTTPRoute resources are required",
			HostnameStatus:  metav1.ConditionTrue,
			HostnameReason:  operatorstatus.ReasonNoRoutesRequired,
			HostnameMessage: "No HTTPRoute hostnames are required",
		}, nil
	}

	result := routingStatusResult{
		Ready:           true,
		Reason:          operatorstatus.ReasonRoutesProgrammed,
		Message:         "All rendered HTTPRoutes are accepted and programmed where reported",
		HostnameStatus:  metav1.ConditionTrue,
		HostnameReason:  operatorstatus.ReasonHostnamesAccepted,
		HostnameMessage: "All rendered HTTPRoutes are accepted for configured hostnames",
	}
	for _, routeName := range routeNames {
		route := &gatewayv1.HTTPRoute{}
		key := types.NamespacedName{Name: routeName, Namespace: tamoss.Namespace}
		if err := r.Client.Get(ctx, key, route); err != nil {
			switch {
			case isKubernetesNoMatchError(err):
				return gatewayAPIUnavailableResult(), nil
			case apierrors.IsNotFound(err):
				result = mergeRoutingStatus(result, routePendingResult(key))
				continue
			default:
				return routingStatusResult{}, err
			}
		}
		result = mergeRoutingStatus(result, assessHTTPRoute(route))
	}
	return result, nil
}

func expectedHTTPRouteNames(tamoss *tamossv1alpha1.Tamoss) []string {
	var names []string
	if tamoss.Spec.API.IsEnabled() {
		names = append(names, tamossResourceName(tamoss, "api"))
	}
	if tamoss.Spec.UI.IsEnabled() {
		names = append(names, tamossResourceName(tamoss, "ui"))
	}
	return names
}

func assessHTTPRoute(route *gatewayv1.HTTPRoute) routingStatusResult {
	if len(route.Status.Parents) == 0 {
		return httpRoutePending(route, "HTTPRoute has no Gateway parent status yet")
	}

	acceptedSeen := false
	for _, parent := range route.Status.Parents {
		conditions := httpRouteConditions(parent.Conditions)
		accepted := conditions[string(gatewayv1.RouteConditionAccepted)]
		if accepted == nil {
			continue
		}
		acceptedSeen = true
		switch accepted.Status {
		case metav1.ConditionTrue:
		case metav1.ConditionFalse:
			return httpRouteRejected(route, accepted, metav1.ConditionFalse)
		default:
			return httpRoutePending(route, conditionMessage(route, accepted, "HTTPRoute acceptance is pending"))
		}
		if resolved := conditions[string(gatewayv1.RouteConditionResolvedRefs)]; resolved != nil && resolved.Status != metav1.ConditionTrue {
			return httpRouteRejected(route, resolved, metav1.ConditionTrue)
		}
		if programmed := conditions["Programmed"]; programmed != nil && programmed.Status != metav1.ConditionTrue {
			return httpRouteRejected(route, programmed, metav1.ConditionTrue)
		}
	}
	if !acceptedSeen {
		return httpRoutePending(route, "HTTPRoute status does not include an Accepted condition yet")
	}
	return routingStatusResult{
		Ready:           true,
		Reason:          operatorstatus.ReasonRoutesProgrammed,
		Message:         fmt.Sprintf("HTTPRoute %s/%s is accepted and programmed where reported", route.GetNamespace(), route.GetName()),
		HostnameStatus:  metav1.ConditionTrue,
		HostnameReason:  operatorstatus.ReasonHostnamesAccepted,
		HostnameMessage: fmt.Sprintf("HTTPRoute %s/%s hostnames are accepted", route.GetNamespace(), route.GetName()),
	}
}

func httpRouteConditions(items []metav1.Condition) map[string]*metav1.Condition {
	conditions := make(map[string]*metav1.Condition, len(items))
	for i := range items {
		condition := items[i]
		if condition.Type == "" || condition.Status == "" {
			continue
		}
		conditions[condition.Type] = &condition
	}
	return conditions
}

func httpRoutePending(route *gatewayv1.HTTPRoute, message string) routingStatusResult {
	return routingStatusResult{
		Ready:           false,
		Reason:          operatorstatus.ReasonRoutePending,
		Message:         fmt.Sprintf("HTTPRoute %s/%s is pending: %s", route.GetNamespace(), route.GetName(), message),
		HostnameStatus:  metav1.ConditionUnknown,
		HostnameReason:  operatorstatus.ReasonRoutePending,
		HostnameMessage: fmt.Sprintf("HTTPRoute %s/%s hostname acceptance is pending", route.GetNamespace(), route.GetName()),
	}
}

func httpRouteRejected(route *gatewayv1.HTTPRoute, condition *metav1.Condition, hostnameStatus metav1.ConditionStatus) routingStatusResult {
	reason := operatorstatus.ConditionReason(condition, operatorstatus.ReasonRouteRejected)
	message := conditionMessage(route, condition, fmt.Sprintf("HTTPRoute condition %s is %s", condition.Type, condition.Status))
	hostnameReason := operatorstatus.ReasonHostnamesAccepted
	hostnameMessage := fmt.Sprintf("HTTPRoute %s/%s hostnames are accepted", route.GetNamespace(), route.GetName())
	if hostnameStatus != metav1.ConditionTrue {
		hostnameReason = reason
		hostnameMessage = message
	}
	return routingStatusResult{
		Ready:           false,
		Reason:          reason,
		Message:         message,
		HostnameStatus:  hostnameStatus,
		HostnameReason:  hostnameReason,
		HostnameMessage: hostnameMessage,
	}
}

func conditionMessage(route *gatewayv1.HTTPRoute, condition *metav1.Condition, fallback string) string {
	if message := operatorstatus.ConditionMessage(condition, ""); message != "" {
		return fmt.Sprintf("HTTPRoute %s/%s reports %s: %s", route.GetNamespace(), route.GetName(), condition.Type, message)
	}
	return fmt.Sprintf("HTTPRoute %s/%s reports %s", route.GetNamespace(), route.GetName(), fallback)
}

func mergeRoutingStatus(current routingStatusResult, route routingStatusResult) routingStatusResult {
	if !route.Ready && current.Ready {
		current.Ready = false
		current.Reason = route.Reason
		current.Message = route.Message
	}
	switch {
	case route.HostnameStatus == metav1.ConditionFalse && current.HostnameStatus != metav1.ConditionFalse:
		current.HostnameStatus = metav1.ConditionFalse
		current.HostnameReason = route.HostnameReason
		current.HostnameMessage = route.HostnameMessage
	case route.HostnameStatus == metav1.ConditionUnknown && current.HostnameStatus == metav1.ConditionTrue:
		current.HostnameStatus = metav1.ConditionUnknown
		current.HostnameReason = route.HostnameReason
		current.HostnameMessage = route.HostnameMessage
	}
	return current
}

func routePendingResult(key types.NamespacedName) routingStatusResult {
	message := fmt.Sprintf("HTTPRoute %s/%s has not been observed yet", key.Namespace, key.Name)
	return routingStatusResult{
		Ready:           false,
		Reason:          operatorstatus.ReasonRoutePending,
		Message:         message,
		HostnameStatus:  metav1.ConditionUnknown,
		HostnameReason:  operatorstatus.ReasonRoutePending,
		HostnameMessage: message,
	}
}

func gatewayAPIUnavailableResult() routingStatusResult {
	message := "Gateway API HTTPRoute CRD is unavailable; install Gateway API and a Gateway controller before enabling spec.httpRoute"
	return routingStatusResult{
		Ready:           false,
		Reason:          operatorstatus.ReasonGatewayAPIUnavailable,
		Message:         message,
		HostnameStatus:  metav1.ConditionFalse,
		HostnameReason:  operatorstatus.ReasonGatewayAPIUnavailable,
		HostnameMessage: message,
	}
}

func setRoutingConditions(conditions *[]metav1.Condition, generation int64, result routingStatusResult) {
	operatorstatus.SetConditionBool(conditions, generation, operatorstatus.ConditionRoutingReady, result.Ready, result.Reason, result.Message)
	operatorstatus.SetConditionStatus(conditions, generation, operatorstatus.ConditionHostnamesReady, result.HostnameStatus, result.HostnameReason, result.HostnameMessage)
}

func isKubernetesNoMatchError(err error) bool {
	if err == nil {
		return false
	}
	return meta.IsNoMatchError(err)
}
