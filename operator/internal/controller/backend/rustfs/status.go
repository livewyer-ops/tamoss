package rustfs

import (
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	operatorstatus "github.com/livewyer-ops/tamoss/operator/internal/status"
)

type StatusEvent struct {
	Reason  string
	Message string
}

type tenantStatusCondition struct {
	Type    string
	Status  metav1.ConditionStatus
	Reason  string
	Message string
}

func RollupStatus(tenant *unstructured.Unstructured) (metav1.Condition, []StatusEvent) {
	condition := metav1.Condition{
		Type:    operatorstatus.ConditionBackendsReady,
		Status:  metav1.ConditionFalse,
		Reason:  operatorstatus.ReasonTenantNotReady,
		Message: fmt.Sprintf("RustFS Tenant %s is not ready", tenant.GetName()),
	}
	if ready, ok := findTenantCondition(tenant, "Ready"); ok {
		condition.Status = ready.Status
		condition.Reason = stringOrFallback(ready.Reason, operatorstatus.ReasonTenantNotReady)
		condition.Message = stringOrFallback(ready.Message, condition.Message)
	}

	events := []StatusEvent{}
	if degraded, ok := findTenantCondition(tenant, "Degraded"); ok && degraded.Status == metav1.ConditionTrue {
		events = append(events, StatusEvent{
			Reason:  operatorstatus.ReasonTenantFailed,
			Message: fmt.Sprintf("RustFS Tenant %s degraded: %s: %s", tenant.GetName(), stringOrFallback(degraded.Reason, "Degraded"), stringOrFallback(degraded.Message, "Tenant is degraded")),
		})
	}
	return condition, events
}

func findTenantCondition(tenant *unstructured.Unstructured, conditionType string) (tenantStatusCondition, bool) {
	for _, condition := range tenantConditions(tenant) {
		if condition.Type == conditionType {
			return condition, true
		}
	}
	return tenantStatusCondition{}, false
}

func tenantConditions(tenant *unstructured.Unstructured) []tenantStatusCondition {
	conditions, found, err := unstructured.NestedSlice(tenant.Object, "status", "conditions")
	if err != nil || !found {
		return nil
	}
	result := make([]tenantStatusCondition, 0, len(conditions))
	for _, item := range conditions {
		condition, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		result = append(result, tenantStatusCondition{
			Type:    conditionString(condition["type"], ""),
			Status:  conditionStatus(condition["status"]),
			Reason:  conditionString(condition["reason"], ""),
			Message: conditionString(condition["message"], ""),
		})
	}
	return result
}

func conditionStatus(value interface{}) metav1.ConditionStatus {
	if status, ok := value.(string); ok {
		switch metav1.ConditionStatus(status) {
		case metav1.ConditionTrue, metav1.ConditionFalse, metav1.ConditionUnknown:
			return metav1.ConditionStatus(status)
		}
	}
	return metav1.ConditionFalse
}

func stringOrFallback(text, fallback string) string {
	if text == "" {
		return fallback
	}
	return text
}

func conditionString(value interface{}, fallback string) string {
	text, ok := value.(string)
	if !ok || text == "" {
		return fallback
	}
	return text
}
