package controller

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"reflect"
	"sort"
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
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	tamossv1alpha1 "github.com/livewyer-ops/tamoss/operator/api/v1alpha1"
	operatorstatus "github.com/livewyer-ops/tamoss/operator/internal/status"
	"github.com/livewyer-ops/tamsin/ingestevent"
)

const (
	ingestRunReadyCondition       = operatorstatus.ConditionReady
	ingestRunProgressingCondition = operatorstatus.ConditionProgressing

	ingestRunLabel               = "tamoss.livewyer.io/ingest-run"
	ingestRunTargetLabel         = "tamoss.livewyer.io/tamoss"
	ingestSourceAnnotation       = "tamoss.livewyer.io/ingest-source"
	ingestSourcePolicyAnnotation = "tamoss.livewyer.io/ingest-source-policy-digest"
	ingestFlowMetadataAnnotation = "tamoss.livewyer.io/flow-metadata"
	ingestFlowMetadataVolume     = "flow-metadata"
	ingestFlowMetadataMountPath  = "/etc/tamsin/flow-metadata"
	ingestFlowMetadataFile       = ingestFlowMetadataMountPath + "/flow-metadata.json"
	ingestFlowFormatVideo        = "video"

	ingestRunRequeuePending   = 15 * time.Second
	ingestRunRequeueCancel    = 2 * time.Second
	maxIngestSelectors        = 32
	maxIngestSelectorBytes    = 32 * 1024
	ingestFlowProfileRefIndex = ".spec.options.tamsFlowProfiles.profileRef.name"

	// ingestTerminalObservationDeadline bounds how long a finished Job may stay
	// non-terminal while the operator waits for evidence it cannot yet see: a
	// digest-verified durable result, or the exit code of a Pod that may already
	// have been garbage collected. Without a bound the run would poll until the
	// Job's TTL removed it, and then never reach a terminal phase at all.
	ingestTerminalObservationDeadline = 15 * time.Minute
)

type IngestRunReconciler struct {
	Client          client.Client
	Scheme          *runtime.Scheme
	WatchNamespaces WatchNamespaceSet
	TamsinImage     string
	// APIReader performs uncached reads. Absence is only trusted after a live
	// confirmation, because a lagging informer cache would otherwise fail a
	// healthy run whose Job or target instance does exist.
	APIReader        client.Reader
	InputResolver    IngestInputResolver
	EndpointResolver IngestEndpointResolver
	// PodLogs reads the finished TAMSin Pod's event stream. A terminal run must
	// have a valid complete stream whose exit code matches the container.
	PodLogs IngestPodLogReader

	// now is overridden by tests that exercise observation deadlines.
	now func() time.Time
}

func (r *IngestRunReconciler) currentTime() time.Time {
	if r.now != nil {
		return r.now()
	}
	return time.Now()
}

// ResolvedIngestInputs is the private handoff from an approved input service to
// the fixed TAMSin Job template. Selectors are bounded top-level TAMSin inputs,
// such as one staged object, manifest, or S3 prefix. The resolver must not
// expand a large batch into Job arguments.
type ResolvedIngestInputs struct {
	Selectors            []string
	ExpectedInputs       int32
	SourceName           string
	PolicyDigest         string
	CredentialSecretName string
	CredentialKind       tamossv1alpha1.IngestSourceKind
	S3Endpoint           string
	S3Region             string
	S3PathStyle          bool
}

// IngestInputResolver validates an immutable run selector against the target
// instance's source policy and records a digest of that validation decision.
type IngestInputResolver interface {
	Resolve(context.Context, *tamossv1alpha1.Tamoss, tamossv1alpha1.IngestRunInput, int32) (ResolvedIngestInputs, error)
}

// IngestEndpointResolver selects an operator-approved TLS endpoint for a
// Tamoss instance. TAMSin deliberately rejects bearer credentials over a
// cluster-private plaintext HTTP service.
type IngestEndpointResolver interface {
	Resolve(context.Context, string, string) (string, error)
}

// Patch is required so the operator can delegate one-way cancellation to the
// per-instance Console Role without violating RBAC privilege escalation checks.
//+kubebuilder:rbac:groups=tamoss.livewyer.io,resources=ingestruns,verbs=get;list;watch;patch
//+kubebuilder:rbac:groups=tamoss.livewyer.io,resources=ingestruns/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=tamoss.livewyer.io,resources=tamosses,verbs=get;list;watch
//+kubebuilder:rbac:groups=tamoss.livewyer.io,resources=flowprofiles,verbs=get;list;watch
// Source-bound credentials are read only to validate the required keys and
// shape. The Job receives SecretKeyRefs; values are never copied or logged.
//+kubebuilder:rbac:groups="",namespace=system,resources=secrets,verbs=get
//+kubebuilder:rbac:groups=batch,namespace=system,resources=jobs,verbs=get;list;watch;create;delete
//+kubebuilder:rbac:groups="",namespace=system,resources=pods,verbs=get;list;watch
// The operator reads a finished TAMSin Pod's stdout to record run outcome
// counters. The Console service account deliberately never receives this.
//+kubebuilder:rbac:groups="",namespace=system,resources=pods/log,verbs=get

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
	if spec.Output != nil && spec.Options.MaxInputs != 1 {
		return r.setIngestRunStaticPhase(ctx, run, tamossv1alpha1.IngestRunPhasePending, "InvalidOutputIntent", "Root Flow output metadata requires maxInputs to equal 1", false)
	}
	if err := tamossv1alpha1.ValidateIngestRunOutput(spec.Output); err != nil {
		log.FromContext(ctx).Error(err, "IngestRun output intent is invalid", "namespace", run.Namespace, "ingestRun", run.Name)
		return r.setIngestRunStaticPhase(ctx, run, tamossv1alpha1.IngestRunPhasePending, "InvalidOutputIntent", "The Flow output metadata is invalid", false)
	}
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
			if job != nil {
				return r.abandonIngestRunForDeletedTamoss(ctx, run, job, key)
			}
			return r.setIngestRunPhase(ctx, run, tamossv1alpha1.IngestRunPhasePending, operatorstatus.ReasonTamossNotFound, fmt.Sprintf("Tamoss %s/%s does not exist", run.Namespace, spec.TamossRef.Name), false)
		}
		return ctrl.Result{}, err
	}

	// An attempt that already has a Job is tracked from the Job alone. The gates
	// below admit new work; re-applying them here would regress a running run to
	// Pending whenever the target instance was momentarily not Ready, and would
	// ignore the Job's own completion while that gate stayed shut.
	if job != nil {
		var stream *ingestStreamSummary
		if ingestJobFinished(job) {
			var streamErr error
			stream, streamErr = r.collectIngestStream(ctx, job)
			if streamErr != nil {
				phase, reason, message := ingestStreamFailurePhase(job, r.currentTime(), streamErr)
				return r.setIngestRunJobPhase(ctx, run, job, phase, reason, message, false, 0, 0)
			}
		}
		phase, reason, message, progressing, phaseErr := ingestPhaseFromJob(ctx, r.Client, job, run.Status.ResultRef, r.currentTime(), stream)
		if phaseErr != nil {
			return ctrl.Result{}, phaseErr
		}
		return r.setIngestRunJobPhaseWithStream(ctx, run, job, phase, reason, message, progressing, 0, 0, stream)
	}
	if run.Status.JobRef.Name != "" {
		return r.reconcileMissingIngestJob(ctx, run)
	}

	if !tamossReadyForIngest(tamoss) {
		return r.setIngestRunPhase(ctx, run, tamossv1alpha1.IngestRunPhasePending, "TamossNotReady", "The target Tamoss instance is not Ready", false)
	}
	if strings.TrimSpace(r.TamsinImage) == "" {
		return r.setIngestRunStaticPhase(ctx, run, tamossv1alpha1.IngestRunPhasePending, "TamsinRuntimeUnavailable", "The operator has no immutable TAMSin image configured", false)
	}
	if !isImmutableImageReference(r.TamsinImage) {
		return r.setIngestRunStaticPhase(ctx, run, tamossv1alpha1.IngestRunPhasePending, "TamsinImageNotImmutable", "TAMOSS_TAMSIN_IMAGE must use an immutable sha256 digest", false)
	}
	if !tamoss.Spec.Secrets.APIToken.Generate {
		return r.setIngestRunPhase(ctx, run, tamossv1alpha1.IngestRunPhasePending, "IngestAuthenticationUnavailable", "IngestRun currently requires an operator-managed API token Secret", false)
	}

	attempt, retryReason, retryMessage, err := r.validateRetryParent(ctx, run, spec)
	if err != nil {
		return ctrl.Result{}, err
	}
	if retryReason != "" {
		return r.setIngestRunPhase(ctx, run, tamossv1alpha1.IngestRunPhasePending, retryReason, retryMessage, false)
	}
	if r.InputResolver == nil {
		return r.setIngestRunStaticPhase(ctx, run, tamossv1alpha1.IngestRunPhasePending, "InputResolverUnavailable", "The ingest source-policy resolver is not configured", false)
	}
	storageID, storageReason, storageMessage, err := r.resolveIngestStorageBackend(ctx, run, spec)
	if err != nil {
		return ctrl.Result{}, err
	}
	if storageReason != "" {
		return r.setIngestRunPhase(ctx, run, tamossv1alpha1.IngestRunPhasePending, storageReason, storageMessage, false)
	}
	resolvedProfiles, control, stop, err := r.resolveIngestFlowProfilesStage(ctx, run, spec)
	if err != nil || stop {
		return control, err
	}
	for i := range spec.Options.TAMSFlowProfiles {
		spec.Options.TAMSFlowProfiles[i].ProfileID = resolvedProfiles[i].ProfileID
		spec.Options.TAMSFlowProfiles[i].ProfileRef = nil
	}
	resolved, resolveErr := r.InputResolver.Resolve(ctx, tamoss, spec.Input, spec.Options.MaxInputs)
	if resolveErr != nil {
		log.FromContext(ctx).Error(resolveErr, "IngestRun input was rejected by source policy", "namespace", run.Namespace, "ingestRun", run.Name, "inputKind", spec.Input.Kind)
		return r.setIngestRunPhase(ctx, run, tamossv1alpha1.IngestRunPhasePending, "InputPolicyRejected", "The input is not permitted by the target Tamoss source policy", false)
	}
	if err := validateResolvedIngestInputs(resolved, spec.Options.MaxInputs); err != nil {
		log.FromContext(ctx).Error(err, "input resolver returned an invalid ingest plan", "namespace", run.Namespace, "ingestRun", run.Name)
		return r.setIngestRunPhase(ctx, run, tamossv1alpha1.IngestRunPhasePending, "InvalidResolvedInput", "The source-policy resolver returned an invalid ingest plan", false)
	}
	if r.EndpointResolver == nil {
		return r.setIngestRunStaticPhase(ctx, run, tamossv1alpha1.IngestRunPhasePending, "IngestEndpointResolverUnavailable", "The approved TLS ingest endpoint resolver is not configured", false)
	}
	endpoint, resolveEndpointErr := r.EndpointResolver.Resolve(ctx, run.Namespace, spec.TamossRef.Name)
	if resolveEndpointErr != nil {
		log.FromContext(ctx).Error(resolveEndpointErr, "unable to resolve the TAMSin endpoint", "namespace", run.Namespace, "ingestRun", run.Name)
		return r.setIngestRunPhase(ctx, run, tamossv1alpha1.IngestRunPhasePending, "IngestEndpointResolutionFailed", "The approved TAMSin endpoint could not be resolved", false)
	}
	endpoint, endpointErr := validateIngestEndpoint(endpoint)
	if endpointErr != nil {
		log.FromContext(ctx).Error(endpointErr, "ingest endpoint resolver returned an invalid endpoint", "namespace", run.Namespace, "ingestRun", run.Name)
		return r.setIngestRunStaticPhase(ctx, run, tamossv1alpha1.IngestRunPhasePending, "IngestEndpointInvalid", "The approved TAMSin endpoint must be an HTTPS URL without embedded credentials", false)
	}

	flowMetadata, err := tamossv1alpha1.IngestRunFlowMetadataJSON(spec.Output)
	if err != nil {
		return ctrl.Result{}, err
	}
	desired := desiredIngestJob(run, spec, tamoss, endpoint, r.TamsinImage, storageID, flowMetadata, resolved)
	if err := controllerutil.SetControllerReference(run, desired, r.Scheme); err != nil {
		return ctrl.Result{}, err
	}
	if err := r.Client.Create(ctx, desired); err != nil {
		if !apierrors.IsAlreadyExists(err) {
			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true}, nil
	}
	return r.setIngestRunJobPhase(ctx, run, desired, tamossv1alpha1.IngestRunPhaseQueued, "JobCreated", "The TAMSin Job was created", true, resolved.ExpectedInputs, attempt)
}

func (r *IngestRunReconciler) resolveIngestFlowProfilesStage(ctx context.Context, run *tamossv1alpha1.IngestRun, spec tamossv1alpha1.IngestRunSpec) ([]tamossv1alpha1.IngestRunResolvedFlowProfileStatus, ctrl.Result, bool, error) {
	resolved, reason, message, err := r.resolveIngestFlowProfiles(ctx, run, spec)
	if err != nil {
		return nil, ctrl.Result{}, true, err
	}
	if reason != "" {
		changed, err := r.persistResolvedIngestFlowProfiles(ctx, run, nil)
		if err != nil {
			return nil, ctrl.Result{}, true, err
		}
		if changed {
			return nil, ctrl.Result{Requeue: true}, true, nil
		}
		result, err := r.setIngestRunPhase(ctx, run, tamossv1alpha1.IngestRunPhasePending, reason, message, false)
		return nil, result, true, err
	}
	changed, err := r.persistResolvedIngestFlowProfiles(ctx, run, resolved)
	if err != nil {
		return nil, ctrl.Result{}, true, err
	}
	if changed {
		return nil, ctrl.Result{Requeue: true}, true, nil
	}
	return resolved, ctrl.Result{}, false, nil
}

func (r *IngestRunReconciler) persistResolvedIngestFlowProfiles(ctx context.Context, run *tamossv1alpha1.IngestRun, resolved []tamossv1alpha1.IngestRunResolvedFlowProfileStatus) (bool, error) {
	if len(resolved) == 0 {
		resolved = nil
	}
	if reflect.DeepEqual(run.Status.ResolvedTAMSFlowProfiles, resolved) {
		return false, nil
	}
	original := run.DeepCopy()
	run.Status.ResolvedTAMSFlowProfiles = append([]tamossv1alpha1.IngestRunResolvedFlowProfileStatus(nil), resolved...)
	if err := r.Client.Status().Patch(ctx, run, client.MergeFrom(original)); err != nil {
		return false, err
	}
	return true, nil
}

// reconcileMissingIngestJob records a terminal outcome for a run whose recorded
// Job has gone. TAMSin ingest is not idempotent, so the operator never replays
// it; leaving the run non-terminal instead wedged it Pending forever, blocked
// retries, and kept it in the console's active set.
func (r *IngestRunReconciler) reconcileMissingIngestJob(ctx context.Context, run *tamossv1alpha1.IngestRun) (ctrl.Result, error) {
	present, err := r.ingestJobPresentLive(ctx, run)
	if err != nil {
		return ctrl.Result{}, err
	}
	if present {
		return ctrl.Result{RequeueAfter: ingestRunRequeueCancel}, nil
	}
	return r.setIngestRunPhase(
		ctx,
		run,
		tamossv1alpha1.IngestRunPhaseFailed,
		"IngestJobMissing",
		"The recorded TAMSin Job no longer exists, so the run outcome cannot be confirmed; the operator does not replay ingest automatically",
		false,
	)
}

// abandonIngestRunForDeletedTamoss stops an attempt whose target instance has
// gone. The Job is owned by the IngestRun rather than the Tamoss, so nothing
// else would stop it uploading to a deleted endpoint until its active deadline.
func (r *IngestRunReconciler) abandonIngestRunForDeletedTamoss(ctx context.Context, run *tamossv1alpha1.IngestRun, job *batchv1.Job, tamossKey client.ObjectKey) (ctrl.Result, error) {
	if r.APIReader != nil {
		live := &tamossv1alpha1.Tamoss{}
		err := r.APIReader.Get(ctx, tamossKey, live)
		if err == nil {
			return ctrl.Result{RequeueAfter: ingestRunRequeueCancel}, nil
		}
		if !apierrors.IsNotFound(err) {
			return ctrl.Result{}, err
		}
	}
	if job.DeletionTimestamp.IsZero() {
		if err := r.Client.Delete(ctx, job, client.PropagationPolicy(metav1.DeletePropagationForeground)); err != nil && !apierrors.IsNotFound(err) {
			return ctrl.Result{}, err
		}
	}
	return r.setIngestRunJobPhase(
		ctx,
		run,
		job,
		tamossv1alpha1.IngestRunPhaseFailed,
		operatorstatus.ReasonTamossNotFound,
		fmt.Sprintf("Tamoss %s/%s was deleted while the TAMSin Job was running", tamossKey.Namespace, tamossKey.Name),
		false,
		0,
		0,
	)
}

// ingestJobPresentLive confirms a Job's absence against the API server. A cold
// or lagging informer cache must not be enough to fail a healthy run.
func (r *IngestRunReconciler) ingestJobPresentLive(ctx context.Context, run *tamossv1alpha1.IngestRun) (bool, error) {
	if r.APIReader == nil {
		return false, nil
	}
	key := client.ObjectKey{Namespace: run.Namespace, Name: ingestJobName(run.Name)}
	err := r.APIReader.Get(ctx, key, &batchv1.Job{})
	if err == nil {
		return true, nil
	}
	if apierrors.IsNotFound(err) {
		return false, nil
	}
	return false, err
}

func (r *IngestRunReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if err := mgr.GetFieldIndexer().IndexField(context.Background(), &tamossv1alpha1.IngestRun{}, ingestFlowProfileRefIndex, func(obj client.Object) []string {
		run, ok := obj.(*tamossv1alpha1.IngestRun)
		if !ok {
			return nil
		}
		values := make([]string, 0, len(run.Spec.Options.TAMSFlowProfiles))
		for _, assignment := range run.Spec.Options.TAMSFlowProfiles {
			if assignment.ProfileRef != nil && strings.TrimSpace(assignment.ProfileRef.Name) != "" {
				values = append(values, assignment.ProfileRef.Name)
			}
		}
		return values
	}); err != nil {
		return err
	}
	return ctrl.NewControllerManagedBy(mgr).
		For(&tamossv1alpha1.IngestRun{}).
		Owns(&batchv1.Job{}).
		Watches(&tamossv1alpha1.FlowProfile{}, handler.EnqueueRequestsFromMapFunc(r.ingestRunsForFlowProfile)).
		Complete(r)
}

func (r *IngestRunReconciler) ingestRunsForFlowProfile(ctx context.Context, obj client.Object) []reconcile.Request {
	list := &tamossv1alpha1.IngestRunList{}
	if err := r.Client.List(ctx, list, client.InNamespace(obj.GetNamespace()), client.MatchingFields{ingestFlowProfileRefIndex: obj.GetName()}); err != nil {
		return nil
	}
	requests := make([]reconcile.Request, 0, len(list.Items))
	for i := range list.Items {
		requests = append(requests, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(&list.Items[i])})
	}
	return requests
}

func defaultIngestRunSpec(spec tamossv1alpha1.IngestRunSpec) tamossv1alpha1.IngestRunSpec {
	if spec.Profile == "" {
		spec.Profile = tamossv1alpha1.IngestRunProfileEssenceSegments
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
		return 0, "RetryParentConfigurationMismatch", "A retry must preserve the parent's input, profile, size class, options, and output intent", nil
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
	if spec.Options.StorageBackendRef == nil {
		return "", "", "", nil
	}
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

func (r *IngestRunReconciler) resolveIngestFlowProfiles(ctx context.Context, run *tamossv1alpha1.IngestRun, spec tamossv1alpha1.IngestRunSpec) ([]tamossv1alpha1.IngestRunResolvedFlowProfileStatus, string, string, error) {
	resolved := make([]tamossv1alpha1.IngestRunResolvedFlowProfileStatus, 0, len(spec.Options.TAMSFlowProfiles))
	for _, assignment := range spec.Options.TAMSFlowProfiles {
		item := tamossv1alpha1.IngestRunResolvedFlowProfileStatus{Format: assignment.Format, Index: assignment.Index, ProfileID: assignment.ProfileID}
		if assignment.ProfileRef == nil {
			resolved = append(resolved, item)
			continue
		}
		name := assignment.ProfileRef.Name
		profile := &tamossv1alpha1.FlowProfile{}
		key := client.ObjectKey{Namespace: run.Namespace, Name: name}
		if err := r.Client.Get(ctx, key, profile); err != nil {
			if apierrors.IsNotFound(err) {
				return nil, "IngestFlowProfileNotFound", fmt.Sprintf("FlowProfile %s/%s does not exist", run.Namespace, name), nil
			}
			return nil, "", "", err
		}
		if profile.Spec.TamossRef.Name != spec.TamossRef.Name {
			return nil, "IngestFlowProfileTargetMismatch", "The selected FlowProfile belongs to a different Tamoss instance", nil
		}
		if !profile.DeletionTimestamp.IsZero() || !flowProfileReady(profile) || profile.Status.ProfileID == "" {
			return nil, "IngestFlowProfileNotReady", fmt.Sprintf("FlowProfile %s/%s has not reached Ready", run.Namespace, name), nil
		}
		if flowProfileAssignmentFormat(profile.Status.Resolved.Format) != assignment.Format {
			return nil, "IngestFlowProfileFormatMismatch", "The selected FlowProfile format does not match the requested essence stream", nil
		}
		item.ProfileID = profile.Status.ProfileID
		item.ProfileRef = name
		resolved = append(resolved, item)
	}
	return resolved, "", "", nil
}

func flowProfileAssignmentFormat(format string) string {
	switch format {
	case "urn:x-nmos:format:video":
		return ingestFlowFormatVideo
	case "urn:x-nmos:format:audio":
		return "audio"
	case "urn:x-tam:format:image":
		return "image"
	case "urn:x-nmos:format:data":
		return "data"
	default:
		return ""
	}
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
	if strings.TrimSpace(resolved.SourceName) == "" {
		return fmt.Errorf("resolved source policy is incomplete")
	}
	if decoded, err := hex.DecodeString(resolved.PolicyDigest); err != nil || len(decoded) != sha256.Size {
		return fmt.Errorf("resolved source policy digest is invalid")
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
		case "https":
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
			result, err := r.setIngestRunPhase(ctx, run, tamossv1alpha1.IngestRunPhaseRunning, "CancellationRequested", "The TAMSin Pod is terminating", true)
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
	result, err := r.setIngestRunJobPhase(ctx, run, job, tamossv1alpha1.IngestRunPhaseRunning, "CancellationRequested", "The TAMSin Job is terminating", true, 0, 0)
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
	return r.setIngestRunJobPhaseWithStream(ctx, run, job, phase, reason, message, progressing, inputsTotal, attempt, nil)
}

func (r *IngestRunReconciler) setIngestRunJobPhaseWithStream(ctx context.Context, run *tamossv1alpha1.IngestRun, job *batchv1.Job, phase tamossv1alpha1.IngestRunPhase, reason, message string, progressing bool, inputsTotal, attempt int32, stream *ingestStreamSummary) (ctrl.Result, error) {
	original := run.DeepCopy()
	applyIngestStreamSummary(&run.Status, stream)
	if ingestResultRequiredForPhase(phase) &&
		ingestResultVerificationRequired(run.Status.ResultRef) &&
		!isDigestVerifiedIngestResult(run.Status.ResultRef) {
		phase = tamossv1alpha1.IngestRunPhaseRunning
		reason = "ResultVerificationPending"
		message = "A durable result was recorded but has not passed digest verification"
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
		run.Status.ResolvedSource.Name = job.Annotations[ingestSourceAnnotation]
		run.Status.ResolvedSource.PolicyDigest = job.Annotations[ingestSourcePolicyAnnotation]
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

// ingestJobFinished reports whether the Job has reached either terminal
// condition, which is when its Pod's event stream is complete and worth
// reading.
func ingestJobFinished(job *batchv1.Job) bool {
	for _, condition := range job.Status.Conditions {
		if condition.Status != corev1.ConditionTrue {
			continue
		}
		if condition.Type == batchv1.JobComplete || condition.Type == batchv1.JobFailed {
			return true
		}
	}
	return false
}

func ingestStreamFailurePhase(job *batchv1.Job, now time.Time, streamErr error) (tamossv1alpha1.IngestRunPhase, string, string) {
	if errors.Is(streamErr, errIngestStreamInvalid) {
		return tamossv1alpha1.IngestRunPhaseFailed, "IngestProtocolInvalid", "TAMSin produced an invalid terminal event stream"
	}
	if ingestObservationDeadlineExpired(ingestJobTerminalReference(job), now) {
		return tamossv1alpha1.IngestRunPhaseFailed, "IngestResultUnavailable", "The terminal TAMSin event stream was not available within the observation deadline"
	}
	return tamossv1alpha1.IngestRunPhaseRunning, "IngestResultPending", "The Job is terminal, but its TAMSin event stream is not yet available"
}

func ingestJobTerminalReference(job *batchv1.Job) metav1.Time {
	if job.Status.CompletionTime != nil {
		return *job.Status.CompletionTime
	}
	for _, condition := range job.Status.Conditions {
		if condition.Status == corev1.ConditionTrue && (condition.Type == batchv1.JobComplete || condition.Type == batchv1.JobFailed) {
			return condition.LastTransitionTime
		}
	}
	return metav1.Time{}
}

func ingestPhaseFromJob(ctx context.Context, reader client.Reader, job *batchv1.Job, result tamossv1alpha1.IngestRunResultStatus, now time.Time, stream *ingestStreamSummary) (tamossv1alpha1.IngestRunPhase, string, string, bool, error) {
	_ = ctx
	_ = reader
	if ingestJobFinished(job) {
		if stream == nil || !stream.RunFinished {
			return tamossv1alpha1.IngestRunPhaseRunning, "IngestResultPending", "The Job is terminal, but its TAMSin event stream is not yet available", false, nil
		}
		if (stream.Outcome == ingestevent.RunSucceeded || stream.Outcome == ingestevent.RunPartial) &&
			ingestResultVerificationRequired(result) && !isDigestVerifiedIngestResult(result) {
			if ingestObservationDeadlineExpired(ingestJobTerminalReference(job), now) {
				return tamossv1alpha1.IngestRunPhaseFailed, "ResultVerificationTimeout", "A durable result was recorded but did not pass digest verification within the observation deadline", false, nil
			}
			return tamossv1alpha1.IngestRunPhaseRunning, "ResultVerificationPending", "A durable result was recorded but has not passed digest verification", false, nil
		}
		switch stream.Outcome {
		case ingestevent.RunSucceeded:
			return tamossv1alpha1.IngestRunPhaseSucceeded, "IngestSucceeded", "TAMSin completed the ingest run", false, nil
		case ingestevent.RunPartial:
			return tamossv1alpha1.IngestRunPhasePartiallySucceeded, "IngestPartiallySucceeded", "TAMSin completed with both successful and failed inputs", false, nil
		case ingestevent.RunInterrupted:
			return tamossv1alpha1.IngestRunPhaseFailed, "IngestInterrupted", "TAMSin was interrupted before the ingest completed", false, nil
		case ingestevent.RunFailed:
			return tamossv1alpha1.IngestRunPhaseFailed, "IngestFailed", "TAMSin reported an ingest failure", false, nil
		default:
			return tamossv1alpha1.IngestRunPhaseFailed, "IngestProtocolInvalid", "TAMSin reported an unknown terminal outcome", false, nil
		}
	}
	if job.Status.Active > 0 {
		return tamossv1alpha1.IngestRunPhaseRunning, "IngestRunning", "TAMSin is processing media", true, nil
	}
	if job.Status.StartTime != nil {
		return tamossv1alpha1.IngestRunPhaseRunning, "JobStarting", "The TAMSin Pod is starting", true, nil
	}
	return tamossv1alpha1.IngestRunPhaseQueued, "JobQueued", "The TAMSin Job is waiting to be scheduled", true, nil
}

func ingestResultRequiredForPhase(phase tamossv1alpha1.IngestRunPhase) bool {
	return phase == tamossv1alpha1.IngestRunPhaseSucceeded || phase == tamossv1alpha1.IngestRunPhasePartiallySucceeded
}

// ingestResultVerificationRequired reports whether a recorded durable result
// must pass digest verification before the run is believed.
//
// TAMSin v1.0.0-rc.1 publishes a complete, versioned terminal event stream but
// does not publish a separate durable result artefact. Demanding such an
// artefact unconditionally would make success unreachable for every run.
// TAMSin's own --verify pass still reads back and checks each uploaded Media
// Object, while the operator validates the event lifecycle and matching exit
// code.
//
// The stronger gate stays armed for whatever does record a result: once a key
// is present it must carry a valid digest, so a collector cannot publish a
// half-written or unverifiable artifact and have the run claim success on it.
func ingestResultVerificationRequired(result tamossv1alpha1.IngestRunResultStatus) bool {
	return strings.TrimSpace(result.Key) != ""
}

// ingestObservationDeadlineExpired reports whether the operator has waited long
// enough for evidence about a finished Job. A zero reference means the API
// server has not timestamped the transition yet, so the wait continues.
func ingestObservationDeadlineExpired(reference metav1.Time, now time.Time) bool {
	if reference.IsZero() {
		return false
	}
	return now.Sub(reference.Time) > ingestTerminalObservationDeadline
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

func desiredIngestJob(run *tamossv1alpha1.IngestRun, spec tamossv1alpha1.IngestRunSpec, tamoss *tamossv1alpha1.Tamoss, endpoint, image, storageID, flowMetadata string, resolved ResolvedIngestInputs) *batchv1.Job {
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
		"--profile", string(spec.Profile),
		"--max-inputs", fmt.Sprintf("%d", spec.Options.MaxInputs),
		"--concurrency", fmt.Sprintf("%d", concurrency),
		"--staging-byte-budget", jobResources.StagingByteBudget,
		"--verify=" + ingestVerifyMode(ptr.Deref(spec.Options.Verify, true)),
	}
	if resolved.S3Endpoint != "" {
		args = append(args, "--s3-endpoint", resolved.S3Endpoint, "--s3-region", resolved.S3Region)
		if resolved.S3PathStyle {
			args = append(args, "--s3-path-style")
		}
	}
	if spec.Options.DryRun {
		args = append(args, "--dry-run=exact")
	}
	args = append(args, tamsFlowProfileArguments(spec.Options.TAMSFlowProfiles)...)
	if tamoss.Spec.Profile == tamossv1alpha1.TamossProfileLocalKind {
		args = append(args, "--insecure-skip-verify")
	}
	if storageID != "" {
		args = append(args, "--storage-id", storageID)
	}
	podAnnotations := map[string]string{}
	volumeMounts := []corev1.VolumeMount{{Name: "temporary-media", MountPath: "/tmp"}}
	volumes := []corev1.Volume{{
		Name:         "temporary-media",
		VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{SizeLimit: &jobResources.TemporarySize}},
	}}
	if flowMetadata != "" {
		podAnnotations[ingestFlowMetadataAnnotation] = flowMetadata
		args = append(args, "--flow-metadata", ingestFlowMetadataFile)
		volumeMounts = append(volumeMounts, corev1.VolumeMount{
			Name: ingestFlowMetadataVolume, MountPath: ingestFlowMetadataMountPath, ReadOnly: true,
		})
		volumes = append(volumes, corev1.Volume{
			Name: ingestFlowMetadataVolume,
			VolumeSource: corev1.VolumeSource{DownwardAPI: &corev1.DownwardAPIVolumeSource{
				Items: []corev1.DownwardAPIVolumeFile{{
					Path: "flow-metadata.json",
					FieldRef: &corev1.ObjectFieldSelector{
						FieldPath: "metadata.annotations['" + ingestFlowMetadataAnnotation + "']",
					},
				}},
			}},
		})
	}
	for _, selector := range resolved.Selectors {
		args = append(args, "--input", selector)
	}
	env := []corev1.EnvVar{
		{Name: "TAMSIN_AUTH_MODE", Value: "bearer"},
		{
			Name: "TAMSIN_AUTH_TOKEN",
			ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{Name: tamoss.ResourceName("api-token")},
				Key:                  "TAMOSS_API_TOKEN",
			}},
		},
	}
	if resolved.CredentialSecretName != "" {
		switch resolved.CredentialKind {
		case tamossv1alpha1.IngestSourceKindHTTP:
			env = append(env, sourceSecretEnv(httpCredentialSecretKey, resolved.CredentialSecretName, false))
		case tamossv1alpha1.IngestSourceKindS3:
			env = append(env,
				sourceSecretEnv(s3AccessKeySecretKey, resolved.CredentialSecretName, false),
				sourceSecretEnv(s3SecretKeySecretKey, resolved.CredentialSecretName, false),
				sourceSecretEnv(s3SessionTokenSecretKey, resolved.CredentialSecretName, true),
			)
		}
	}

	labels := map[string]string{
		"app.kubernetes.io/name":       "tamsin",
		appComponentLabel:              "ingest",
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
			Annotations: map[string]string{
				ingestSourceAnnotation:       resolved.SourceName,
				ingestSourcePolicyAnnotation: resolved.PolicyDigest,
			},
		},
		Spec: batchv1.JobSpec{
			BackoffLimit:            ptr.To[int32](2),
			ActiveDeadlineSeconds:   ptr.To[int64](21600),
			TTLSecondsAfterFinished: ptr.To[int32](3600),
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: labels, Annotations: podAnnotations},
				Spec: corev1.PodSpec{
					AutomountServiceAccountToken: ptr.To(false),
					// TAMSin rejects any TAMSIN_-prefixed variable it does not
					// recognise and exits before doing any work. Service links
					// inject one per Service in the namespace, so a Service whose
					// name starts with "tamsin" would break every ingest here.
					EnableServiceLinks:            ptr.To(false),
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
						Env:             env,
						Resources:       corev1.ResourceRequirements{Requests: jobResources.Requests, Limits: jobResources.Limits},
						SecurityContext: &corev1.SecurityContext{
							AllowPrivilegeEscalation: ptr.To(false),
							ReadOnlyRootFilesystem:   ptr.To(true),
							Capabilities:             &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}},
						},
						VolumeMounts: volumeMounts,
					}},
					Volumes: volumes,
				},
			},
		},
	}
}

func ingestVerifyMode(verify bool) string {
	if verify {
		return "auto"
	}
	return "none"
}

func tamsFlowProfileArguments(assignments []tamossv1alpha1.IngestRunTAMSFlowProfile) []string {
	ordered := append([]tamossv1alpha1.IngestRunTAMSFlowProfile(nil), assignments...)
	formatOrder := map[string]int{ingestFlowFormatVideo: 0, "audio": 1, "image": 2, "data": 3}
	sort.Slice(ordered, func(i, j int) bool {
		left, right := ordered[i], ordered[j]
		if formatOrder[left.Format] != formatOrder[right.Format] {
			return formatOrder[left.Format] < formatOrder[right.Format]
		}
		return left.Index < right.Index
	})
	args := make([]string, 0, len(ordered)*2)
	for _, assignment := range ordered {
		args = append(args, "--tams-flow-profile", fmt.Sprintf("%s:%d=%s", assignment.Format, assignment.Index, assignment.ProfileID))
	}
	return args
}

func sourceSecretEnv(key, secretName string, optional bool) corev1.EnvVar {
	return corev1.EnvVar{
		Name: key,
		ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{
			LocalObjectReference: corev1.LocalObjectReference{Name: secretName},
			Key:                  key,
			Optional:             ptr.To(optional),
		}},
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
