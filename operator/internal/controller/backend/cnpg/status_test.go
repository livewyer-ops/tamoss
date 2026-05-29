package cnpg

import (
	"testing"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	operatorstatus "github.com/livewyer-ops/tamoss/operator/internal/status"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestRollupStatusReady(t *testing.T) {
	cluster := cnpgClusterWithConditions(metav1.Condition{
		Type:    string(cnpgv1.ConditionClusterReady),
		Status:  metav1.ConditionTrue,
		Reason:  string(cnpgv1.ClusterReady),
		Message: "Cluster is ready",
	})

	condition, events := RollupStatus(cluster)
	if condition.Type != operatorstatus.ConditionBackendsReady || condition.Status != metav1.ConditionTrue {
		t.Fatalf("expected BackendsReady true, got %#v", condition)
	}
	if len(events) != 0 {
		t.Fatalf("expected no events, got %#v", events)
	}
}

func TestRollupStatusNotReady(t *testing.T) {
	cluster := cnpgClusterWithConditions(metav1.Condition{
		Type:    string(cnpgv1.ConditionClusterReady),
		Status:  metav1.ConditionFalse,
		Reason:  string(cnpgv1.ClusterIsNotReady),
		Message: "initializing",
	})

	condition, _ := RollupStatus(cluster)
	if condition.Status != metav1.ConditionFalse {
		t.Fatalf("expected BackendsReady false, got %#v", condition)
	}
	if condition.Reason != string(cnpgv1.ClusterIsNotReady) || condition.Message != "initializing" {
		t.Fatalf("expected CNPG reason/message to pass through, got %#v", condition)
	}
}

func TestRollupStatusContinuousArchivingEvent(t *testing.T) {
	cluster := cnpgClusterWithConditions(metav1.Condition{
		Type:    string(cnpgv1.ConditionContinuousArchiving),
		Status:  metav1.ConditionFalse,
		Reason:  "WALArchiveFailing",
		Message: "archive command failed",
	})

	_, events := RollupStatus(cluster)
	if len(events) != 1 {
		t.Fatalf("expected one event, got %#v", events)
	}
	if events[0].Reason != operatorstatus.ReasonBackupArchivingFailed {
		t.Fatalf("expected backup failure event, got %#v", events[0])
	}
	if events[0].Message != "CNPG ContinuousArchiving is WALArchiveFailing: archive command failed" {
		t.Fatalf("unexpected event message %q", events[0].Message)
	}
}

func cnpgClusterWithConditions(conditions ...metav1.Condition) *cnpgv1.Cluster {
	return &cnpgv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{Name: "example-db"},
		Status: cnpgv1.ClusterStatus{
			Conditions: conditions,
		},
	}
}
