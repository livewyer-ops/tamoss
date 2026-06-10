package status

import (
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const MessageReconciliationActive = "Reconciliation is active"

func SetConditionBool(conditions *[]metav1.Condition, generation int64, conditionType string, ok bool, reason, message string) {
	status := metav1.ConditionFalse
	if ok {
		status = metav1.ConditionTrue
	}
	SetConditionStatus(conditions, generation, conditionType, status, reason, message)
}

func SetConditionStatus(conditions *[]metav1.Condition, generation int64, conditionType string, status metav1.ConditionStatus, reason, message string) {
	meta.SetStatusCondition(conditions, metav1.Condition{
		Type:               conditionType,
		Status:             status,
		ObservedGeneration: generation,
		Reason:             reason,
		Message:            message,
	})
}

func SetReconciliationActive(conditions *[]metav1.Condition, generation int64) {
	SetConditionBool(
		conditions,
		generation,
		ConditionPaused,
		false,
		ReasonReconciliationActive,
		MessageReconciliationActive,
	)
}

func ConditionReason(condition *metav1.Condition, fallback string) string {
	if condition != nil && condition.Reason != "" {
		return condition.Reason
	}
	return fallback
}

func ConditionMessage(condition *metav1.Condition, fallback string) string {
	if condition != nil && condition.Message != "" {
		return condition.Message
	}
	return fallback
}

func ConditionBecameTrue(oldConditions, newConditions []metav1.Condition, conditionType string) bool {
	newCondition := meta.FindStatusCondition(newConditions, conditionType)
	if newCondition == nil || newCondition.Status != metav1.ConditionTrue {
		return false
	}
	oldCondition := meta.FindStatusCondition(oldConditions, conditionType)
	return oldCondition == nil || oldCondition.Status != metav1.ConditionTrue
}

func ChangedConditionReason(oldConditions, newConditions []metav1.Condition, conditionType string) (string, string, bool) {
	newCondition := meta.FindStatusCondition(newConditions, conditionType)
	if newCondition == nil {
		return "", "", false
	}
	oldCondition := meta.FindStatusCondition(oldConditions, conditionType)
	if oldCondition != nil &&
		oldCondition.Status == newCondition.Status &&
		oldCondition.Reason == newCondition.Reason &&
		oldCondition.Message == newCondition.Message {
		return "", "", false
	}
	return newCondition.Reason, newCondition.Message, true
}
