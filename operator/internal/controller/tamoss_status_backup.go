package controller

import (
	"context"
	"fmt"
	"strings"
	"time"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	tamossv1alpha1 "github.com/livewyer-ops/tamoss/operator/api/v1alpha1"
	operatorstatus "github.com/livewyer-ops/tamoss/operator/internal/status"
)

func setBackupPolicyCondition(conditions *[]metav1.Condition, tamoss *tamossv1alpha1.Tamoss) {
	var condition metav1.Condition
	if tamoss.Spec.Backends.DB.Provider() != tamossv1alpha1.BackendProvidedByCNPG {
		condition = backupPolicyCondition(tamoss.Generation, metav1.ConditionUnknown, operatorstatus.ReasonBackupPolicyNotManaged, "Database backup policy is owned outside TAMOSS for this provider")
		meta.SetStatusCondition(conditions, condition)
		tamoss.Status.BackupPolicy = backupPolicyStatus(tamoss, condition)
		return
	}
	if tamoss.Spec.Backends.DB.CNPG == nil || !tamoss.Spec.Backends.DB.CNPG.Backup.Enabled {
		condition = backupPolicyCondition(tamoss.Generation, metav1.ConditionTrue, operatorstatus.ReasonBackupPolicyDisabled, "Managed CNPG backup policy is disabled")
		meta.SetStatusCondition(conditions, condition)
		tamoss.Status.BackupPolicy = backupPolicyStatus(tamoss, condition)
		return
	}
	if missing := cnpgBackupPolicyMissingFields(tamoss); len(missing) > 0 {
		condition = backupPolicyCondition(tamoss.Generation, metav1.ConditionFalse, operatorstatus.ReasonBackupPolicyIncomplete, fmt.Sprintf("CNPG backup policy is missing required fields: %s", strings.Join(missing, ", ")))
		meta.SetStatusCondition(conditions, condition)
		tamoss.Status.BackupPolicy = backupPolicyStatus(tamoss, condition)
		return
	}
	condition = backupPolicyCondition(tamoss.Generation, metav1.ConditionTrue, operatorstatus.ReasonBackupPolicyConfigured, "Managed CNPG backup policy is configured")
	meta.SetStatusCondition(conditions, condition)
	tamoss.Status.BackupPolicy = backupPolicyStatus(tamoss, condition)
}

func (r *TamossReconciler) refreshObservedBackupPolicyCondition(ctx context.Context, tamoss *tamossv1alpha1.Tamoss) error {
	if tamoss.Spec.Backends.DB.Provider() != tamossv1alpha1.BackendProvidedByCNPG ||
		tamoss.Spec.Backends.DB.CNPG == nil ||
		!tamoss.Spec.Backends.DB.CNPG.Backup.Enabled ||
		len(cnpgBackupPolicyMissingFields(tamoss)) > 0 {
		return nil
	}
	condition, status, err := r.observedBackupPolicy(ctx, tamoss)
	if err != nil {
		return err
	}
	meta.SetStatusCondition(&tamoss.Status.Conditions, condition)
	tamoss.Status.BackupPolicy = status
	return nil
}

func (r *TamossReconciler) observedBackupPolicy(ctx context.Context, tamoss *tamossv1alpha1.Tamoss) (metav1.Condition, tamossv1alpha1.BackupPolicyStatus, error) {
	scheduled := &cnpgv1.ScheduledBackup{}
	scheduledKey := types.NamespacedName{Name: tamoss.ResourceName("db-backup"), Namespace: tamoss.Namespace}
	if err := r.Client.Get(ctx, scheduledKey, scheduled); err != nil {
		if apierrors.IsNotFound(err) {
			condition := backupPolicyCondition(tamoss.Generation, metav1.ConditionFalse, operatorstatus.ReasonBackupPolicyMissing, fmt.Sprintf("CNPG ScheduledBackup %s has not been observed", scheduledKey.Name))
			return condition, backupPolicyStatus(tamoss, condition), nil
		}
		return metav1.Condition{}, tamossv1alpha1.BackupPolicyStatus{}, err
	}

	cluster := &cnpgv1.Cluster{}
	clusterKey := types.NamespacedName{Name: tamoss.ResourceName("db"), Namespace: tamoss.Namespace}
	if err := r.Client.Get(ctx, clusterKey, cluster); err != nil {
		if apierrors.IsNotFound(err) {
			condition := backupPolicyCondition(tamoss.Generation, metav1.ConditionFalse, operatorstatus.ReasonBackupPolicyPending, fmt.Sprintf("CNPG Cluster %s has not been observed for backup status", clusterKey.Name))
			return condition, backupPolicyStatus(tamoss, condition), nil
		}
		return metav1.Condition{}, tamossv1alpha1.BackupPolicyStatus{}, err
	}

	continuousArchiving := meta.FindStatusCondition(cluster.Status.Conditions, string(cnpgv1.ConditionContinuousArchiving))
	if continuousArchiving == nil {
		condition := backupPolicyCondition(tamoss.Generation, metav1.ConditionFalse, operatorstatus.ReasonBackupArchivingUnknown, fmt.Sprintf("CNPG Cluster %s has no ContinuousArchiving condition yet", cluster.Name))
		status := backupPolicyStatus(tamoss, condition)
		status = backupPolicyStatusWithCluster(status, cluster)
		return condition, status, nil
	}
	if continuousArchiving.Status == metav1.ConditionFalse {
		condition := backupPolicyCondition(tamoss.Generation, metav1.ConditionFalse, operatorstatus.ReasonBackupArchivingFailed, fmt.Sprintf("CNPG ContinuousArchiving is %s: %s", continuousArchiving.Reason, continuousArchiving.Message))
		status := backupPolicyStatus(tamoss, condition)
		status = backupPolicyStatusWithCluster(status, cluster)
		return condition, status, nil
	}
	if backupFailureNewerThanSuccess(cluster.Status.LastFailedBackup, cluster.Status.LastSuccessfulBackup) { //nolint:staticcheck // CNPG deprecates these fields for backup plugins only; this operator configures the in-tree barman object store, which still populates them.
		condition := backupPolicyCondition(tamoss.Generation, metav1.ConditionFalse, operatorstatus.ReasonBackupPolicyFailed, fmt.Sprintf("CNPG Cluster %s reports a backup failure newer than the latest successful backup", cluster.Name))
		status := backupPolicyStatus(tamoss, condition)
		status = backupPolicyStatusWithCluster(status, cluster)
		return condition, status, nil
	}
	if cluster.Status.LastSuccessfulBackup == "" && cluster.Status.FirstRecoverabilityPoint == "" && scheduled.Status.LastScheduleTime == nil { //nolint:staticcheck // CNPG deprecates these fields for backup plugins only; this operator configures the in-tree barman object store, which still populates them.
		condition := backupPolicyCondition(tamoss.Generation, metav1.ConditionFalse, operatorstatus.ReasonBackupPolicyPending, fmt.Sprintf("CNPG ScheduledBackup %s exists but no successful backup has been observed yet", scheduled.Name))
		status := backupPolicyStatus(tamoss, condition)
		status = backupPolicyStatusWithCluster(status, cluster)
		return condition, status, nil
	}
	condition := backupPolicyCondition(tamoss.Generation, metav1.ConditionTrue, operatorstatus.ReasonBackupPolicyHealthy, fmt.Sprintf("CNPG ScheduledBackup %s and Cluster %s report backup health", scheduled.Name, cluster.Name))
	status := backupPolicyStatus(tamoss, condition)
	status = backupPolicyStatusWithCluster(status, cluster)
	return condition, status, nil
}

func backupPolicyCondition(generation int64, status metav1.ConditionStatus, reason, message string) metav1.Condition {
	return metav1.Condition{
		Type:               operatorstatus.ConditionBackupPolicyReady,
		Status:             status,
		ObservedGeneration: generation,
		Reason:             reason,
		Message:            message,
	}
}

func backupPolicyStatus(tamoss *tamossv1alpha1.Tamoss, condition metav1.Condition) tamossv1alpha1.BackupPolicyStatus {
	managed := tamoss.Spec.Backends.DB.Provider() == tamossv1alpha1.BackendProvidedByCNPG
	enabled := managed && tamoss.Spec.Backends.DB.CNPG != nil && tamoss.Spec.Backends.DB.CNPG.Backup.Enabled
	status := tamossv1alpha1.BackupPolicyStatus{
		Managed: managed,
		Enabled: enabled,
		Status:  condition.Status,
		Reason:  condition.Reason,
		Message: condition.Message,
	}
	if managed {
		status.Cluster = tamoss.ResourceName("db")
	}
	if enabled {
		status.ScheduledBackup = tamoss.ResourceName("db-backup")
	}
	return status
}

//nolint:staticcheck // CNPG deprecates these fields for backup plugins only; this operator configures the in-tree barman object store, which still populates them.
func backupPolicyStatusWithCluster(status tamossv1alpha1.BackupPolicyStatus, cluster *cnpgv1.Cluster) tamossv1alpha1.BackupPolicyStatus {
	status.LastSuccessfulBackup = cluster.Status.LastSuccessfulBackup
	status.LastFailedBackup = cluster.Status.LastFailedBackup
	status.FirstRecoverabilityPoint = cluster.Status.FirstRecoverabilityPoint
	return status
}

func backupFailureNewerThanSuccess(failed, successful string) bool {
	if strings.TrimSpace(failed) == "" {
		return false
	}
	failedAt, failedErr := time.Parse(time.RFC3339, failed)
	successAt, successErr := time.Parse(time.RFC3339, successful)
	if failedErr != nil {
		return successful == ""
	}
	if successErr != nil {
		return true
	}
	return failedAt.After(successAt)
}
