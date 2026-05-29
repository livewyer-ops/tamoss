package cnpg

import (
	"fmt"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	operatorstatus "github.com/livewyer-ops/tamoss/operator/internal/status"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type StatusEvent struct {
	Reason  string
	Message string
}

func RollupStatus(cluster *cnpgv1.Cluster) (metav1.Condition, []StatusEvent) {
	ready := meta.FindStatusCondition(cluster.Status.Conditions, string(cnpgv1.ConditionClusterReady))
	condition := metav1.Condition{
		Type:    operatorstatus.ConditionBackendsReady,
		Status:  metav1.ConditionFalse,
		Reason:  operatorstatus.ReasonClusterNotReady,
		Message: fmt.Sprintf("CNPG Cluster %s is not ready", cluster.Name),
	}
	if ready != nil {
		condition.Status = ready.Status
		condition.Reason = ready.Reason
		condition.Message = ready.Message
	}

	events := []StatusEvent{}
	continuousArchiving := meta.FindStatusCondition(cluster.Status.Conditions, string(cnpgv1.ConditionContinuousArchiving))
	if continuousArchiving != nil && continuousArchiving.Status == metav1.ConditionFalse {
		events = append(events, StatusEvent{
			Reason:  operatorstatus.ReasonBackupArchivingFailed,
			Message: fmt.Sprintf("CNPG ContinuousArchiving is %s: %s", continuousArchiving.Reason, continuousArchiving.Message),
		})
	}
	return condition, events
}
