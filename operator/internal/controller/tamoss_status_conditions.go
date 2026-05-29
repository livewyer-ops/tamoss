package controller

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	tamossv1alpha1 "github.com/livewyer-ops/tamoss/operator/api/v1alpha1"
	operatorstatus "github.com/livewyer-ops/tamoss/operator/internal/status"
)

func setUpgradeStatusFromSchema(tamoss *tamossv1alpha1.Tamoss, schemaResult SchemaResult) {
	tamoss.Status.SchemaMigration = schemaResult.SchemaMigration
	switch {
	case schemaResult.Degraded:
		setUpgradeStatus(
			tamoss,
			operatorstatus.PhaseBlocked,
			metav1.ConditionFalse,
			schemaReason(schemaResult),
			schemaMessage(schemaResult),
		)
	case !schemaResult.Ready:
		setUpgradeStatus(
			tamoss,
			operatorstatus.PhaseProgressing,
			metav1.ConditionUnknown,
			schemaReason(schemaResult),
			schemaMessage(schemaResult),
		)
	default:
		setUpgradeStatus(
			tamoss,
			operatorstatus.PhaseUpgradeable,
			metav1.ConditionTrue,
			operatorstatus.ReasonUpgradeReady,
			"Desired schema state can complete safely",
		)
	}
}

func setUpgradeUnknown(tamoss *tamossv1alpha1.Tamoss, reason, message string) {
	setUpgradeStatus(tamoss, operatorstatus.PhaseUnknown, metav1.ConditionUnknown, reason, message)
}

func setUpgradeStatus(tamoss *tamossv1alpha1.Tamoss, phase string, upgradeable metav1.ConditionStatus, reason, message string) {
	tamoss.Status.Upgrade = tamossv1alpha1.UpgradeStatus{
		Phase:   phase,
		Reason:  reason,
		Message: message,
	}
	operatorstatus.SetConditionStatus(&tamoss.Status.Conditions, operatorstatus.ConditionUpgradeable, upgradeable, reason, message)
}
