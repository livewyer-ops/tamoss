package rustfs

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	operatorstatus "github.com/livewyer-ops/tamoss/operator/internal/status"
)

func TestRollupStatusReady(t *testing.T) {
	tenant := tenantWithConditions(map[string]interface{}{
		"type":    "Ready",
		"status":  "True",
		"reason":  "ReconcileSucceeded",
		"message": "4/4 pods ready",
	})

	condition, events := RollupStatus(tenant)
	if condition.Status != metav1.ConditionTrue {
		t.Fatalf("expected BackendsReady True, got %#v", condition)
	}
	if condition.Reason != "ReconcileSucceeded" || condition.Message != "4/4 pods ready" {
		t.Fatalf("expected upstream reason/message to pass through, got %#v", condition)
	}
	if len(events) != 0 {
		t.Fatalf("did not expect warning events, got %#v", events)
	}
}

func TestRollupStatusNotReady(t *testing.T) {
	tenant := tenantWithConditions(map[string]interface{}{
		"type":    "Ready",
		"status":  "False",
		"reason":  "PodsNotReady",
		"message": "2/4 pods ready",
	})

	condition, _ := RollupStatus(tenant)
	if condition.Status != metav1.ConditionFalse || condition.Reason != "PodsNotReady" {
		t.Fatalf("expected not-ready condition, got %#v", condition)
	}
}

func TestRollupStatusDegradedEvent(t *testing.T) {
	tenant := tenantWithConditions(
		map[string]interface{}{
			"type":    "Ready",
			"status":  "False",
			"reason":  "PoolDegraded",
			"message": "One or more pools are degraded",
		},
		map[string]interface{}{
			"type":    "Degraded",
			"status":  "True",
			"reason":  "PoolDegraded",
			"message": "One or more pools are degraded",
		},
	)

	_, events := RollupStatus(tenant)
	if len(events) != 1 {
		t.Fatalf("expected one warning event, got %#v", events)
	}
	if events[0].Reason != operatorstatus.ReasonTenantFailed {
		t.Fatalf("expected TenantFailed event, got %#v", events[0])
	}
}

func tenantWithConditions(conditions ...map[string]interface{}) *unstructured.Unstructured {
	tenant := NewTenant()
	tenant.SetName("example-s3")
	values := make([]interface{}, 0, len(conditions))
	for _, condition := range conditions {
		values = append(values, condition)
	}
	tenant.Object["status"] = map[string]interface{}{"conditions": values}
	return tenant
}
