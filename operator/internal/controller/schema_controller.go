package controller

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	tamossv1alpha1 "github.com/livewyer-ops/tamoss/operator/api/v1alpha1"
	"github.com/livewyer-ops/tamoss/operator/internal/controller/defaults"
	operatormetrics "github.com/livewyer-ops/tamoss/operator/internal/metrics"
	schemabundle "github.com/livewyer-ops/tamoss/operator/internal/schema"
	operatorstatus "github.com/livewyer-ops/tamoss/operator/internal/status"
)

const (
	schemaStateAppliedVersionKey = "appliedVersion"
	schemaStateLastAppliedAtKey  = "lastAppliedAt"
	schemaStateJobUIDKey         = "jobUID"
	schemaStateFixturesKey       = "fixturesApplied"
	schemaStateSupportedTAMSAPI  = "supportedTAMSAPI"
	schemaStateFailureCountKey   = "failureCount"
	schemaStateFailedGeneration  = "failedGeneration"
	schemaStateFailedVersion     = "failedVersion"
	schemaStateFailedJobUID      = "failedJobUID"
	schemaStateFailedAtKey       = "failedAt"
)

type SchemaResult struct {
	Ready           bool
	Version         string
	ManagedObjects  []client.Object
	Degraded        bool
	Reason          string
	Message         string
	SchemaMigration tamossv1alpha1.SchemaMigrationStatus
	RecoveryEvent   *recoveryActionEvent
}

type SchemaController struct {
	Client client.Client
	Scheme *runtime.Scheme
}

func schemaMigrationPostgresClientImage(tamoss *tamossv1alpha1.Tamoss) string {
	if tamoss.Spec.Images.SchemaMigrationPostgresClient != "" {
		return tamoss.Spec.Images.SchemaMigrationPostgresClient
	}
	return defaults.DefaultPostgresClientImage
}

func schemaMigrationRuntimeImage(tamoss *tamossv1alpha1.Tamoss) string {
	return resolvedImageRef(tamoss.Spec.API.Image, defaults.DefaultAPIRepository)
}

func (s *SchemaController) Reconcile(ctx context.Context, tamoss *tamossv1alpha1.Tamoss) (SchemaResult, error) {
	state, stateFound, err := s.getSchemaState(ctx, tamoss)
	if err != nil {
		return SchemaResult{}, err
	}
	managed := schemaManagedObjects(state, stateFound)
	if result, done := observedSchemaStateResult(state, stateFound, managed); done {
		return result, nil
	}

	includeFixtures := tamoss.Spec.Backends.DB.ShouldApplyFixtures() && !stateFound
	job := schemaMigrationJob(tamoss, includeFixtures)
	managed = append(managed, job)
	liveJob, jobFound, err := s.getSchemaJob(ctx, job)
	if err != nil {
		return SchemaResult{}, err
	}
	if result, done, err := s.reconcileSucceededSchemaJob(ctx, tamoss, state, managed, includeFixtures, liveJob, jobFound); err != nil || done {
		return result, err
	}
	if result, done, err := s.reconcileSchemaRetryStage(ctx, tamoss, state, managed, liveJob, jobFound); err != nil || done {
		return result, err
	}
	if result, done, err := s.reconcileFailedSchemaJob(ctx, tamoss, state, managed, liveJob, jobFound); err != nil || done {
		return result, err
	}
	if result, done := runningSchemaJobResult(managed, liveJob, jobFound); done {
		return result, nil
	}
	return s.launchSchemaJob(ctx, tamoss, managed, job)
}

func schemaManagedObjects(state *corev1.ConfigMap, stateFound bool) []client.Object {
	if !stateFound {
		return []client.Object{}
	}
	return []client.Object{state}
}

func observedSchemaStateResult(state *corev1.ConfigMap, stateFound bool, managed []client.Object) (SchemaResult, bool) {
	if !stateFound {
		return SchemaResult{}, false
	}
	if !schemabundle.IsSupportedStartingVersion(state.Data[schemaStateAppliedVersionKey]) {
		observed := state.Data[schemaStateAppliedVersionKey]
		return SchemaResult{
			Ready:           false,
			Version:         schemabundle.SchemaVersion,
			ManagedObjects:  managed,
			Degraded:        true,
			Reason:          operatorstatus.ReasonUnsupportedSchemaVersion,
			Message:         fmt.Sprintf("Observed schema revision %q is not supported by this operator", observed),
			SchemaMigration: unsupportedSchemaMigrationStatus(observed),
		}, true
	}
	if state.Data[schemaStateAppliedVersionKey] == schemabundle.SchemaVersion {
		operatormetrics.RecordSchemaMigration("skipped")
		return SchemaResult{
			Ready:           true,
			Version:         schemabundle.SchemaVersion,
			ManagedObjects:  managed,
			Reason:          operatorstatus.ReasonAlreadyAtVersion,
			Message:         "Schema state already records the current version",
			SchemaMigration: schemaMigrationFromState(state, operatorstatus.PhaseSucceeded, operatorstatus.PhaseSucceeded),
		}, true
	}
	return SchemaResult{}, false
}

func (s *SchemaController) reconcileSucceededSchemaJob(ctx context.Context, tamoss *tamossv1alpha1.Tamoss, state *corev1.ConfigMap, managed []client.Object, includeFixtures bool, liveJob *batchv1.Job, jobFound bool) (SchemaResult, bool, error) {
	if !jobFound || !jobSucceeded(liveJob) {
		return SchemaResult{}, false, nil
	}
	operatormetrics.RecordSchemaMigration("succeeded")
	state = schemaStateConfigMap(tamoss, liveJob, includeFixtures, state)
	if err := s.applyOwned(ctx, tamoss, state); err != nil {
		return SchemaResult{}, true, err
	}
	managed = append(managed, state)
	return SchemaResult{
		Ready:           true,
		Version:         schemabundle.SchemaVersion,
		ManagedObjects:  managed,
		Reason:          operatorstatus.ReasonSchemaApplied,
		Message:         "Schema migration completed successfully",
		SchemaMigration: schemaMigrationFromState(state, operatorstatus.PhaseSucceeded, operatorstatus.PhaseSucceeded),
	}, true, nil
}

func (s *SchemaController) reconcileSchemaRetryStage(ctx context.Context, tamoss *tamossv1alpha1.Tamoss, state *corev1.ConfigMap, managed []client.Object, liveJob *batchv1.Job, jobFound bool) (SchemaResult, bool, error) {
	accepted, resetState, err := s.acceptSchemaRetry(ctx, tamoss, state, liveJob, jobFound)
	if err != nil || !accepted {
		return SchemaResult{}, accepted, err
	}
	managed = append(managed, resetState)
	return SchemaResult{
		Ready:           false,
		Version:         schemabundle.SchemaVersion,
		ManagedObjects:  managed,
		Reason:          operatorstatus.ReasonSchemaRetryAccepted,
		Message:         "Schema retry annotation accepted; failed migration state was reset",
		SchemaMigration: schemaMigrationFromState(resetState, "Retrying", "RetryAccepted"),
		RecoveryEvent: &recoveryActionEvent{
			Type:    corev1.EventTypeNormal,
			Reason:  operatorstatus.ReasonSchemaRetryAccepted,
			Message: "Schema retry annotation accepted; failed migration Job was deleted and failure counter was cleared",
		},
	}, true, nil
}

func (s *SchemaController) reconcileFailedSchemaJob(ctx context.Context, tamoss *tamossv1alpha1.Tamoss, state *corev1.ConfigMap, managed []client.Object, liveJob *batchv1.Job, jobFound bool) (SchemaResult, bool, error) {
	if !jobFound || !jobFailed(liveJob) {
		return SchemaResult{}, false, nil
	}
	operatormetrics.RecordSchemaMigration("failed")
	if terminalSchemaFailure(state, tamoss) {
		operatormetrics.RecordSchemaMigration("blocked")
		return terminalSchemaFailureResult(state, managed), true, nil
	}
	failureCount := nextFailureCount(state, tamoss)
	state = schemaFailureStateConfigMap(tamoss, liveJob, state, failureCount)
	if err := s.applyOwned(ctx, tamoss, state); err != nil {
		return SchemaResult{}, true, err
	}
	managed = append(managed, state)
	if failureCount >= 3 {
		operatormetrics.RecordSchemaMigration("blocked")
		return terminalSchemaFailureResult(state, managed), true, nil
	}
	return SchemaResult{
		Ready:           false,
		Version:         schemabundle.SchemaVersion,
		ManagedObjects:  managed,
		Reason:          operatorstatus.ReasonSchemaMigrationFailed,
		Message:         fmt.Sprintf("Schema migration job failed; observed failed attempt %d of 3 before marking degraded", failureCount),
		SchemaMigration: schemaMigrationFromState(state, operatorstatus.PhaseFailed, operatorstatus.PhaseFailed),
	}, true, nil
}

func terminalSchemaFailureResult(state *corev1.ConfigMap, managed []client.Object) SchemaResult {
	return SchemaResult{
		Ready:           false,
		Version:         schemabundle.SchemaVersion,
		ManagedObjects:  managed,
		Degraded:        true,
		Reason:          operatorstatus.ReasonSchemaMigrationFailed,
		Message:         "Schema migration failed three consecutive reconciles",
		SchemaMigration: schemaMigrationFromState(state, operatorstatus.PhaseFailed, operatorstatus.PhaseFailed),
	}
}

func runningSchemaJobResult(managed []client.Object, liveJob *batchv1.Job, jobFound bool) (SchemaResult, bool) {
	if !jobFound {
		return SchemaResult{}, false
	}
	return SchemaResult{
		Ready:           false,
		Version:         schemabundle.SchemaVersion,
		ManagedObjects:  managed,
		Reason:          operatorstatus.ReasonMigrationInProgress,
		Message:         fmt.Sprintf("Schema migration job %s is running", liveJob.Name),
		SchemaMigration: schemaMigrationFromJob(liveJob, operatorstatus.PhaseRunning, operatorstatus.PhaseRunning),
	}, true
}

func (s *SchemaController) launchSchemaJob(ctx context.Context, tamoss *tamossv1alpha1.Tamoss, managed []client.Object, job *batchv1.Job) (SchemaResult, error) {
	if err := s.applyOwned(ctx, tamoss, job); err != nil {
		return SchemaResult{}, err
	}
	operatormetrics.RecordSchemaMigration("launched")
	return SchemaResult{
		Ready:           false,
		Version:         schemabundle.SchemaVersion,
		ManagedObjects:  managed,
		Reason:          operatorstatus.ReasonMigrationInProgress,
		Message:         "Schema migration job was launched",
		SchemaMigration: schemaMigrationFromJob(job, operatorstatus.PhaseRunning, operatorstatus.PhaseRunning),
	}, nil
}

func (s *SchemaController) acceptSchemaRetry(ctx context.Context, tamoss *tamossv1alpha1.Tamoss, state *corev1.ConfigMap, liveJob *batchv1.Job, jobFound bool) (bool, *corev1.ConfigMap, error) {
	value := strings.TrimSpace(tamoss.Annotations[AnnotationSchemaRetry])
	if value == "" || !terminalSchemaFailure(state, tamoss) {
		return false, nil, nil
	}
	if state != nil && state.Annotations[annotationSchemaRetryDone] == value {
		return false, nil, nil
	}
	if jobFound && liveJob != nil && jobFailed(liveJob) {
		if err := s.Client.Delete(ctx, liveJob); err != nil && !apierrors.IsNotFound(err) {
			return false, nil, err
		}
	}
	resetState := schemaRetryStateConfigMap(tamoss, state, value)
	if err := s.applyOwned(ctx, tamoss, resetState); err != nil {
		return false, nil, err
	}
	return true, resetState, nil
}

func (s *SchemaController) applyOwned(ctx context.Context, tamoss *tamossv1alpha1.Tamoss, obj client.Object) error {
	if err := controllerutil.SetControllerReference(tamoss, obj, s.Scheme); err != nil {
		return err
	}
	_, err := applyCanonicalObject(ctx, s.Client, obj)
	return err
}

func (s *SchemaController) getSchemaState(ctx context.Context, tamoss *tamossv1alpha1.Tamoss) (*corev1.ConfigMap, bool, error) {
	state := &corev1.ConfigMap{}
	err := s.Client.Get(ctx, client.ObjectKey{Name: tamossResourceName(tamoss, "schema-state"), Namespace: tamoss.Namespace}, state)
	if err == nil {
		return state, true, nil
	}
	if apierrors.IsNotFound(err) {
		return nil, false, nil
	}
	return nil, false, err
}

func (s *SchemaController) getSchemaJob(ctx context.Context, desired *batchv1.Job) (*batchv1.Job, bool, error) {
	job := &batchv1.Job{}
	err := s.Client.Get(ctx, client.ObjectKeyFromObject(desired), job)
	if err == nil {
		return job, true, nil
	}
	if apierrors.IsNotFound(err) {
		return nil, false, nil
	}
	return nil, false, err
}

func schemaStateConfigMap(tamoss *tamossv1alpha1.Tamoss, job *batchv1.Job, fixturesApplied bool, previous *corev1.ConfigMap) *corev1.ConfigMap {
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:        tamossResourceName(tamoss, "schema-state"),
			Namespace:   tamoss.Namespace,
			Labels:      schemaLabels(tamoss),
			Annotations: schemaStateAnnotations(previous),
		},
		Data: map[string]string{
			schemaStateAppliedVersionKey: schemabundle.SchemaVersion,
			schemaStateLastAppliedAtKey:  time.Now().UTC().Format(time.RFC3339),
			schemaStateJobUIDKey:         string(job.UID),
			schemaStateFixturesKey:       fmt.Sprintf("%t", fixturesApplied),
			schemaStateSupportedTAMSAPI:  schemabundle.SupportedTAMSAPIVersion,
		},
	}
}

func schemaFailureStateConfigMap(tamoss *tamossv1alpha1.Tamoss, job *batchv1.Job, previous *corev1.ConfigMap, failureCount int) *corev1.ConfigMap {
	data := map[string]string{}
	if previous != nil {
		for key, value := range previous.Data {
			data[key] = value
		}
	}
	data[schemaStateFailureCountKey] = strconv.Itoa(failureCount)
	data[schemaStateFailedGeneration] = strconv.FormatInt(tamoss.Generation, 10)
	data[schemaStateFailedVersion] = schemabundle.SchemaVersion
	data[schemaStateFailedJobUID] = string(job.UID)
	data[schemaStateFailedAtKey] = time.Now().UTC().Format(time.RFC3339)
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:        tamossResourceName(tamoss, "schema-state"),
			Namespace:   tamoss.Namespace,
			Labels:      schemaLabels(tamoss),
			Annotations: schemaStateAnnotations(previous),
		},
		Data: data,
	}
}

func schemaStateAnnotations(previous *corev1.ConfigMap) map[string]string {
	if previous == nil || len(previous.Annotations) == 0 {
		return nil
	}
	annotations := map[string]string{}
	if value := previous.Annotations[annotationSchemaRetryDone]; value != "" {
		annotations[annotationSchemaRetryDone] = value
	}
	if len(annotations) == 0 {
		return nil
	}
	return annotations
}

func schemaRetryStateConfigMap(tamoss *tamossv1alpha1.Tamoss, previous *corev1.ConfigMap, value string) *corev1.ConfigMap {
	data := map[string]string{}
	annotations := map[string]string{annotationSchemaRetryDone: value}
	if previous != nil {
		for key, item := range previous.Data {
			data[key] = item
		}
		for key, item := range previous.Annotations {
			annotations[key] = item
		}
	}
	delete(data, schemaStateFailureCountKey)
	delete(data, schemaStateFailedGeneration)
	delete(data, schemaStateFailedVersion)
	delete(data, schemaStateFailedJobUID)
	delete(data, schemaStateFailedAtKey)
	annotations[annotationSchemaRetryDone] = value
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:        tamossResourceName(tamoss, "schema-state"),
			Namespace:   tamoss.Namespace,
			Labels:      schemaLabels(tamoss),
			Annotations: annotations,
		},
		Data: data,
	}
}

func schemaMigrationFromState(state *corev1.ConfigMap, phase, result string) tamossv1alpha1.SchemaMigrationStatus {
	observed := ""
	if state != nil {
		observed = state.Data[schemaStateAppliedVersionKey]
	}
	status := tamossv1alpha1.SchemaMigrationStatus{
		Phase:                     phase,
		LastAttemptResult:         result,
		Attempts:                  schemaFailureAttempts(state),
		AppliedRevision:           observed,
		ObservedRevision:          observed,
		CurrentRevision:           schemabundle.SchemaVersion,
		PreviousSupportedRevision: schemabundle.PreviousSupportedSchemaVersion,
		SupportedTAMSAPI:          schemabundle.SupportedTAMSAPIVersion,
	}
	if phase == operatorstatus.PhaseSucceeded && status.Attempts == 0 {
		status.Attempts = 1
	}
	if timestamp := schemaStateTime(state); timestamp != nil {
		status.LastAttemptTime = timestamp
	}
	return status
}

func schemaFailureAttempts(state *corev1.ConfigMap) int32 {
	count := schemaFailureCount(state)
	if count > math.MaxInt32 {
		return math.MaxInt32
	}
	return int32(count) //nolint:gosec // Count is clamped to math.MaxInt32 above.
}

func schemaMigrationFromJob(job *batchv1.Job, phase, result string) tamossv1alpha1.SchemaMigrationStatus {
	timestamp := job.CreationTimestamp
	if timestamp.IsZero() {
		timestamp = metav1.Now()
	}
	return tamossv1alpha1.SchemaMigrationStatus{
		Phase:                     phase,
		LastAttemptTime:           &timestamp,
		LastAttemptResult:         result,
		Attempts:                  1,
		CurrentRevision:           schemabundle.SchemaVersion,
		PreviousSupportedRevision: schemabundle.PreviousSupportedSchemaVersion,
		SupportedTAMSAPI:          schemabundle.SupportedTAMSAPIVersion,
	}
}

func unsupportedSchemaMigrationStatus(observed string) tamossv1alpha1.SchemaMigrationStatus {
	return tamossv1alpha1.SchemaMigrationStatus{
		Phase:                     operatorstatus.PhaseBlocked,
		LastAttemptResult:         operatorstatus.ReasonUnsupportedSchemaVersion,
		ObservedRevision:          observed,
		CurrentRevision:           schemabundle.SchemaVersion,
		PreviousSupportedRevision: schemabundle.PreviousSupportedSchemaVersion,
		SupportedTAMSAPI:          schemabundle.SupportedTAMSAPIVersion,
	}
}

func schemaStateTime(state *corev1.ConfigMap) *metav1.Time {
	if state == nil {
		return nil
	}
	raw := state.Data[schemaStateLastAppliedAtKey]
	if raw == "" {
		raw = state.Data[schemaStateFailedAtKey]
	}
	if raw == "" {
		return nil
	}
	parsed, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return nil
	}
	timestamp := metav1.NewTime(parsed)
	return &timestamp
}

func schemaFailureCount(state *corev1.ConfigMap) int {
	if state == nil {
		return 0
	}
	count, err := strconv.Atoi(state.Data[schemaStateFailureCountKey])
	if err != nil {
		return 0
	}
	return count
}

func nextFailureCount(state *corev1.ConfigMap, tamoss *tamossv1alpha1.Tamoss) int {
	if state == nil {
		return 1
	}
	if state.Data[schemaStateFailedVersion] != schemabundle.SchemaVersion {
		return 1
	}
	if state.Data[schemaStateFailedGeneration] != strconv.FormatInt(tamoss.Generation, 10) {
		return 1
	}
	count, err := strconv.Atoi(state.Data[schemaStateFailureCountKey])
	if err != nil {
		return 1
	}
	return count + 1
}

func terminalSchemaFailure(state *corev1.ConfigMap, tamoss *tamossv1alpha1.Tamoss) bool {
	if state == nil {
		return false
	}
	if state.Data[schemaStateFailedVersion] != schemabundle.SchemaVersion {
		return false
	}
	if state.Data[schemaStateFailedGeneration] != strconv.FormatInt(tamoss.Generation, 10) {
		return false
	}
	count, err := strconv.Atoi(state.Data[schemaStateFailureCountKey])
	return err == nil && count >= 3
}

func jobSucceeded(job *batchv1.Job) bool {
	if job.Status.Succeeded > 0 {
		return true
	}
	for _, condition := range job.Status.Conditions {
		if condition.Type == batchv1.JobComplete && condition.Status == corev1.ConditionTrue {
			return true
		}
	}
	return false
}

func jobFailed(job *batchv1.Job) bool {
	if job.Status.Failed > 0 {
		return true
	}
	for _, condition := range job.Status.Conditions {
		if condition.Type == batchv1.JobFailed && condition.Status == corev1.ConditionTrue {
			return true
		}
	}
	return false
}

func schemaMigrationJob(tamoss *tamossv1alpha1.Tamoss, includeFixtures bool) *batchv1.Job {
	backoffLimit := int32(0)
	args := schemaMigrationArgs(tamoss, includeFixtures)
	return &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      tamossResourceName(tamoss, "schema-migrate-"+schemaVersionForName()),
			Namespace: tamoss.Namespace,
			Labels:    schemaLabels(tamoss),
		},
		Spec: batchv1.JobSpec{
			BackoffLimit: &backoffLimit,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: schemaLabels(tamoss),
				},
				Spec: corev1.PodSpec{
					RestartPolicy: corev1.RestartPolicyNever,
					Containers: []corev1.Container{{
						Name:            "schema-migrate",
						Image:           schemaMigrationRuntimeImage(tamoss),
						ImagePullPolicy: corev1.PullIfNotPresent,
						Command:         []string{"uv"},
						Args:            args,
						Env:             schemaMigrationEnv(tamoss),
					}},
				},
			},
		},
	}
}

func schemaMigrationArgs(tamoss *tamossv1alpha1.Tamoss, includeFixtures bool) []string {
	args := []string{"run", "tamoss-db", "migrate"}
	if includeFixtures {
		args = append(args, "--apply-fixtures")
	}
	if tamoss.Spec.Backends.DB.Provider() == tamossv1alpha1.BackendProvidedByCNPG {
		args = append(args, "--apply-cnpg-ownership")
	}
	return args
}

func schemaMigrationEnv(tamoss *tamossv1alpha1.Tamoss) []corev1.EnvVar {
	db := tamoss.DBConnection()
	secretName := db.Auth.ExistingSecret
	if tamoss.Spec.Backends.DB.Provider() == tamossv1alpha1.BackendProvidedByCNPG {
		secretName = tamossResourceName(tamoss, "db-superuser")
	}
	return []corev1.EnvVar{
		{Name: "POSTGRES_HOST", Value: db.Host},
		{Name: "POSTGRES_PORT", Value: db.Port},
		{Name: "POSTGRES_DB", Value: db.Database},
		{
			Name: "POSTGRES_USER",
			ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{Name: secretName},
				Key:                  db.Auth.SecretKeys.Username,
			}},
		},
		{
			Name: "POSTGRES_PASSWORD",
			ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{Name: secretName},
				Key:                  db.Auth.SecretKeys.Password,
			}},
		},
	}
}

func schemaVersionForName() string {
	version := strings.TrimPrefix(schemabundle.SchemaVersion, "v")
	if version == "" {
		version = schemabundle.DevelopmentSchemaVersion
	}
	version = strings.NewReplacer(".", "-", "_", "-").Replace(version)
	return strings.ToLower(version)
}

func schemaLabels(tamoss *tamossv1alpha1.Tamoss) map[string]string {
	return map[string]string{
		"app.kubernetes.io/name":       "tamoss",
		"app.kubernetes.io/instance":   tamoss.Name,
		"app.kubernetes.io/component":  "schema",
		"app.kubernetes.io/managed-by": "tamoss-operator",
	}
}
