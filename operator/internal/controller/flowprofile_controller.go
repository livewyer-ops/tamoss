package controller

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	tamossv1alpha1 "github.com/livewyer-ops/tamoss/operator/api/v1alpha1"
	"github.com/livewyer-ops/tamoss/operator/internal/controller/defaults"
	"github.com/livewyer-ops/tamoss/operator/internal/controller/resource"
	schemabundle "github.com/livewyer-ops/tamoss/operator/internal/schema"
	operatorstatus "github.com/livewyer-ops/tamoss/operator/internal/status"
)

const (
	flowProfileFinalizer                = "flowprofile.tamoss.livewyer.io/finalizer"
	flowProfileTamossIndex              = ".spec.tamossRef.name"
	flowProfileDesiredHash              = "tamoss.livewyer.io/desired-hash"
	flowProfileJobHash                  = "tamoss.livewyer.io/job-hash"
	flowProfileStateReady               = "ready"
	flowProfileStateValueReady          = "true"
	flowProfileStateID                  = "profileID"
	flowProfileStateHash                = "desiredHash"
	flowProfileStateJobUID              = "jobUID"
	flowProfileStateGeneration          = "observedGeneration"
	flowProfileConditionReady           = "Ready"
	flowProfileConditionRegistered      = "Registered"
	flowProfileConditionDeletionBlocked = "DeletionBlocked"
	flowProfileExitInvalid              = int32(2)
	flowProfileExitConflict             = int32(3)
	flowProfileExitInUse                = int32(4)
	flowProfileRetryInterval            = 30 * time.Second
	flowProfileInUseRetryInterval       = 5 * time.Minute
)

type FlowProfileReconciler struct {
	Client          client.Client
	Scheme          *runtime.Scheme
	Recorder        record.EventRecorder
	WatchNamespaces WatchNamespaceSet
}

type resolvedFlowProfile struct {
	Spec     tamossv1alpha1.FlowProfileSpec
	Document string
	Hash     string
	Format   string
	Codec    string
}

type flowProfileStatusInput struct {
	Phase           tamossv1alpha1.FlowProfilePhase
	Ready           bool
	Registered      bool
	DeletionBlocked bool
	Reason          string
	Message         string
	ProfileID       string
	TamossName      string
	Format          string
	Codec           string
	RequeueAfter    time.Duration
}

// +kubebuilder:rbac:groups=tamoss.livewyer.io,resources=flowprofiles,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=tamoss.livewyer.io,resources=flowprofiles/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=tamoss.livewyer.io,resources=flowprofiles/finalizers,verbs=update
// +kubebuilder:rbac:groups=tamoss.livewyer.io,resources=tamosses,verbs=get;list;watch
// +kubebuilder:rbac:groups=batch,namespace=system,resources=jobs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",namespace=system,resources=configmaps;events;secrets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",namespace=system,resources=pods,verbs=get;list;watch;delete

func (r *FlowProfileReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	profile := &tamossv1alpha1.FlowProfile{}
	if err := r.Client.Get(ctx, req.NamespacedName, profile); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	if !r.WatchNamespaces.Allows(profile.Namespace) {
		return ctrl.Result{}, nil
	}
	if !profile.DeletionTimestamp.IsZero() {
		return r.finaliseFlowProfile(ctx, profile)
	}
	if controllerutil.AddFinalizer(profile, flowProfileFinalizer) {
		if err := r.Client.Update(ctx, profile); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	resolved, err := resolveFlowProfile(profile)
	if err != nil {
		return r.updateFlowProfileStatus(ctx, profile, flowProfileStatusInput{
			Phase: tamossv1alpha1.FlowProfilePhaseDegraded, Reason: "InvalidProfile", Message: err.Error(),
		})
	}
	tamoss, found, err := r.flowProfileTamoss(ctx, profile.Namespace, resolved.Spec.TamossRef.Name)
	if err != nil {
		return ctrl.Result{}, err
	}
	if !found {
		return r.updateFlowProfileStatus(ctx, profile, flowProfileStatusInput{
			Phase: tamossv1alpha1.FlowProfilePhasePending, Reason: operatorstatus.ReasonTamossNotFound,
			Message: "Referenced Tamoss was not found", ProfileID: resolved.Spec.ID,
			TamossName: resolved.Spec.TamossRef.Name, Format: resolved.Format, Codec: resolved.Codec,
			RequeueAfter: flowProfileRetryInterval,
		})
	}
	if !r.flowProfileSchemaReady(ctx, tamoss) || tamoss.Status.Resolved.Versions.TAMSAPI != schemabundle.SupportedTAMSAPIVersion {
		return r.updateFlowProfileStatus(ctx, profile, flowProfileStatusInput{
			Phase: tamossv1alpha1.FlowProfilePhasePending, Reason: operatorstatus.ReasonWaitingForSchema,
			Message: "The target TAMS 8.2 schema is not ready", ProfileID: resolved.Spec.ID,
			TamossName: tamoss.Name, Format: resolved.Format, Codec: resolved.Codec,
			RequeueAfter: flowProfileRetryInterval,
		})
	}
	if winner, err := r.flowProfileIDWinner(ctx, profile, resolved.Spec); err != nil {
		return ctrl.Result{}, err
	} else if winner != profile.Name {
		if err := r.clearFlowProfileRegistrationState(ctx, profile); err != nil {
			return ctrl.Result{}, err
		}
		return r.updateFlowProfileStatus(ctx, profile, flowProfileStatusInput{
			Phase: tamossv1alpha1.FlowProfilePhaseDegraded, Reason: "DuplicateProfileID",
			Message:   fmt.Sprintf("FlowProfile %s owns this Profile UUID for the target Tamoss", winner),
			ProfileID: resolved.Spec.ID, TamossName: tamoss.Name, Format: resolved.Format, Codec: resolved.Codec,
		})
	}

	registered, err := r.flowProfileRegistered(ctx, profile, resolved)
	if err != nil {
		return ctrl.Result{}, err
	}
	if registered {
		return r.updateFlowProfileStatus(ctx, profile, flowProfileStatusInput{
			Phase: tamossv1alpha1.FlowProfilePhaseReady, Ready: true, Registered: true,
			Reason: "FlowProfileReady", Message: "TAMS Flow Profile is registered",
			ProfileID: resolved.Spec.ID, TamossName: tamoss.Name, Format: resolved.Format, Codec: resolved.Codec,
		})
	}
	if err := r.applyFlowProfileDocument(ctx, profile, resolved); err != nil {
		return ctrl.Result{}, err
	}
	job := flowProfileRegistrationJob(profile, tamoss, resolved)
	if err := controllerutil.SetControllerReference(profile, job, r.Scheme); err != nil {
		return ctrl.Result{}, err
	}
	live, found, err := r.getFlowProfileJob(ctx, job)
	if err != nil {
		return ctrl.Result{}, err
	}
	if !found {
		if _, err := applyManagedObject(ctx, r.Client, job); err != nil {
			return ctrl.Result{}, err
		}
		return r.progressFlowProfile(ctx, profile, resolved, tamoss.Name, "RegistrationInProgress", "TAMS Flow Profile registration job was launched")
	}
	if flowProfileJobDrifted(live, job) {
		if err := r.Client.Delete(ctx, live, client.PropagationPolicy(metav1.DeletePropagationForeground)); err != nil && !apierrors.IsNotFound(err) {
			return ctrl.Result{}, err
		}
		return r.progressFlowProfile(ctx, profile, resolved, tamoss.Name, "RegistrationRuntimeChanged", "The registration runtime changed and the Job will be replaced")
	}
	if jobFailed(live) {
		exitCode, known, err := r.flowProfileJobExitCode(ctx, live)
		if err != nil {
			return ctrl.Result{}, err
		}
		if known && (exitCode == flowProfileExitInvalid || exitCode == flowProfileExitConflict) {
			reason := "RegistrationFailed"
			switch exitCode {
			case flowProfileExitInvalid:
				reason = "InvalidProfile"
			case flowProfileExitConflict:
				reason = "ProfileConflict"
			}
			return r.updateFlowProfileStatus(ctx, profile, flowProfileStatusInput{
				Phase: tamossv1alpha1.FlowProfilePhaseDegraded, Reason: reason,
				Message:   "TAMS Flow Profile registration was rejected; inspect the owned Job logs",
				ProfileID: resolved.Spec.ID, TamossName: tamoss.Name, Format: resolved.Format, Codec: resolved.Codec,
			})
		}
		if err := r.Client.Delete(ctx, live, client.PropagationPolicy(metav1.DeletePropagationForeground)); err != nil && !apierrors.IsNotFound(err) {
			return ctrl.Result{}, err
		}
		result, statusErr := r.progressFlowProfile(ctx, profile, resolved, tamoss.Name, "RegistrationRetrying", "TAMS Flow Profile registration failed and will be retried")
		result.RequeueAfter = flowProfileRetryInterval
		return result, statusErr
	}
	if !jobSucceeded(live) {
		return r.progressFlowProfile(ctx, profile, resolved, tamoss.Name, "RegistrationInProgress", "TAMS Flow Profile registration job is running")
	}
	if err := r.applyFlowProfileState(ctx, profile, resolved, live); err != nil {
		return ctrl.Result{}, err
	}
	return r.updateFlowProfileStatus(ctx, profile, flowProfileStatusInput{
		Phase: tamossv1alpha1.FlowProfilePhaseReady, Ready: true, Registered: true,
		Reason: "FlowProfileReady", Message: "TAMS Flow Profile is registered",
		ProfileID: resolved.Spec.ID, TamossName: tamoss.Name, Format: resolved.Format, Codec: resolved.Codec,
	})
}

func (r *FlowProfileReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if err := mgr.GetFieldIndexer().IndexField(context.Background(), &tamossv1alpha1.FlowProfile{}, flowProfileTamossIndex, func(obj client.Object) []string {
		profile, ok := obj.(*tamossv1alpha1.FlowProfile)
		if !ok || strings.TrimSpace(profile.Spec.TamossRef.Name) == "" {
			return nil
		}
		return []string{profile.Spec.TamossRef.Name}
	}); err != nil {
		return err
	}
	return ctrl.NewControllerManagedBy(mgr).
		For(&tamossv1alpha1.FlowProfile{}, builder.WithPredicates(primaryResourcePredicate(flowProfileFinalizer, nil))).
		Owns(&batchv1.Job{}).
		Owns(&corev1.ConfigMap{}).
		Watches(&tamossv1alpha1.Tamoss{}, handler.EnqueueRequestsFromMapFunc(r.flowProfilesForTamoss)).
		Complete(r)
}

func (r *FlowProfileReconciler) flowProfilesForTamoss(ctx context.Context, obj client.Object) []reconcile.Request {
	list := &tamossv1alpha1.FlowProfileList{}
	if err := r.Client.List(ctx, list, client.InNamespace(obj.GetNamespace()), client.MatchingFields{flowProfileTamossIndex: obj.GetName()}); err != nil {
		return nil
	}
	requests := make([]reconcile.Request, 0, len(list.Items))
	for i := range list.Items {
		requests = append(requests, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(&list.Items[i])})
	}
	return requests
}

func resolveFlowProfile(profile *tamossv1alpha1.FlowProfile) (resolvedFlowProfile, error) {
	spec := profile.Spec
	spec.ApplyDefaults(profile.Namespace, profile.Name)
	if strings.TrimSpace(spec.TamossRef.Name) == "" {
		return resolvedFlowProfile{}, fmt.Errorf("spec.tamossRef.name is required")
	}
	if err := tamossv1alpha1.ValidateTAMSTags(spec.Tags); err != nil {
		return resolvedFlowProfile{}, fmt.Errorf("spec.tags is invalid: %w", err)
	}
	metadata := map[string]any{}
	if len(spec.FlowMetadata.Raw) == 0 || json.Unmarshal(spec.FlowMetadata.Raw, &metadata) != nil {
		return resolvedFlowProfile{}, fmt.Errorf("spec.flowMetadata must be a JSON object")
	}
	format, ok := metadata["format"].(string)
	if !ok || !supportedFlowProfileFormat(format) {
		return resolvedFlowProfile{}, fmt.Errorf("spec.flowMetadata.format is not a supported TAMS Profile format")
	}
	codec := ""
	if value, exists := metadata["codec"]; exists {
		var valid bool
		codec, valid = value.(string)
		if !valid {
			return resolvedFlowProfile{}, fmt.Errorf("spec.flowMetadata.codec must be a string")
		}
	}
	documentValue := map[string]any{
		"id":            spec.ID,
		"flow_metadata": metadata,
	}
	if spec.Label != "" {
		documentValue["label"] = spec.Label
	}
	if spec.Description != "" {
		documentValue["description"] = spec.Description
	}
	if len(spec.Tags) > 0 {
		tagsJSON, err := json.Marshal(spec.Tags)
		if err != nil {
			return resolvedFlowProfile{}, err
		}
		var tags map[string]any
		if err := json.Unmarshal(tagsJSON, &tags); err != nil {
			return resolvedFlowProfile{}, err
		}
		documentValue["tags"] = tags
	}
	document, err := json.Marshal(documentValue)
	if err != nil {
		return resolvedFlowProfile{}, err
	}
	hash := sha256.Sum256(document)
	return resolvedFlowProfile{Spec: spec, Document: string(document), Hash: fmt.Sprintf("%x", hash[:]), Format: format, Codec: codec}, nil
}

func supportedFlowProfileFormat(value string) bool {
	switch value {
	case "urn:x-nmos:format:video", "urn:x-nmos:format:audio", "urn:x-tam:format:image", "urn:x-nmos:format:data":
		return true
	default:
		return false
	}
}

func (r *FlowProfileReconciler) flowProfileTamoss(ctx context.Context, namespace, name string) (*tamossv1alpha1.Tamoss, bool, error) {
	tamoss := &tamossv1alpha1.Tamoss{}
	if err := r.Client.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, tamoss); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, false, nil
		}
		return nil, false, err
	}
	resolved := tamoss.DeepCopy()
	defaults.Apply(resolved)
	return resolved, true, nil
}

func (r *FlowProfileReconciler) flowProfileSchemaReady(ctx context.Context, tamoss *tamossv1alpha1.Tamoss) bool {
	state := &corev1.ConfigMap{}
	key := types.NamespacedName{Namespace: tamoss.Namespace, Name: tamossResourceName(tamoss, "schema-state")}
	if err := r.Client.Get(ctx, key, state); err != nil {
		return false
	}
	return state.Data[schemaStateAppliedVersionKey] == schemabundle.SchemaVersion
}

func (r *FlowProfileReconciler) flowProfileIDWinner(ctx context.Context, current *tamossv1alpha1.FlowProfile, spec tamossv1alpha1.FlowProfileSpec) (string, error) {
	candidates, err := r.flowProfileIDCandidates(ctx, current, spec)
	if err != nil {
		return "", err
	}
	if len(candidates) == 0 {
		return current.Name, nil
	}
	return candidates[0].Name, nil
}

func (r *FlowProfileReconciler) flowProfileIDCandidates(ctx context.Context, current *tamossv1alpha1.FlowProfile, spec tamossv1alpha1.FlowProfileSpec) ([]*tamossv1alpha1.FlowProfile, error) {
	list := &tamossv1alpha1.FlowProfileList{}
	if err := r.Client.List(ctx, list, client.InNamespace(current.Namespace), client.MatchingFields{flowProfileTamossIndex: spec.TamossRef.Name}); err != nil {
		return nil, err
	}
	candidates := make([]*tamossv1alpha1.FlowProfile, 0, len(list.Items))
	for i := range list.Items {
		candidateSpec := list.Items[i].Spec
		candidateSpec.ApplyDefaults(list.Items[i].Namespace, list.Items[i].Name)
		if candidateSpec.ID == spec.ID && (list.Items[i].DeletionTimestamp.IsZero() || list.Items[i].Name == current.Name) {
			candidates = append(candidates, &list.Items[i])
		}
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].CreationTimestamp.Equal(&candidates[j].CreationTimestamp) {
			return candidates[i].Name < candidates[j].Name
		}
		return candidates[i].CreationTimestamp.Before(&candidates[j].CreationTimestamp)
	})
	return candidates, nil
}

func flowProfileLabels(profile *tamossv1alpha1.FlowProfile) map[string]string {
	return map[string]string{
		resource.LabelName:                tamossAppName,
		appInstanceLabel:                  profile.Spec.TamossRef.Name,
		appComponentLabel:                 "flow-profile",
		resource.LabelManagedBy:           resource.ManagedBy,
		"tamoss.livewyer.io/flow-profile": profile.Name,
	}
}

func flowProfileResourceName(profile *tamossv1alpha1.FlowProfile, suffix string) string {
	base := profile.Name + "-" + suffix
	if len(base) <= 63 {
		return base
	}
	hash := sha256.Sum256([]byte(base))
	return fmt.Sprintf("%.54s-%x", strings.TrimRight(profile.Name, "-"), hash[:4])
}

func (r *FlowProfileReconciler) applyFlowProfileDocument(ctx context.Context, profile *tamossv1alpha1.FlowProfile, resolved resolvedFlowProfile) error {
	configMap := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{
		Name: flowProfileResourceName(profile, "document"), Namespace: profile.Namespace, Labels: flowProfileLabels(profile),
	}, Data: map[string]string{"profile.json": resolved.Document}}
	if err := controllerutil.SetControllerReference(profile, configMap, r.Scheme); err != nil {
		return err
	}
	_, err := applyManagedObject(ctx, r.Client, configMap)
	return err
}

func flowProfileRegistrationJob(profile *tamossv1alpha1.FlowProfile, tamoss *tamossv1alpha1.Tamoss, resolved resolvedFlowProfile) *batchv1.Job {
	return flowProfileJob(profile, tamoss, resolved, "register", []string{
		"run", "tamoss-profile", "ensure", "--file", "/profile/profile.json", "--created-by", fmt.Sprintf("tamoss-operator:%s/%s", profile.Namespace, profile.Name),
	})
}

func flowProfileDeletionJob(profile *tamossv1alpha1.FlowProfile, tamoss *tamossv1alpha1.Tamoss, resolved resolvedFlowProfile) *batchv1.Job {
	return flowProfileJob(profile, tamoss, resolved, "delete", []string{"run", "tamoss-profile", "delete-if-unused", "--id", resolved.Spec.ID})
}

func flowProfileJob(profile *tamossv1alpha1.FlowProfile, tamoss *tamossv1alpha1.Tamoss, resolved resolvedFlowProfile, action string, args []string) *batchv1.Job {
	backoff := int32(1)
	if action == "delete" {
		backoff = 0
	}
	labels := flowProfileLabels(profile)
	container := corev1.Container{
		Name: "flow-profile-" + action, Image: schemaMigrationRuntimeImage(tamoss), ImagePullPolicy: corev1.PullIfNotPresent,
		Command: []string{"uv"}, Args: args, Env: flowProfileDatabaseEnv(tamoss),
		SecurityContext: &corev1.SecurityContext{
			AllowPrivilegeEscalation: ptr.To(false),
			Capabilities:             &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}},
		},
	}
	if action == "register" {
		container.VolumeMounts = []corev1.VolumeMount{{Name: "profile", MountPath: "/profile", ReadOnly: true}}
	}
	podSpec := corev1.PodSpec{
		AutomountServiceAccountToken: ptr.To(false),
		EnableServiceLinks:           ptr.To(false),
		RestartPolicy:                corev1.RestartPolicyNever,
		SecurityContext: &corev1.PodSecurityContext{
			RunAsNonRoot: ptr.To(true),
			SeccompProfile: &corev1.SeccompProfile{
				Type: corev1.SeccompProfileTypeRuntimeDefault,
			},
		},
		Containers: []corev1.Container{container},
	}
	if action == "register" {
		podSpec.Volumes = []corev1.Volume{{Name: "profile", VolumeSource: corev1.VolumeSource{ConfigMap: &corev1.ConfigMapVolumeSource{
			LocalObjectReference: corev1.LocalObjectReference{Name: flowProfileResourceName(profile, "document")},
		}}}}
	}
	return &batchv1.Job{ObjectMeta: metav1.ObjectMeta{
		Name: flowProfileResourceName(profile, action), Namespace: profile.Namespace, Labels: labels,
		Annotations: map[string]string{
			flowProfileDesiredHash: resolved.Hash,
			flowProfileJobHash:     flowProfileJobInputHash(tamoss, resolved, action, args),
		},
	}, Spec: batchv1.JobSpec{
		BackoffLimit:          &backoff,
		ActiveDeadlineSeconds: ptr.To[int64](300),
		Template:              corev1.PodTemplateSpec{ObjectMeta: metav1.ObjectMeta{Labels: labels}, Spec: podSpec},
	}}
}

func flowProfileJobInputHash(tamoss *tamossv1alpha1.Tamoss, resolved resolvedFlowProfile, action string, args []string) string {
	db := tamoss.DBConnection()
	values := make([]string, 0, 9+len(args))
	values = append(values,
		action,
		resolved.Hash,
		schemaMigrationRuntimeImage(tamoss),
		db.Host,
		db.Port,
		db.Database,
		db.Auth.ExistingSecret,
		db.Auth.SecretKeys.Username,
		db.Auth.SecretKeys.Password,
	)
	values = append(values, args...)
	hash := sha256.Sum256([]byte(strings.Join(values, "\x00")))
	return fmt.Sprintf("%x", hash[:])
}

func flowProfileJobDrifted(live, desired *batchv1.Job) bool {
	return live.Annotations[flowProfileJobHash] == "" || live.Annotations[flowProfileJobHash] != desired.Annotations[flowProfileJobHash]
}

func flowProfileDatabaseEnv(tamoss *tamossv1alpha1.Tamoss) []corev1.EnvVar {
	db := tamoss.DBConnection()
	return []corev1.EnvVar{
		{Name: "POSTGRES_HOST", Value: db.Host},
		{Name: "POSTGRES_PORT", Value: db.Port},
		{Name: "POSTGRES_DB", Value: db.Database},
		{
			Name: "POSTGRES_USER",
			ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{Name: db.Auth.ExistingSecret},
				Key:                  db.Auth.SecretKeys.Username,
			}},
		},
		{
			Name: "POSTGRES_PASSWORD",
			ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{Name: db.Auth.ExistingSecret},
				Key:                  db.Auth.SecretKeys.Password,
			}},
		},
	}
}

func (r *FlowProfileReconciler) getFlowProfileJob(ctx context.Context, desired *batchv1.Job) (*batchv1.Job, bool, error) {
	live := &batchv1.Job{}
	if err := r.Client.Get(ctx, client.ObjectKeyFromObject(desired), live); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, false, nil
		}
		return nil, false, err
	}
	return live, true, nil
}

func (r *FlowProfileReconciler) flowProfileJobExitCode(ctx context.Context, job *batchv1.Job) (int32, bool, error) {
	pods := &corev1.PodList{}
	if err := r.Client.List(ctx, pods, client.InNamespace(job.Namespace), client.MatchingLabels{"job-name": job.Name}); err != nil {
		return 0, false, err
	}
	for i := range pods.Items {
		if !podOwnedByJobUID(&pods.Items[i], job.UID) {
			continue
		}
		for _, status := range pods.Items[i].Status.ContainerStatuses {
			if status.State.Terminated != nil {
				return status.State.Terminated.ExitCode, true, nil
			}
		}
	}
	return 0, false, nil
}

func (r *FlowProfileReconciler) applyFlowProfileState(ctx context.Context, profile *tamossv1alpha1.FlowProfile, resolved resolvedFlowProfile, job *batchv1.Job) error {
	state := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: flowProfileResourceName(profile, "state"), Namespace: profile.Namespace, Labels: flowProfileLabels(profile)}, Data: map[string]string{
		flowProfileStateReady: flowProfileStateValueReady, flowProfileStateID: resolved.Spec.ID, flowProfileStateHash: resolved.Hash,
		flowProfileStateJobUID: string(job.UID), flowProfileStateGeneration: fmt.Sprintf("%d", profile.Generation),
	}}
	if err := controllerutil.SetControllerReference(profile, state, r.Scheme); err != nil {
		return err
	}
	_, err := applyManagedObject(ctx, r.Client, state)
	return err
}

func (r *FlowProfileReconciler) flowProfileRegistered(ctx context.Context, profile *tamossv1alpha1.FlowProfile, resolved resolvedFlowProfile) (bool, error) {
	state := &corev1.ConfigMap{}
	err := r.Client.Get(ctx, types.NamespacedName{Namespace: profile.Namespace, Name: flowProfileResourceName(profile, "state")}, state)
	if apierrors.IsNotFound(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return state.Data[flowProfileStateReady] == flowProfileStateValueReady && state.Data[flowProfileStateID] == resolved.Spec.ID && state.Data[flowProfileStateHash] == resolved.Hash, nil
}

func (r *FlowProfileReconciler) progressFlowProfile(ctx context.Context, profile *tamossv1alpha1.FlowProfile, resolved resolvedFlowProfile, tamossName, reason, message string) (ctrl.Result, error) {
	return r.updateFlowProfileStatus(ctx, profile, flowProfileStatusInput{
		Phase: tamossv1alpha1.FlowProfilePhaseProgressing, Reason: reason, Message: message,
		ProfileID: resolved.Spec.ID, TamossName: tamossName, Format: resolved.Format, Codec: resolved.Codec,
		RequeueAfter: flowProfileRetryInterval,
	})
}

func (r *FlowProfileReconciler) updateFlowProfileStatus(ctx context.Context, profile *tamossv1alpha1.FlowProfile, input flowProfileStatusInput) (ctrl.Result, error) {
	original := profile.DeepCopy()
	profile.Status.ObservedGeneration = profile.Generation
	profile.Status.Phase = input.Phase
	profile.Status.ProfileID = input.ProfileID
	profile.Status.Resolved = tamossv1alpha1.FlowProfileResolvedStatus{TamossName: input.TamossName, Format: input.Format, Codec: input.Codec}
	operatorstatus.SetConditionBool(&profile.Status.Conditions, profile.Generation, flowProfileConditionReady, input.Ready, input.Reason, input.Message)
	operatorstatus.SetConditionBool(&profile.Status.Conditions, profile.Generation, flowProfileConditionRegistered, input.Registered, input.Reason, input.Message)
	operatorstatus.SetConditionBool(&profile.Status.Conditions, profile.Generation, flowProfileConditionDeletionBlocked, input.DeletionBlocked, input.Reason, input.Message)
	if err := r.Client.Status().Patch(ctx, profile, client.MergeFromWithOptions(original, client.MergeFromWithOptimisticLock{})); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{RequeueAfter: input.RequeueAfter}, nil
}

func (r *FlowProfileReconciler) finaliseFlowProfile(ctx context.Context, profile *tamossv1alpha1.FlowProfile) (ctrl.Result, error) {
	if !controllerutil.ContainsFinalizer(profile, flowProfileFinalizer) {
		return ctrl.Result{}, nil
	}
	resolved, err := resolveFlowProfile(profile)
	if err != nil {
		return r.removeFlowProfileFinalizer(ctx, profile)
	}
	claims, err := r.flowProfileIDCandidates(ctx, profile, resolved.Spec)
	if err != nil {
		return ctrl.Result{}, err
	}
	if len(claims) > 0 && claims[0].Name != profile.Name {
		if err := r.deleteFlowProfileChildren(ctx, profile); err != nil {
			return ctrl.Result{}, err
		}
		return r.removeFlowProfileFinalizer(ctx, profile)
	}
	if len(claims) > 1 {
		return r.updateFlowProfileStatus(ctx, profile, flowProfileStatusInput{
			Phase: tamossv1alpha1.FlowProfilePhaseDeleting, DeletionBlocked: true,
			Reason:    "ProfileClaimedByFlowProfile",
			Message:   fmt.Sprintf("Profile deletion is blocked while FlowProfile %s also claims its UUID", claims[1].Name),
			ProfileID: resolved.Spec.ID, TamossName: resolved.Spec.TamossRef.Name,
			Format: resolved.Format, Codec: resolved.Codec, RequeueAfter: flowProfileInUseRetryInterval,
		})
	}
	tamoss, found, err := r.flowProfileTamoss(ctx, profile.Namespace, resolved.Spec.TamossRef.Name)
	if err != nil {
		return ctrl.Result{}, err
	}
	registered, err := r.flowProfileRegistered(ctx, profile, resolved)
	if err != nil {
		return ctrl.Result{}, err
	}
	if found && tamoss.DeletionTimestamp.IsZero() && !registered {
		registration, exists, err := r.getFlowProfileJob(ctx, flowProfileRegistrationJob(profile, tamoss, resolved))
		if err != nil {
			return ctrl.Result{}, err
		}
		if exists && metav1.IsControlledBy(registration, profile) {
			if !jobSucceeded(registration) && !jobFailed(registration) {
				return ctrl.Result{RequeueAfter: flowProfileRetryInterval}, nil
			}
			registered = jobSucceeded(registration)
		}
	}
	if found && tamoss.DeletionTimestamp.IsZero() && registered {
		if !r.flowProfileSchemaReady(ctx, tamoss) {
			return r.updateFlowProfileStatus(ctx, profile, flowProfileStatusInput{
				Phase: tamossv1alpha1.FlowProfilePhaseDeleting, Registered: true, DeletionBlocked: true,
				Reason: operatorstatus.ReasonWaitingForSchema, Message: "Profile deletion is waiting for the target schema before checking Flow references",
				ProfileID: resolved.Spec.ID, TamossName: tamoss.Name, Format: resolved.Format, Codec: resolved.Codec,
				RequeueAfter: flowProfileRetryInterval,
			})
		}
		job := flowProfileDeletionJob(profile, tamoss, resolved)
		if err := controllerutil.SetControllerReference(profile, job, r.Scheme); err != nil {
			return ctrl.Result{}, err
		}
		live, exists, err := r.getFlowProfileJob(ctx, job)
		if err != nil {
			return ctrl.Result{}, err
		}
		if !exists {
			if _, err := applyManagedObject(ctx, r.Client, job); err != nil {
				return ctrl.Result{}, err
			}
			return r.updateFlowProfileStatus(ctx, profile, flowProfileStatusInput{
				Phase: tamossv1alpha1.FlowProfilePhaseDeleting, Registered: true, Reason: "DeletionInProgress",
				Message: "TAMS Flow Profile deletion job was launched", ProfileID: resolved.Spec.ID,
				TamossName: tamoss.Name, Format: resolved.Format, Codec: resolved.Codec, RequeueAfter: flowProfileRetryInterval,
			})
		}
		if flowProfileJobDrifted(live, job) {
			if err := r.Client.Delete(ctx, live, client.PropagationPolicy(metav1.DeletePropagationForeground)); err != nil && !apierrors.IsNotFound(err) {
				return ctrl.Result{}, err
			}
			return ctrl.Result{RequeueAfter: flowProfileRetryInterval}, nil
		}
		if jobFailed(live) {
			exitCode, known, err := r.flowProfileJobExitCode(ctx, live)
			if err != nil {
				return ctrl.Result{}, err
			}
			if known && exitCode == flowProfileExitInUse {
				if err := r.Client.Delete(ctx, live, client.PropagationPolicy(metav1.DeletePropagationForeground)); err != nil && !apierrors.IsNotFound(err) {
					return ctrl.Result{}, err
				}
				return r.updateFlowProfileStatus(ctx, profile, flowProfileStatusInput{
					Phase: tamossv1alpha1.FlowProfilePhaseDeleting, Registered: true, DeletionBlocked: true,
					Reason: "ProfileInUse", Message: "Profile deletion is blocked while Flows still reference it",
					ProfileID: resolved.Spec.ID, TamossName: tamoss.Name, Format: resolved.Format, Codec: resolved.Codec,
					RequeueAfter: flowProfileInUseRetryInterval,
				})
			}
			if err := r.Client.Delete(ctx, live, client.PropagationPolicy(metav1.DeletePropagationForeground)); err != nil && !apierrors.IsNotFound(err) {
				return ctrl.Result{}, err
			}
			return ctrl.Result{RequeueAfter: flowProfileRetryInterval}, nil
		}
		if !jobSucceeded(live) {
			return ctrl.Result{RequeueAfter: flowProfileRetryInterval}, nil
		}
	}
	if err := r.deleteFlowProfileChildren(ctx, profile); err != nil {
		return ctrl.Result{}, err
	}
	return r.removeFlowProfileFinalizer(ctx, profile)
}

func (r *FlowProfileReconciler) clearFlowProfileRegistrationState(ctx context.Context, profile *tamossv1alpha1.FlowProfile) error {
	state := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{
		Namespace: profile.Namespace,
		Name:      flowProfileResourceName(profile, "state"),
	}}
	if err := r.Client.Delete(ctx, state); err != nil && !apierrors.IsNotFound(err) {
		return err
	}
	job := &batchv1.Job{ObjectMeta: metav1.ObjectMeta{
		Namespace: profile.Namespace,
		Name:      flowProfileResourceName(profile, "register"),
	}}
	if err := r.Client.Delete(ctx, job, client.PropagationPolicy(metav1.DeletePropagationForeground)); err != nil && !apierrors.IsNotFound(err) {
		return err
	}
	return nil
}

func (r *FlowProfileReconciler) deleteFlowProfileChildren(ctx context.Context, profile *tamossv1alpha1.FlowProfile) error {
	for _, suffix := range []string{"document", "state"} {
		object := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Namespace: profile.Namespace, Name: flowProfileResourceName(profile, suffix)}}
		if err := r.Client.Delete(ctx, object); err != nil && !apierrors.IsNotFound(err) {
			return err
		}
	}
	jobs := &batchv1.JobList{}
	if err := r.Client.List(ctx, jobs, client.InNamespace(profile.Namespace), client.MatchingLabels{"tamoss.livewyer.io/flow-profile": profile.Name}); err != nil {
		return err
	}
	for i := range jobs.Items {
		if err := r.Client.Delete(ctx, &jobs.Items[i], client.PropagationPolicy(metav1.DeletePropagationForeground)); err != nil && !apierrors.IsNotFound(err) {
			return err
		}
	}
	return nil
}

func (r *FlowProfileReconciler) removeFlowProfileFinalizer(ctx context.Context, profile *tamossv1alpha1.FlowProfile) (ctrl.Result, error) {
	original := profile.DeepCopy()
	controllerutil.RemoveFinalizer(profile, flowProfileFinalizer)
	return ctrl.Result{}, r.Client.Patch(ctx, profile, client.MergeFrom(original))
}

func flowProfileReady(profile *tamossv1alpha1.FlowProfile) bool {
	return profile.Status.ObservedGeneration == profile.Generation && meta.IsStatusConditionTrue(profile.Status.Conditions, flowProfileConditionReady)
}
