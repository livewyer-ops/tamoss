package controller

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/url"
	"reflect"
	"strings"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	tamossv1alpha1 "github.com/livewyer-ops/tamoss/operator/api/v1alpha1"
	operatorstatus "github.com/livewyer-ops/tamoss/operator/internal/status"
)

const (
	ingestRunReadyCondition       = operatorstatus.ConditionReady
	ingestRunProgressingCondition = operatorstatus.ConditionProgressing

	ingestRunLabel       = "tamoss.livewyer.io/ingest-run"
	ingestRunTargetLabel = "tamoss.livewyer.io/tamoss"

	ingestRunRequeuePending = 15 * time.Second
	ingestRunRequeueCancel  = 2 * time.Second
	maxIngestSelectors      = 32
	maxIngestSelectorBytes  = 32 * 1024
)

type IngestRunReconciler struct {
	Client           client.Client
	Scheme           *runtime.Scheme
	WatchNamespaces  WatchNamespaceSet
	TamsinImage      string
	InputResolver    IngestInputResolver
	EndpointResolver IngestEndpointResolver
}

// ResolvedIngestInputs is the private handoff from an approved input service to
// the fixed Tamsin Job template. Selectors are bounded top-level Tamsin inputs,
// such as one staged object, manifest, or S3 prefix. The resolver must not
// expand a large batch into Job arguments.
type ResolvedIngestInputs struct {
	Selectors      []string
	ExpectedInputs int32
}

// IngestInputResolver is the security boundary between an opaque IngestRun
// inputRef and concrete media locations. The manager deliberately leaves this
// unset until a server-owned staging and credential-profile resolver exists.
type IngestInputResolver interface {
	Resolve(context.Context, string, string, tamossv1alpha1.IngestInputReference, int32) (ResolvedIngestInputs, error)
}

// IngestEndpointResolver selects an operator-approved TLS endpoint for a
// Tamoss instance. Tamsin deliberately rejects bearer credentials over a
// cluster-private plaintext HTTP service.
type IngestEndpointResolver interface {
	Resolve(context.Context, string, string) (string, error)
}

//+kubebuilder:rbac:groups=tamoss.livewyer.io,resources=ingestruns,verbs=get;list;watch
//+kubebuilder:rbac:groups=tamoss.livewyer.io,resources=ingestruns/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=tamoss.livewyer.io,resources=tamosses,verbs=get;list;watch
//+kubebuilder:rbac:groups=batch,namespace=system,resources=jobs,verbs=get;list;watch;create;delete
//+kubebuilder:rbac:groups="",namespace=system,resources=pods,verbs=get;list;watch

func (r *IngestRunReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	run := &tamossv1alpha1.IngestRun{}
	if err := r.Client.Get(ctx, req.NamespacedName, run); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	if !r.WatchNamespaces.Allows(run.Namespace) {
		log.FromContext(ctx).Info("ignoring IngestRun outside configured watch scope", "namespace", run.Namespace)
		return ctrl.Result{}, nil
	}

	spec := defaultIngestRunSpec(run.Spec)
	job, err := r.loadIngestJob(ctx, run)
	if err != nil {
		return ctrl.Result{}, err
	}
	if isIngestRunTerminal(run.Status.Phase) {
		return ctrl.Result{}, nil
	}
	if job != nil && !ingestJobBelongsToRun(job, run, spec) {
		return r.setIngestRunPhase(ctx, run, tamossv1alpha1.IngestRunPhasePending, "JobOwnershipConflict", fmt.Sprintf("Job %s/%s is not owned by this IngestRun", job.Namespace, job.Name), false)
	}
	if spec.DesiredState == tamossv1alpha1.IngestRunDesiredStateCancelled {
		return r.reconcileIngestCancellation(ctx, run, job)
	}

	tamoss := &tamossv1alpha1.Tamoss{}
	key := client.ObjectKey{Namespace: run.Namespace, Name: spec.TamossRef.Name}
	if err := r.Client.Get(ctx, key, tamoss); err != nil {
		if apierrors.IsNotFound(err) {
			return r.setIngestRunPhase(ctx, run, tamossv1alpha1.IngestRunPhasePending, operatorstatus.ReasonTamossNotFound, fmt.Sprintf("Tamoss %s/%s does not exist", run.Namespace, spec.TamossRef.Name), false)
		}
		return ctrl.Result{}, err
	}
	if !tamossReadyForIngest(tamoss) {
		return r.setIngestRunPhase(ctx, run, tamossv1alpha1.IngestRunPhasePending, "TamossNotReady", "The target Tamoss instance is not Ready", false)
	}
	if strings.TrimSpace(r.TamsinImage) == "" {
		return r.setIngestRunStaticPhase(ctx, run, tamossv1alpha1.IngestRunPhasePending, "TamsinRuntimeUnavailable", "The operator has no immutable Tamsin image configured", false)
	}
	if !isImmutableImageReference(r.TamsinImage) {
		return r.setIngestRunStaticPhase(ctx, run, tamossv1alpha1.IngestRunPhasePending, "TamsinImageNotImmutable", "TAMOSS_TAMSIN_IMAGE must use an immutable sha256 digest", false)
	}
	if !tamoss.Spec.Secrets.APIToken.Generate {
		return r.setIngestRunPhase(ctx, run, tamossv1alpha1.IngestRunPhasePending, "IngestAuthenticationUnavailable", "IngestRun currently requires an operator-managed API token Secret", false)
	}

	if job == nil {
		if run.Status.JobRef.Name != "" {
			return r.setIngestRunStaticPhase(ctx, run, tamossv1alpha1.IngestRunPhasePending, "IngestJobMissing", "The recorded Tamsin Job no longer exists; the operator will not replay it automatically", false)
		}
		attempt, retryReason, retryMessage, err := r.validateRetryParent(ctx, run, spec)
		if err != nil {
			return ctrl.Result{}, err
		}
		if retryReason != "" {
			return r.setIngestRunPhase(ctx, run, tamossv1alpha1.IngestRunPhasePending, retryReason, retryMessage, false)
		}
		if spec.CredentialProfileRef != nil {
			return r.setIngestRunStaticPhase(ctx, run, tamossv1alpha1.IngestRunPhasePending, "CredentialProfileResolverUnavailable", "Credential profiles are not enabled until the approved server-side resolver is configured", false)
		}
		if r.InputResolver == nil {
			return r.setIngestRunStaticPhase(ctx, run, tamossv1alpha1.IngestRunPhasePending, "InputResolverUnavailable", "The approved server-side input resolver is not configured", false)
		}
		storageID, storageReason, storageMessage, err := r.resolveIngestStorageBackend(ctx, run, spec)
		if err != nil {
			return ctrl.Result{}, err
		}
		if storageReason != "" {
			return r.setIngestRunPhase(ctx, run, tamossv1alpha1.IngestRunPhasePending, storageReason, storageMessage, false)
		}
		resolved, resolveErr := r.InputResolver.Resolve(ctx, run.Namespace, spec.TamossRef.Name, spec.InputRef, spec.Options.MaxInputs)
		if resolveErr != nil {
			log.FromContext(ctx).Error(resolveErr, "unable to resolve IngestRun input reference", "namespace", run.Namespace, "ingestRun", run.Name, "inputKind", spec.InputRef.Kind, "inputID", spec.InputRef.ID)
			return r.setIngestRunPhase(ctx, run, tamossv1alpha1.IngestRunPhasePending, "InputResolutionFailed", "The approved input reference could not be resolved", false)
		}
		if err := validateResolvedIngestInputs(resolved, spec.Options.MaxInputs); err != nil {
			log.FromContext(ctx).Error(err, "input resolver returned an invalid ingest plan", "namespace", run.Namespace, "ingestRun", run.Name)
			return r.setIngestRunPhase(ctx, run, tamossv1alpha1.IngestRunPhasePending, "InvalidResolvedInput", "The approved input resolver returned an invalid ingest plan", false)
		}
		if r.EndpointResolver == nil {
			return r.setIngestRunStaticPhase(ctx, run, tamossv1alpha1.IngestRunPhasePending, "IngestEndpointResolverUnavailable", "The approved TLS ingest endpoint resolver is not configured", false)
		}
		endpoint, resolveEndpointErr := r.EndpointResolver.Resolve(ctx, run.Namespace, spec.TamossRef.Name)
		if resolveEndpointErr != nil {
			log.FromContext(ctx).Error(resolveEndpointErr, "unable to resolve the Tamsin endpoint", "namespace", run.Namespace, "ingestRun", run.Name)
			return r.setIngestRunPhase(ctx, run, tamossv1alpha1.IngestRunPhasePending, "IngestEndpointResolutionFailed", "The approved Tamsin endpoint could not be resolved", false)
		}
		endpoint, endpointErr := validateIngestEndpoint(endpoint)
		if endpointErr != nil {
			log.FromContext(ctx).Error(endpointErr, "ingest endpoint resolver returned an invalid endpoint", "namespace", run.Namespace, "ingestRun", run.Name)
			return r.setIngestRunStaticPhase(ctx, run, tamossv1alpha1.IngestRunPhasePending, "IngestEndpointInvalid", "The approved Tamsin endpoint must be an HTTPS URL without embedded credentials", false)
		}

		desired := desiredIngestJob(run, spec, tamoss, endpoint, r.TamsinImage, storageID, resolved.Selectors)
		if err := controllerutil.SetControllerReference(run, desired, r.Scheme); err != nil {
			return ctrl.Result{}, err
		}
		if err := r.Client.Create(ctx, desired); err != nil {
			if !apierrors.IsAlreadyExists(err) {
				return ctrl.Result{}, err
			}
			return ctrl.Result{Requeue: true}, nil
		}
		return r.setIngestRunJobPhase(ctx, run, desired, tamossv1alpha1.IngestRunPhaseQueued, "JobCreated", "The Tamsin Job was created", true, resolved.ExpectedInputs, attempt)
	}
	phase, reason, message, progressing, phaseErr := ingestPhaseFromJob(ctx, r.Client, job, run.Status.ResultRef)
	if phaseErr != nil {
		return ctrl.Result{}, phaseErr
	}
	return r.setIngestRunJobPhase(ctx, run, job, phase, reason, message, progressing, 0, 0)
}

func (r *IngestRunReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&tamossv1alpha1.IngestRun{}).
		Owns(&batchv1.Job{}).
		Complete(r)
}

func defaultIngestRunSpec(spec tamossv1alpha1.IngestRunSpec) tamossv1alpha1.IngestRunSpec {
	if spec.Profile == "" {
		spec.Profile = tamossv1alpha1.IngestRunProfileEditorial
	}
	if spec.SizeClass == "" {
		spec.SizeClass = tamossv1alpha1.IngestRunSizeClassStandard
	}
	if spec.DesiredState == "" {
		spec.DesiredState = tamossv1alpha1.IngestRunDesiredStateRunning
	}
	if spec.Options.Verify == nil {
		spec.Options.Verify = ptr.To(true)
	}
	if spec.Options.MaxInputs == 0 {
		spec.Options.MaxInputs = 1000
	}
	return spec
}

func tamossReadyForIngest(tamoss *tamossv1alpha1.Tamoss) bool {
	condition := apimeta.FindStatusCondition(tamoss.Status.Conditions, operatorstatus.ConditionReady)
	return condition != nil && condition.Status == metav1.ConditionTrue &&
		tamoss.Status.ObservedGeneration == tamoss.Generation && tamoss.Spec.API.IsEnabled()
}

func (r *IngestRunReconciler) validateRetryParent(ctx context.Context, run *tamossv1alpha1.IngestRun, spec tamossv1alpha1.IngestRunSpec) (int32, string, string, error) {
	if spec.RetryOf == nil {
		return 1, "", "", nil
	}
	if spec.RetryOf.Name == run.Name {
		return 0, "RetryParentInvalid", "An IngestRun cannot retry itself", nil
	}
	parent := &tamossv1alpha1.IngestRun{}
	key := client.ObjectKey{Namespace: run.Namespace, Name: spec.RetryOf.Name}
	if err := r.Client.Get(ctx, key, parent); err != nil {
		if apierrors.IsNotFound(err) {
			return 0, "RetryParentNotFound", fmt.Sprintf("Retry parent %s/%s does not exist", run.Namespace, spec.RetryOf.Name), nil
		}
		return 0, "", "", err
	}
	if string(parent.UID) != spec.RetryOf.UID {
		return 0, "RetryParentReplaced", "The retry parent UID does not match the referenced IngestRun", nil
	}
	if parent.Spec.TamossRef.Name != spec.TamossRef.Name {
		return 0, "RetryParentTargetMismatch", "The retry parent belongs to a different Tamoss instance", nil
	}
	if !ingestRetryConfigurationMatches(defaultIngestRunSpec(parent.Spec), spec) {
		return 0, "RetryParentConfigurationMismatch", "A retry must preserve the parent's input, profile, size class, options, and credential profile", nil
	}
	if !isIngestRunTerminal(parent.Status.Phase) {
		return 0, "RetryParentNotComplete", "The retry parent has not reached a terminal phase", nil
	}
	attempt := parent.Status.Attempt + 1
	if attempt < 2 {
		attempt = 2
	}
	return attempt, "", "", nil
}

func ingestRetryConfigurationMatches(parent, retry tamossv1alpha1.IngestRunSpec) bool {
	parent.DesiredState = ""
	parent.RetryOf = nil
	retry.DesiredState = ""
	retry.RetryOf = nil
	return reflect.DeepEqual(parent, retry)
}

func (r *IngestRunReconciler) resolveIngestStorageBackend(ctx context.Context, run *tamossv1alpha1.IngestRun, spec tamossv1alpha1.IngestRunSpec) (string, string, string, error) {
	name := spec.Options.StorageBackendRef.Name
	if name == "" {
		return "", "", "", nil
	}

	storageBackend := &tamossv1alpha1.StorageBackend{}
	key := client.ObjectKey{Namespace: run.Namespace, Name: name}
	if err := r.Client.Get(ctx, key, storageBackend); err != nil {
		if apierrors.IsNotFound(err) {
			return "", "IngestStorageBackendNotFound", fmt.Sprintf("StorageBackend %s/%s does not exist", run.Namespace, name), nil
		}
		return "", "", "", err
	}
	if storageBackend.Spec.TamossRef.Name != spec.TamossRef.Name {
		return "", "IngestStorageBackendTargetMismatch", "The selected StorageBackend belongs to a different Tamoss instance", nil
	}
	if !storageBackend.DeletionTimestamp.IsZero() {
		return "", "IngestStorageBackendNotReady", "The selected StorageBackend is being deleted", nil
	}

	backendSpec := storageBackend.Spec
	backendSpec.ApplyDefaults(storageBackend.Namespace, storageBackend.Name)
	if backendSpec.Usage != tamossv1alpha1.StorageBackendUsageMedia {
		return "", "IngestStorageBackendUsageInvalid", "The selected StorageBackend is not a media destination", nil
	}
	ready := apimeta.FindStatusCondition(storageBackend.Status.Conditions, operatorstatus.ConditionReady)
	if storageBackend.Status.ObservedGeneration != storageBackend.Generation ||
		ready == nil || ready.Status != metav1.ConditionTrue || ready.ObservedGeneration != storageBackend.Generation {
		return "", "IngestStorageBackendNotReady", "The selected StorageBackend has not reached Ready for its current generation", nil
	}
	if storageBackend.Status.Resolved.BackendID != backendSpec.ID || storageBackend.Status.BackendID != backendSpec.ID {
		return "", "IngestStorageBackendIdentityPending", "The selected StorageBackend has not published its approved TAMS backend identity", nil
	}
	return backendSpec.ID, "", "", nil
}

func validateResolvedIngestInputs(resolved ResolvedIngestInputs, maxInputs int32) error {
	if len(resolved.Selectors) == 0 {
		return fmt.Errorf("no input selectors")
	}
	if len(resolved.Selectors) > maxIngestSelectors {
		return fmt.Errorf("resolved %d top-level selectors, limit is %d", len(resolved.Selectors), maxIngestSelectors)
	}
	if resolved.ExpectedInputs < 0 || resolved.ExpectedInputs > maxInputs {
		return fmt.Errorf("expected input count %d exceeds limit %d", resolved.ExpectedInputs, maxInputs)
	}
	totalBytes := 0
	for _, location := range resolved.Selectors {
		if len(location) > 2048 {
			return fmt.Errorf("input selector exceeds 2048 bytes")
		}
		totalBytes += len(location)
		if totalBytes > maxIngestSelectorBytes {
			return fmt.Errorf("input selectors exceed %d bytes", maxIngestSelectorBytes)
		}
		parsed, err := url.Parse(location)
		if err != nil || !parsed.IsAbs() {
			return fmt.Errorf("input selector is not an absolute URL")
		}
		if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
			return fmt.Errorf("input selector contains credentials, query parameters, or a fragment")
		}
		switch strings.ToLower(parsed.Scheme) {
		case "s3":
			if parsed.Host == "" || strings.Trim(parsed.Path, "/") == "" {
				return fmt.Errorf("S3 input location requires a bucket and object key")
			}
		case "http", "https":
			if parsed.Host == "" {
				return fmt.Errorf("HTTP input location requires a host")
			}
		default:
			return fmt.Errorf("unsupported input location scheme %q", parsed.Scheme)
		}
	}
	return nil
}

func validateIngestEndpoint(endpoint string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(endpoint))
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", fmt.Errorf("endpoint must be an HTTPS URL without userinfo, query, or fragment")
	}
	return parsed.String(), nil
}

func (r *IngestRunReconciler) loadIngestJob(ctx context.Context, run *tamossv1alpha1.IngestRun) (*batchv1.Job, error) {
	job := &batchv1.Job{}
	err := r.Client.Get(ctx, client.ObjectKey{Namespace: run.Namespace, Name: ingestJobName(run.Name)}, job)
	if apierrors.IsNotFound(err) {
		return nil, nil
	}
	return job, err
}

func (r *IngestRunReconciler) reconcileIngestCancellation(ctx context.Context, run *tamossv1alpha1.IngestRun, job *batchv1.Job) (ctrl.Result, error) {
	if job == nil {
		podsRemain, err := r.ingestRunPodsRemain(ctx, run)
		if err != nil {
			return ctrl.Result{}, err
		}
		if podsRemain {
			result, err := r.setIngestRunPhase(ctx, run, tamossv1alpha1.IngestRunPhaseRunning, "CancellationRequested", "The Tamsin Pod is terminating", true)
			if err == nil && result.RequeueAfter == 0 {
				result.RequeueAfter = ingestRunRequeueCancel
			}
			return result, err
		}
		return r.setIngestRunPhase(ctx, run, tamossv1alpha1.IngestRunPhaseCancelled, "Cancelled", "The ingest run was cancelled", false)
	}
	if job.DeletionTimestamp.IsZero() {
		if err := r.Client.Delete(ctx, job, client.PropagationPolicy(metav1.DeletePropagationForeground)); err != nil && !apierrors.IsNotFound(err) {
			return ctrl.Result{}, err
		}
	}
	result, err := r.setIngestRunJobPhase(ctx, run, job, tamossv1alpha1.IngestRunPhaseRunning, "CancellationRequested", "The Tamsin Job is terminating", true, 0, 0)
	if err == nil && result.RequeueAfter == 0 {
		result.RequeueAfter = ingestRunRequeueCancel
	}
	return result, err
}

func (r *IngestRunReconciler) ingestRunPodsRemain(ctx context.Context, run *tamossv1alpha1.IngestRun) (bool, error) {
	pods := &corev1.PodList{}
	if err := r.Client.List(ctx, pods, client.InNamespace(run.Namespace), client.MatchingLabels{ingestRunLabel: ingestRunSelectorValue(run.Name)}); err != nil {
		return false, err
	}
	for i := range pods.Items {
		pod := &pods.Items[i]
		if pod.Labels[ingestRunTargetLabel] != run.Spec.TamossRef.Name {
			continue
		}
		if run.Status.JobRef.UID == "" || podOwnedByJobUID(pod, run.Status.JobRef.UID) {
			return true, nil
		}
	}
	return false, nil
}

func podOwnedByJobUID(pod *corev1.Pod, jobUID types.UID) bool {
	for _, owner := range pod.OwnerReferences {
		if owner.Kind == "Job" && owner.UID == jobUID {
			return true
		}
	}
	return false
}

func ingestJobBelongsToRun(job *batchv1.Job, run *tamossv1alpha1.IngestRun, spec tamossv1alpha1.IngestRunSpec) bool {
	return metav1.IsControlledBy(job, run) &&
		job.Labels[ingestRunLabel] == ingestRunSelectorValue(run.Name) &&
		job.Labels[ingestRunTargetLabel] == spec.TamossRef.Name
}

func (r *IngestRunReconciler) setIngestRunPhase(ctx context.Context, run *tamossv1alpha1.IngestRun, phase tamossv1alpha1.IngestRunPhase, reason, message string, progressing bool) (ctrl.Result, error) {
	return r.setIngestRunJobPhase(ctx, run, nil, phase, reason, message, progressing, 0, 0)
}

// setIngestRunStaticPhase records gates that require configuration or deliberate
// intervention. Informer events or an operator restart will reconcile them; a
// fixed-period poll would only create unbounded API traffic.
func (r *IngestRunReconciler) setIngestRunStaticPhase(ctx context.Context, run *tamossv1alpha1.IngestRun, phase tamossv1alpha1.IngestRunPhase, reason, message string, progressing bool) (ctrl.Result, error) {
	_, err := r.setIngestRunPhase(ctx, run, phase, reason, message, progressing)
	return ctrl.Result{}, err
}

func (r *IngestRunReconciler) setIngestRunJobPhase(ctx context.Context, run *tamossv1alpha1.IngestRun, job *batchv1.Job, phase tamossv1alpha1.IngestRunPhase, reason, message string, progressing bool, inputsTotal, attempt int32) (ctrl.Result, error) {
	original := run.DeepCopy()
	if ingestResultRequiredForPhase(phase) && !isDigestVerifiedIngestResult(run.Status.ResultRef) {
		phase = tamossv1alpha1.IngestRunPhaseRunning
		reason = "ResultVerificationPending"
		message = "Tamsin finished, but its durable result has not passed digest verification"
		progressing = false
	}
	run.Status.ObservedGeneration = run.Generation
	run.Status.Phase = phase
	if inputsTotal > 0 {
		run.Status.Progress.InputsTotal = inputsTotal
	}
	if attempt > 0 {
		run.Status.Attempt = attempt
	}
	if job != nil {
		run.Status.JobRef.Name = job.Name
		run.Status.JobRef.UID = job.UID
		if run.Status.StartedAt == nil {
			now := metav1.Now()
			run.Status.StartedAt = &now
		}
	}
	terminal := isIngestRunTerminal(phase)
	if terminal && run.Status.CompletedAt == nil {
		now := metav1.Now()
		run.Status.CompletedAt = &now
	}
	ready := metav1.ConditionFalse
	if phase == tamossv1alpha1.IngestRunPhaseSucceeded {
		ready = metav1.ConditionTrue
	}
	apimeta.SetStatusCondition(&run.Status.Conditions, metav1.Condition{
		Type:               ingestRunReadyCondition,
		Status:             ready,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: run.Generation,
	})
	progressingStatus := metav1.ConditionFalse
	if progressing {
		progressingStatus = metav1.ConditionTrue
	}
	apimeta.SetStatusCondition(&run.Status.Conditions, metav1.Condition{
		Type:               ingestRunProgressingCondition,
		Status:             progressingStatus,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: run.Generation,
	})
	if reflect.DeepEqual(original.Status, run.Status) {
		if !terminal && !progressing {
			return ctrl.Result{RequeueAfter: ingestRunRequeuePending}, nil
		}
		return ctrl.Result{}, nil
	}
	if err := r.Client.Status().Patch(ctx, run, client.MergeFrom(original)); err != nil {
		return ctrl.Result{}, err
	}
	if !terminal && !progressing {
		return ctrl.Result{RequeueAfter: ingestRunRequeuePending}, nil
	}
	return ctrl.Result{}, nil
}

func isIngestRunTerminal(phase tamossv1alpha1.IngestRunPhase) bool {
	switch phase {
	case tamossv1alpha1.IngestRunPhaseSucceeded,
		tamossv1alpha1.IngestRunPhasePartiallySucceeded,
		tamossv1alpha1.IngestRunPhaseFailed,
		tamossv1alpha1.IngestRunPhaseCancelled:
		return true
	default:
		return false
	}
}

func ingestPhaseFromJob(ctx context.Context, reader client.Reader, job *batchv1.Job, result tamossv1alpha1.IngestRunResultStatus) (tamossv1alpha1.IngestRunPhase, string, string, bool, error) {
	for _, condition := range job.Status.Conditions {
		if condition.Type == batchv1.JobComplete && condition.Status == corev1.ConditionTrue {
			if !isDigestVerifiedIngestResult(result) {
				return tamossv1alpha1.IngestRunPhaseRunning, "ResultVerificationPending", "Tamsin finished, but its durable result has not passed digest verification", false, nil
			}
			return tamossv1alpha1.IngestRunPhaseSucceeded, "IngestSucceeded", "Tamsin completed the ingest run", false, nil
		}
	}
	for _, condition := range job.Status.Conditions {
		if condition.Type == batchv1.JobFailed && condition.Status == corev1.ConditionTrue {
			exitCode, found, err := ingestJobTerminalExitCode(ctx, reader, job)
			if err != nil {
				return "", "", "", false, err
			}
			if !found {
				return tamossv1alpha1.IngestRunPhaseRunning, "ExitCodePending", "The Job failed, but its owned terminal Tamsin Pod has not been observed", false, nil
			}
			if exitCode == 4 {
				if !isDigestVerifiedIngestResult(result) {
					return tamossv1alpha1.IngestRunPhaseRunning, "ResultVerificationPending", "Tamsin finished with input failures, but its durable result has not passed digest verification", false, nil
				}
				return tamossv1alpha1.IngestRunPhasePartiallySucceeded, "IngestPartiallySucceeded", "Tamsin completed with one or more failed inputs", false, nil
			}
			message := strings.TrimSpace(condition.Message)
			if message == "" {
				message = "The Tamsin Job failed"
			}
			return tamossv1alpha1.IngestRunPhaseFailed, "IngestFailed", message, false, nil
		}
	}
	if job.Status.Active > 0 {
		return tamossv1alpha1.IngestRunPhaseRunning, "IngestRunning", "Tamsin is processing media", true, nil
	}
	if job.Status.StartTime != nil {
		return tamossv1alpha1.IngestRunPhaseRunning, "JobStarting", "The Tamsin Pod is starting", true, nil
	}
	return tamossv1alpha1.IngestRunPhaseQueued, "JobQueued", "The Tamsin Job is waiting to be scheduled", true, nil
}

func ingestResultRequiredForPhase(phase tamossv1alpha1.IngestRunPhase) bool {
	return phase == tamossv1alpha1.IngestRunPhaseSucceeded || phase == tamossv1alpha1.IngestRunPhasePartiallySucceeded
}

func isDigestVerifiedIngestResult(result tamossv1alpha1.IngestRunResultStatus) bool {
	if !result.Verified || strings.TrimSpace(result.Key) == "" || result.Size <= 0 || strings.TrimSpace(result.MediaType) == "" || len(result.SHA256) != 64 {
		return false
	}
	for _, char := range result.SHA256 {
		if (char < '0' || char > '9') && (char < 'a' || char > 'f') {
			return false
		}
	}
	return true
}

func ingestJobTerminalExitCode(ctx context.Context, reader client.Reader, job *batchv1.Job) (int32, bool, error) {
	pods := &corev1.PodList{}
	if err := reader.List(ctx, pods, client.InNamespace(job.Namespace), client.MatchingLabels{"job-name": job.Name}); err != nil {
		return 0, false, fmt.Errorf("list terminal Pods for Job %s/%s: %w", job.Namespace, job.Name, err)
	}
	var selected *corev1.ContainerStateTerminated
	for i := range pods.Items {
		if job.UID == "" || !podOwnedByJobUID(&pods.Items[i], job.UID) {
			continue
		}
		for _, status := range pods.Items[i].Status.ContainerStatuses {
			terminated := status.State.Terminated
			if status.Name != "tamsin" || terminated == nil {
				continue
			}
			if selected == nil || terminated.FinishedAt.After(selected.FinishedAt.Time) {
				selected = terminated
			}
		}
	}
	if selected == nil {
		return 0, false, nil
	}
	return selected.ExitCode, true, nil
}

type ingestJobResources struct {
	Concurrency       int32
	StagingByteBudget string
	TemporarySize     resource.Quantity
	Requests          corev1.ResourceList
	Limits            corev1.ResourceList
}

func resourcesForIngestRun(size tamossv1alpha1.IngestRunSizeClass) ingestJobResources {
	switch size {
	case tamossv1alpha1.IngestRunSizeClassSmall:
		return ingestResources(1, "4GiB", "6Gi", "250m", "1Gi", "8Gi", "2", "2Gi", "10Gi")
	case tamossv1alpha1.IngestRunSizeClassLarge:
		return ingestResources(8, "80GiB", "100Gi", "2", "4Gi", "110Gi", "8", "12Gi", "120Gi")
	default:
		return ingestResources(4, "16GiB", "20Gi", "1", "2Gi", "24Gi", "4", "6Gi", "30Gi")
	}
}

func ingestResources(concurrency int32, budget, temporarySize, requestCPU, requestMemory, requestStorage, limitCPU, limitMemory, limitStorage string) ingestJobResources {
	return ingestJobResources{
		Concurrency:       concurrency,
		StagingByteBudget: budget,
		TemporarySize:     resource.MustParse(temporarySize),
		Requests: corev1.ResourceList{
			corev1.ResourceCPU:              resource.MustParse(requestCPU),
			corev1.ResourceMemory:           resource.MustParse(requestMemory),
			corev1.ResourceEphemeralStorage: resource.MustParse(requestStorage),
		},
		Limits: corev1.ResourceList{
			corev1.ResourceCPU:              resource.MustParse(limitCPU),
			corev1.ResourceMemory:           resource.MustParse(limitMemory),
			corev1.ResourceEphemeralStorage: resource.MustParse(limitStorage),
		},
	}
}

func desiredIngestJob(run *tamossv1alpha1.IngestRun, spec tamossv1alpha1.IngestRunSpec, tamoss *tamossv1alpha1.Tamoss, endpoint, image, storageID string, inputSelectors []string) *batchv1.Job {
	jobResources := resourcesForIngestRun(spec.SizeClass)
	concurrency := spec.Options.Concurrency
	if concurrency == 0 {
		concurrency = jobResources.Concurrency
	}
	args := []string{
		"ingest",
		"--endpoint", endpoint,
		"--format", "json",
		"--log-format", "json",
		"--progress", "none",
		"--profile", strings.TrimSuffix(string(spec.Profile), "@1"),
		"--max-inputs", fmt.Sprintf("%d", spec.Options.MaxInputs),
		"--concurrency", fmt.Sprintf("%d", concurrency),
		"--staging-byte-budget", jobResources.StagingByteBudget,
		fmt.Sprintf("--verify=%t", ptr.Deref(spec.Options.Verify, true)),
	}
	if spec.Options.DryRun {
		args = append(args, "--dry-run")
	}
	if storageID != "" {
		args = append(args, "--storage-id", storageID)
	}
	for _, selector := range inputSelectors {
		args = append(args, "--input", selector)
	}

	labels := map[string]string{
		"app.kubernetes.io/name":       "tamsin",
		"app.kubernetes.io/component":  "ingest",
		"app.kubernetes.io/managed-by": "tamoss-operator",
		"app.kubernetes.io/instance":   spec.TamossRef.Name,
		ingestRunLabel:                 ingestRunSelectorValue(run.Name),
		ingestRunTargetLabel:           spec.TamossRef.Name,
	}
	return &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ingestJobName(run.Name),
			Namespace: run.Namespace,
			Labels:    labels,
		},
		Spec: batchv1.JobSpec{
			BackoffLimit:            ptr.To[int32](2),
			ActiveDeadlineSeconds:   ptr.To[int64](21600),
			TTLSecondsAfterFinished: ptr.To[int32](3600),
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: labels},
				Spec: corev1.PodSpec{
					AutomountServiceAccountToken:  ptr.To(false),
					RestartPolicy:                 corev1.RestartPolicyNever,
					TerminationGracePeriodSeconds: ptr.To[int64](90),
					ImagePullSecrets:              tamoss.Spec.ImagePullSecrets,
					SecurityContext: &corev1.PodSecurityContext{
						RunAsNonRoot: ptr.To(true),
						RunAsUser:    ptr.To[int64](65532),
						RunAsGroup:   ptr.To[int64](65532),
						SeccompProfile: &corev1.SeccompProfile{
							Type: corev1.SeccompProfileTypeRuntimeDefault,
						},
					},
					Containers: []corev1.Container{{
						Name:            "tamsin",
						Image:           image,
						ImagePullPolicy: corev1.PullIfNotPresent,
						Args:            args,
						Env: []corev1.EnvVar{
							{Name: "TAMSIN_AUTH_MODE", Value: "bearer"},
							{
								Name: "TAMSIN_AUTH_TOKEN",
								ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{
									LocalObjectReference: corev1.LocalObjectReference{Name: tamoss.ResourceName("api-token")},
									Key:                  "TAMOSS_API_TOKEN",
								}},
							},
						},
						Resources: corev1.ResourceRequirements{Requests: jobResources.Requests, Limits: jobResources.Limits},
						SecurityContext: &corev1.SecurityContext{
							AllowPrivilegeEscalation: ptr.To(false),
							ReadOnlyRootFilesystem:   ptr.To(true),
							Capabilities:             &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}},
						},
						VolumeMounts: []corev1.VolumeMount{{Name: "temporary-media", MountPath: "/tmp"}},
					}},
					Volumes: []corev1.Volume{{
						Name:         "temporary-media",
						VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{SizeLimit: &jobResources.TemporarySize}},
					}},
				},
			},
		},
	}
}

func ingestJobName(runName string) string {
	const prefix = "tamsin-"
	if len(prefix)+len(runName) <= 63 {
		return prefix + runName
	}
	sum := sha256.Sum256([]byte(runName))
	suffix := hex.EncodeToString(sum[:])[:10]
	maxRunLength := 63 - len(prefix) - len(suffix) - 1
	return prefix + strings.TrimRight(runName[:maxRunLength], "-.") + "-" + suffix
}

func ingestRunSelectorValue(runName string) string {
	if len(runName) <= 63 {
		return runName
	}
	sum := sha256.Sum256([]byte(runName))
	suffix := hex.EncodeToString(sum[:])[:10]
	prefix := strings.TrimRight(runName[:52], "-.")
	return prefix + "-" + suffix
}

func isImmutableImageReference(image string) bool {
	repository, digest, found := strings.Cut(strings.TrimSpace(image), "@sha256:")
	if !found || repository == "" || len(digest) != 64 {
		return false
	}
	for _, char := range digest {
		if (char < '0' || char > '9') && (char < 'a' || char > 'f') {
			return false
		}
	}
	return true
}
