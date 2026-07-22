package controller

import (
	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	tamossv1alpha1 "github.com/livewyer-ops/tamoss/operator/api/v1alpha1"
)

func tamossStatusSemanticEqual(a, b tamossv1alpha1.TamossStatus) bool {
	a.Conditions = cloneConditions(a.Conditions)
	b.Conditions = cloneConditions(b.Conditions)
	normalizeConditionTimes(a.Conditions)
	normalizeConditionTimes(b.Conditions)
	a.SchemaMigration.LastAttemptTime = nil
	b.SchemaMigration.LastAttemptTime = nil
	return equality.Semantic.DeepEqual(a, b)
}

func storageBackendStatusSemanticEqual(a, b tamossv1alpha1.StorageBackendStatus) bool {
	a.Conditions = cloneConditions(a.Conditions)
	b.Conditions = cloneConditions(b.Conditions)
	normalizeConditionTimes(a.Conditions)
	normalizeConditionTimes(b.Conditions)
	return equality.Semantic.DeepEqual(a, b)
}

func operationStatusSemanticEqual(a, b tamossv1alpha1.TamossOperationStatus) bool {
	a.Conditions = cloneConditions(a.Conditions)
	b.Conditions = cloneConditions(b.Conditions)
	normalizeConditionTimes(a.Conditions)
	normalizeConditionTimes(b.Conditions)
	return equality.Semantic.DeepEqual(a, b)
}

func cloneConditions(conditions []metav1.Condition) []metav1.Condition {
	if conditions == nil {
		return nil
	}
	return append([]metav1.Condition(nil), conditions...)
}

func normalizeConditionTimes(conditions []metav1.Condition) {
	for i := range conditions {
		conditions[i].LastTransitionTime = metav1.Time{}
	}
}
