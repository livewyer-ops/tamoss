package controller

import (
	"testing"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	operatorstatus "github.com/livewyer-ops/tamoss/operator/internal/status"
)

func TestAssessHTTPRouteAcceptedAndProgrammed(t *testing.T) {
	route := httpRouteWithConditions(
		metav1.Condition{Type: "Accepted", Status: metav1.ConditionTrue, Reason: "Accepted"},
		metav1.Condition{Type: "ResolvedRefs", Status: metav1.ConditionTrue, Reason: "ResolvedRefs"},
		metav1.Condition{Type: "Programmed", Status: metav1.ConditionTrue, Reason: "Programmed"},
	)

	result := assessHTTPRoute(route)
	if !result.Ready {
		t.Fatalf("expected route ready, got %#v", result)
	}
	if result.HostnameStatus != metav1.ConditionTrue {
		t.Fatalf("expected hostnames ready, got %#v", result)
	}
}

func TestAssessHTTPRouteRejectedHostname(t *testing.T) {
	route := httpRouteWithConditions(
		metav1.Condition{Type: "Accepted", Status: metav1.ConditionFalse, Reason: "HostnameConflict", Message: "hostname is already attached"},
	)

	result := assessHTTPRoute(route)
	if result.Ready {
		t.Fatalf("expected route not ready")
	}
	if result.Reason != "HostnameConflict" {
		t.Fatalf("expected route reason from Gateway API, got %q", result.Reason)
	}
	if result.HostnameStatus != metav1.ConditionFalse || result.HostnameReason != "HostnameConflict" {
		t.Fatalf("expected hostname rejection surfaced, got %#v", result)
	}
}

func TestAssessHTTPRouteResolvedRefsFailurePreservesHostnameReady(t *testing.T) {
	route := httpRouteWithConditions(
		metav1.Condition{Type: "Accepted", Status: metav1.ConditionTrue, Reason: "Accepted"},
		metav1.Condition{Type: "ResolvedRefs", Status: metav1.ConditionFalse, Reason: "BackendNotFound", Message: "service does not exist"},
	)

	result := assessHTTPRoute(route)
	if result.Ready {
		t.Fatalf("expected route not ready")
	}
	if result.Reason != "BackendNotFound" {
		t.Fatalf("expected unresolved reference reason, got %q", result.Reason)
	}
	if result.HostnameStatus != metav1.ConditionTrue {
		t.Fatalf("expected hostnames to remain accepted, got %#v", result)
	}
}

func TestAssessHTTPRoutePendingWithoutParentStatus(t *testing.T) {
	route := &gatewayv1.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "example-api",
			Namespace: "default",
		},
	}

	result := assessHTTPRoute(route)
	if result.Ready {
		t.Fatalf("expected route not ready")
	}
	if result.Reason != operatorstatus.ReasonRoutePending {
		t.Fatalf("expected pending route reason, got %q", result.Reason)
	}
	if result.HostnameStatus != metav1.ConditionUnknown {
		t.Fatalf("expected hostname status unknown, got %#v", result)
	}
}

func TestIsKubernetesNoMatchErrorRecognizesMissingOptionalResource(t *testing.T) {
	err := &meta.NoKindMatchError{GroupKind: httpRouteGVK.GroupKind()}
	if !isKubernetesNoMatchError(err) {
		t.Fatalf("expected missing HTTPRoute resource error to be treated as a no-match error")
	}
}

func httpRouteWithConditions(conditions ...metav1.Condition) *gatewayv1.HTTPRoute {
	return &gatewayv1.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "example-api",
			Namespace: "default",
		},
		Status: gatewayv1.HTTPRouteStatus{
			RouteStatus: gatewayv1.RouteStatus{
				Parents: []gatewayv1.RouteParentStatus{{
					ParentRef:      gatewayv1.ParentReference{Name: gatewayv1.ObjectName("public-gateway")},
					ControllerName: gatewayv1.GatewayController("example.net/gateway-controller"),
					Conditions:     conditions,
				}},
			},
		},
	}
}
