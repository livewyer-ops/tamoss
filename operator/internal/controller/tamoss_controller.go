package controller

import (
	"context"
	"fmt"
	"net/http"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	tamossv1alpha1 "github.com/livewyer-ops/tamoss/operator/api/v1alpha1"
	"github.com/livewyer-ops/tamoss/operator/internal/controller/auth/authentik"
	"github.com/livewyer-ops/tamoss/operator/internal/controller/defaults"
	"github.com/livewyer-ops/tamoss/operator/internal/controller/workload_renderer"
	operatordiscovery "github.com/livewyer-ops/tamoss/operator/internal/discovery"
	operatormetrics "github.com/livewyer-ops/tamoss/operator/internal/metrics"
	operatorstatus "github.com/livewyer-ops/tamoss/operator/internal/status"
)

const (
	tamossFinalizer = "tamoss.livewyer.io/finalizer"

	// tamossAppName is the default app.kubernetes.io/name label value for
	// resources the operator manages.
	tamossAppName = "tamoss"

	defaultAuthentikProbeInterval         = 30 * time.Second
	defaultDependencyProbeInterval        = 5 * time.Minute
	defaultFinalizerPollInterval          = 2 * time.Second
	defaultProviderReadinessProbeInterval = 10 * time.Second
)

// TamossReconciler reconciles a Tamoss object
type TamossReconciler struct {
	Client                      client.Client
	Scheme                      *runtime.Scheme
	Recorder                    record.EventRecorder
	WatchNamespaces             WatchNamespaceSet
	Discovery                   *operatordiscovery.Manager
	AuthentikPlatformNamespaces *authentik.PlatformNamespacePolicy
	AuthentikProbeInterval      time.Duration
	DependencyProbeInterval     time.Duration
	AuthentikHTTPClient         *http.Client
	WarningEvents               operatorstatus.WarningEventDeduper
	optionalWatches             *optionalWatchRegistrar
}

//+kubebuilder:rbac:groups=tamoss.livewyer.io,resources=tamosses,verbs=get;list;watch;update;patch
//+kubebuilder:rbac:groups=tamoss.livewyer.io,resources=tamosses/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=tamoss.livewyer.io,resources=tamosses/finalizers,verbs=update
//+kubebuilder:rbac:groups=tamoss.livewyer.io,resources=tamosshibernations,verbs=get;list;watch;create;delete
//+kubebuilder:rbac:groups=apps,namespace=system,resources=deployments,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=autoscaling,namespace=system,resources=horizontalpodautoscalers,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=batch,namespace=system,resources=jobs,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="",namespace=system,resources=configmaps;events;pods;secrets;serviceaccounts;services,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=networking.k8s.io,namespace=system,resources=ingresses;networkpolicies,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=policy,namespace=system,resources=poddisruptionbudgets,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=gateway.networking.k8s.io,namespace=system,resources=httproutes,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=postgresql.cnpg.io,namespace=system,resources=clusters,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=postgresql.cnpg.io,namespace=system,resources=scheduledbackups,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=rustfs.com,resources=tenants,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=traefik.io,namespace=system,resources=middlewares,verbs=get;list;watch;create;update;patch;delete

// Reconcile drives a Tamoss resource toward its resolved workload, backend,
// identity, routing, schema, and status state.
func (r *TamossReconciler) Reconcile(ctx context.Context, req ctrl.Request) (result ctrl.Result, err error) {
	start := time.Now()
	phaseRecorded := false
	var tamoss *tamossv1alpha1.Tamoss
	recordPhase := func(phase string) {
		operatormetrics.RecordReconcilePhase(phase)
		phaseRecorded = true
	}
	defer func() {
		recordControllerReconcile(tamossAppName, result, err, time.Since(start))
	}()
	defer func() {
		if err != nil && tamoss != nil && tamoss.Spec.HTTPRoute.Enabled && isKubernetesNoMatchError(err) {
			if statusErr := r.updateGatewayAPIUnavailableStatus(ctx, tamoss); statusErr != nil {
				err = statusErr
			} else {
				recordPhase(operatorstatus.PhaseProgressing)
				result = ctrl.Result{RequeueAfter: defaultAuthentikProbeInterval}
				err = nil
				return
			}
		}
		if err != nil && !phaseRecorded {
			recordPhase(operatorstatus.PhaseError)
		}
	}()

	var found bool
	tamoss, found, err = r.loadTamoss(ctx, req.NamespacedName)
	if err != nil || !found {
		return ctrl.Result{}, err
	}
	tamoss, control, err := r.prepareTamossLifecycle(ctx, tamoss, recordPhase)
	if shouldStop(control, err) {
		return control.Result, err
	}

	desiredKeys := map[string]struct{}{}
	if control, err := r.reconcileHTTPRouteInputGate(ctx, tamoss, recordPhase); shouldStop(control, err) {
		return control.Result, err
	}
	if control, err := r.reconcileTamossBackendStages(ctx, tamoss, desiredKeys, recordPhase); shouldStop(control, err) {
		return control.Result, err
	}
	if control, err := r.reconcileTamossIdentityGates(ctx, tamoss, recordPhase); shouldStop(control, err) {
		return control.Result, err
	}
	schemaResult, control, err := r.reconcileTamossSchemaStage(ctx, tamoss, recordPhase)
	if shouldStop(control, err) {
		return control.Result, err
	}
	if control, err := r.reconcileTamossManagedAuthentikStage(ctx, tamoss, desiredKeys, recordPhase); shouldStop(control, err) {
		return control.Result, err
	}
	if err := r.applyTamossDesiredObjects(ctx, tamoss, schemaResult, desiredKeys); err != nil {
		return ctrl.Result{}, err
	}

	if err := r.pruneOwnedObjects(ctx, tamoss, desiredKeys); err != nil {
		return ctrl.Result{}, err
	}
	identityResult := r.identityResult(ctx, tamoss)
	if err := r.updateStatus(ctx, tamoss, schemaResult, identityResult); err != nil {
		return ctrl.Result{}, err
	}
	if err := r.emitImagePullEvents(ctx, tamoss); err != nil {
		return ctrl.Result{}, err
	}
	recordPhase(tamoss.Status.Phase)

	return tamossCompletionResult(tamoss, r.authentikProbeInterval(), r.dependencyProbeInterval()), nil
}

func (r *TamossReconciler) loadTamoss(ctx context.Context, key client.ObjectKey) (*tamossv1alpha1.Tamoss, bool, error) {
	tamoss := &tamossv1alpha1.Tamoss{}
	if err := r.Client.Get(ctx, key, tamoss); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, false, nil
		}
		return nil, false, err
	}
	return tamoss, true, nil
}

func (r *TamossReconciler) prepareTamossLifecycle(ctx context.Context, tamoss *tamossv1alpha1.Tamoss, recordPhase func(string)) (*tamossv1alpha1.Tamoss, reconcileControl, error) {
	if !r.WatchNamespaces.Allows(tamoss.Namespace) {
		log.FromContext(ctx).Info("ignoring Tamoss outside configured watch scope", "namespace", tamoss.Namespace)
		recordPhase(operatorstatus.PhaseIgnored)
		return nil, stopReconcile(ctrl.Result{}), nil
	}
	if !tamoss.DeletionTimestamp.IsZero() {
		recordPhase(operatorstatus.PhaseFinalizing)
		result, err := r.finalizeTamoss(ctx, tamoss)
		return nil, stopReconcile(result), err
	}
	original := tamoss.DeepCopy()
	if controllerutil.AddFinalizer(tamoss, tamossFinalizer) {
		if err := r.Client.Patch(ctx, tamoss, client.MergeFrom(original)); err != nil {
			return nil, stopReconcile(ctrl.Result{}), err
		}
	}
	resolved := tamoss.DeepCopy()
	defaults.Apply(resolved)
	if err := r.reconcileHibernationSpec(ctx, resolved); err != nil {
		return nil, stopReconcile(ctrl.Result{}), err
	}
	if tamossLifecycleBlocksReconcile(resolved) {
		if err := r.updateLifecycleGatedStatus(ctx, resolved); err != nil {
			return nil, stopReconcile(ctrl.Result{}), err
		}
		recordPhase(resolved.Status.Phase)
		return nil, stopReconcile(ctrl.Result{}), nil
	}
	if resolved.Spec.Paused {
		if err := r.updatePausedStatus(ctx, resolved); err != nil {
			return nil, stopReconcile(ctrl.Result{}), err
		}
		recordPhase(resolved.Status.Phase)
		return nil, stopReconcile(ctrl.Result{}), nil
	}
	return resolved, continueReconcile(), nil
}

func tamossLifecycleBlocksReconcile(tamoss *tamossv1alpha1.Tamoss) bool {
	// Declared hibernation gates reconciliation regardless of how far the
	// materialised operation has progressed, so a failed cycle cannot
	// implicitly restore a supposedly hibernated instance.
	if tamoss.Spec.Hibernation.Enabled {
		return true
	}
	switch tamossv1alpha1.TamossLifecyclePhase(tamoss.Status.Lifecycle.Phase) {
	case tamossv1alpha1.TamossLifecyclePhaseHibernating,
		tamossv1alpha1.TamossLifecyclePhaseHibernated,
		tamossv1alpha1.TamossLifecyclePhaseResuming:
		return true
	default:
		return false
	}
}

func (r *TamossReconciler) reconcileTamossBackendStages(ctx context.Context, tamoss *tamossv1alpha1.Tamoss, desiredKeys map[string]struct{}, recordPhase func(string)) (reconcileControl, error) {
	dependencyGate := r.backendDependencyGate(ctx, tamoss)
	if !dependencyGate.Allowed {
		if !dependencyGate.Known {
			return stopReconcile(ctrl.Result{RequeueAfter: r.dependencyProbeInterval()}), nil
		}
		if err := r.updateBlockedBackendStatus(ctx, tamoss, backendBlockedStatusInput{
			Reason:             dependencyGate.Reason,
			Message:            dependencyGate.Message,
			SchemaMessage:      "Schema reconciliation is blocked by missing backend dependencies",
			ProgressingMessage: "Reconciliation is blocked by missing backend dependencies",
		}); err != nil {
			return stopReconcile(ctrl.Result{}), err
		}
		recordPhase(tamoss.Status.Phase)
		return stopReconcile(ctrl.Result{RequeueAfter: r.dependencyProbeInterval()}), nil
	}
	providerBackendResult, err := r.reconcileProviderBackends(ctx, tamoss, desiredKeys)
	if err != nil {
		return stopReconcile(ctrl.Result{}), err
	}
	if !providerBackendResult.Ready {
		if err := r.updateProviderBackendStatus(ctx, tamoss, nil, providerBackendResult); err != nil {
			return stopReconcile(ctrl.Result{}), err
		}
		recordPhase(tamoss.Status.Phase)
		return stopReconcile(ctrl.Result{RequeueAfter: defaultProviderReadinessProbeInterval}), nil
	}
	backendResult, err := r.checkBackendReferences(ctx, tamoss)
	if err != nil {
		return stopReconcile(ctrl.Result{}), err
	}
	if !backendResult.Ready {
		if err := r.updateBlockedBackendStatus(ctx, tamoss, backendBlockedStatusInput{
			Reason:             backendResult.Reason,
			Message:            backendResult.Message,
			SchemaMessage:      "Schema reconciliation is blocked by backend reference errors",
			ProgressingMessage: "Reconciliation is blocked by backend reference errors",
		}); err != nil {
			return stopReconcile(ctrl.Result{}), err
		}
		recordPhase(tamoss.Status.Phase)
		return stopReconcile(ctrl.Result{RequeueAfter: defaultProviderReadinessProbeInterval}), nil
	}
	return continueReconcile(), nil
}

func (r *TamossReconciler) reconcileTamossIdentityGates(ctx context.Context, tamoss *tamossv1alpha1.Tamoss, recordPhase func(string)) (reconcileControl, error) {
	if r.AuthentikPlatformNamespaces == nil {
		r.AuthentikPlatformNamespaces = authentik.NewPlatformNamespacePolicy("")
	}
	if decision := authentik.CheckPlatformNamespace(tamoss, r.AuthentikPlatformNamespaces); !decision.Allowed {
		if err := r.updateIdentityBlockedStatus(ctx, tamoss, operatorstatus.ReasonPlatformNamespaceNotAllowed, decision.Message); err != nil {
			return stopReconcile(ctrl.Result{}), err
		}
		recordPhase(tamoss.Status.Phase)
		return stopReconcile(ctrl.Result{RequeueAfter: defaultAuthentikProbeInterval}), nil
	}
	if identityConfig := externalIdentityConfiguration(tamoss); !identityConfig.Ready {
		if err := r.updateIdentityBlockedStatus(ctx, tamoss, identityConfig.Reason, identityConfig.Message); err != nil {
			return stopReconcile(ctrl.Result{}), err
		}
		recordPhase(tamoss.Status.Phase)
		return stopReconcile(ctrl.Result{}), nil
	}
	return continueReconcile(), nil
}

func (r *TamossReconciler) reconcileTamossSchemaStage(ctx context.Context, tamoss *tamossv1alpha1.Tamoss, recordPhase func(string)) (SchemaResult, reconcileControl, error) {
	schemaResult, err := (&SchemaController{Client: r.Client, Scheme: r.Scheme}).Reconcile(ctx, tamoss)
	if err != nil {
		return SchemaResult{}, stopReconcile(ctrl.Result{}), err
	}
	r.recordRecoveryEvent(tamoss, schemaResult.RecoveryEvent)
	if schemaResult.Ready {
		storageBackendResult, err := r.checkDefaultStorageBackendReady(ctx, tamoss)
		if err != nil {
			return SchemaResult{}, stopReconcile(ctrl.Result{}), err
		}
		if !storageBackendResult.Ready {
			if err := r.updateProviderBackendStatus(ctx, tamoss, &schemaResult, storageBackendResult); err != nil {
				return SchemaResult{}, stopReconcile(ctrl.Result{}), err
			}
			recordPhase(tamoss.Status.Phase)
			return SchemaResult{}, stopReconcile(ctrl.Result{RequeueAfter: defaultProviderReadinessProbeInterval}), nil
		}
	}
	return schemaResult, continueReconcile(), nil
}

func (r *TamossReconciler) reconcileTamossManagedAuthentikStage(ctx context.Context, tamoss *tamossv1alpha1.Tamoss, desiredKeys map[string]struct{}, recordPhase func(string)) (reconcileControl, error) {
	if tamoss.Spec.Auth.Provider() != tamossv1alpha1.AuthProvidedByAuthentikBlueprints {
		return continueReconcile(), nil
	}
	redirectURIs := authentik.RedirectURIs(tamoss)
	if len(redirectURIs) == 0 {
		message := "auth.authentikBlueprints.redirectURIs is empty and no UI ingress or HTTPRoute hostnames are configured"
		if err := r.updateIdentityBlockedStatus(ctx, tamoss, operatorstatus.ReasonNoRedirectURIDerivable, message); err != nil {
			return stopReconcile(ctrl.Result{}), err
		}
		recordPhase(tamoss.Status.Phase)
		return stopReconcile(ctrl.Result{RequeueAfter: r.authentikProbeInterval()}), nil
	}
	secretName, credentials, err := (authentik.CredsManager{Client: r.Client}).Ensure(ctx, tamoss)
	if err != nil {
		return stopReconcile(ctrl.Result{}), err
	}
	desiredKeys[canonicalObjectKey(&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: secretName, Namespace: tamoss.Namespace}})] = struct{}{}
	blueprint, err := authentik.RenderBlueprint(tamoss, credentials)
	if err != nil {
		return stopReconcile(ctrl.Result{}), err
	}
	token, err := authentik.ResolveAPIToken(ctx, r.Client, tamoss)
	if err != nil {
		return stopReconcile(ctrl.Result{}), err
	}
	if token.Token == "" {
		if err := r.updateIdentityBlockedStatus(ctx, tamoss, operatorstatus.ReasonAuthentikAPITokenMissing, token.Message); err != nil {
			return stopReconcile(ctrl.Result{}), err
		}
		recordPhase(tamoss.Status.Phase)
		return stopReconcile(ctrl.Result{RequeueAfter: r.authentikProbeInterval()}), nil
	}
	managedBlueprint, err := authentik.NewManagedBlueprintClient(tamoss, token.Token, r.AuthentikHTTPClient).Reconcile(ctx, authentik.ManagedBlueprintName(tamoss), authentik.ManagedBlueprintPath(tamoss), blueprint)
	if err != nil {
		message := fmt.Sprintf("Authentik managed Blueprint apply failed for %s: %v", authentik.ManagedBlueprintName(tamoss), err)
		if err := r.updateIdentityBlockedStatus(ctx, tamoss, operatorstatus.ReasonAuthentikManagedBlueprintApplyFailed, message); err != nil {
			return stopReconcile(ctrl.Result{}), err
		}
		recordPhase(tamoss.Status.Phase)
		return stopReconcile(ctrl.Result{RequeueAfter: r.authentikProbeInterval()}), nil
	}
	log.FromContext(ctx).V(1).Info("applied Authentik managed Blueprint", "name", managedBlueprint.Name, "status", managedBlueprint.Status)
	if err := authentik.NewProxyOutpostClient(tamoss, token.Token, r.AuthentikHTTPClient).Reconcile(ctx, tamoss); err != nil {
		message := fmt.Sprintf("Authentik proxy outpost apply failed for %s: %v", authentik.ProxyProviderName(tamoss), err)
		if err := r.updateIdentityBlockedStatus(ctx, tamoss, operatorstatus.ReasonAuthentikManagedBlueprintApplyFailed, message); err != nil {
			return stopReconcile(ctrl.Result{}), err
		}
		recordPhase(tamoss.Status.Phase)
		return stopReconcile(ctrl.Result{RequeueAfter: r.authentikProbeInterval()}), nil
	}
	return continueReconcile(), nil
}

func (r *TamossReconciler) applyTamossDesiredObjects(ctx context.Context, tamoss *tamossv1alpha1.Tamoss, schemaResult SchemaResult, desiredKeys map[string]struct{}) error {
	runtimeCredentialsSecret, err := storageBackendRuntimeCredentialsSecret(ctx, r.Client, tamoss)
	if err != nil {
		return err
	}
	desiredObjects := workload_renderer.Render(tamoss)
	if runtimeCredentialsSecret != nil {
		desiredObjects = append([]client.Object{runtimeCredentialsSecret}, desiredObjects...)
	}
	desiredObjects = gateSchemaDependentWorkloads(desiredObjects, schemaResult.Ready)
	if err := r.prepareAPITokenSecret(ctx, tamoss, desiredObjects); err != nil {
		return err
	}
	for _, desired := range schemaResult.ManagedObjects {
		desiredKeys[canonicalObjectKey(desired)] = struct{}{}
	}
	for _, desired := range desiredObjects {
		if err := controllerutil.SetControllerReference(tamoss, desired, r.Scheme); err != nil {
			return err
		}
		if err := applyAdvancedResourcePatches(tamoss, desired); err != nil {
			return err
		}
		desiredKeys[canonicalObjectKey(desired)] = struct{}{}
		result, err := applyManagedObject(ctx, r.Client, desired)
		if err != nil {
			return err
		}
		if result.Changed {
			log.FromContext(ctx).V(1).Info("applied managed object", "kind", canonicalObjectKind(desired), "name", desired.GetName(), "namespace", desired.GetNamespace())
		}
		if result.Changed && !result.Created {
			r.recordDriftCorrected(tamoss, desired)
		}
	}
	return nil
}

func tamossCompletionResult(tamoss *tamossv1alpha1.Tamoss, authentikProbeInterval, dependencyProbeInterval time.Duration) ctrl.Result {
	requeueAfter := time.Duration(0)
	if tamoss.Spec.Auth.Provider() == tamossv1alpha1.AuthProvidedByAuthentikBlueprints {
		requeueAfter = shortestPositiveDuration(requeueAfter, authentikProbeInterval)
	}
	if tamossUsesProviderManagedBackends(tamoss) {
		requeueAfter = shortestPositiveDuration(requeueAfter, dependencyProbeInterval)
	}
	if requeueAfter <= 0 {
		return ctrl.Result{}
	}
	return ctrl.Result{RequeueAfter: requeueAfter}
}

func shortestPositiveDuration(current, candidate time.Duration) time.Duration {
	if candidate <= 0 {
		return current
	}
	if current <= 0 || candidate < current {
		return candidate
	}
	return current
}

func (r *TamossReconciler) recordDriftCorrected(tamoss *tamossv1alpha1.Tamoss, obj client.Object) {
	if r.Recorder == nil || !tamossObservedReady(tamoss) {
		return
	}
	r.Recorder.Eventf(
		tamoss,
		corev1.EventTypeNormal,
		operatorstatus.ReasonDriftCorrected,
		"Corrected drift on %s/%s",
		canonicalObjectKind(obj),
		obj.GetName(),
	)
}

func (r *TamossReconciler) recordWarning(tamoss *tamossv1alpha1.Tamoss, reason, message string) {
	operatorstatus.EmitWarningEvent(r.Recorder, &r.WarningEvents, tamoss, reason, message)
}

func (r *TamossReconciler) recordRecoveryEvent(tamoss *tamossv1alpha1.Tamoss, event *recoveryActionEvent) {
	if event == nil {
		return
	}
	switch event.Type {
	case corev1.EventTypeNormal:
		operatorstatus.EmitNormalEvent(r.Recorder, tamoss, event.Reason, event.Message)
	case corev1.EventTypeWarning:
		operatorstatus.EmitWarningEvent(r.Recorder, &r.WarningEvents, tamoss, event.Reason, event.Message)
	}
}

func gateSchemaDependentWorkloads(objects []client.Object, schemaReady bool) []client.Object {
	if schemaReady {
		return objects
	}
	filtered := make([]client.Object, 0, len(objects))
	for _, obj := range objects {
		deployment, ok := obj.(*appsv1.Deployment)
		if !ok {
			filtered = append(filtered, obj)
			continue
		}
		component := deployment.Labels["app.kubernetes.io/component"]
		if component == "api" || component == "worker" {
			continue
		}
		filtered = append(filtered, obj)
	}
	return filtered
}

func readyReason(ready bool, schemaResult SchemaResult, identityResult identityReconcileResult, routingResult routingStatusResult) string {
	if ready {
		return operatorstatus.ReasonAllComponentsReady
	}
	if schemaResult.Degraded {
		return schemaResult.Reason
	}
	if !schemaResult.Ready {
		return operatorstatus.ReasonWaitingForSchema
	}
	if !identityResult.Ready {
		return identityResult.Reason
	}
	if !routingResult.Ready {
		return routingResult.Reason
	}
	return operatorstatus.ReasonComponentsProgressing
}

func readyMessage(ready bool, schemaResult SchemaResult, identityResult identityReconcileResult, routingResult routingStatusResult) string {
	if ready {
		return "All enabled workloads are available"
	}
	if schemaResult.Degraded {
		return schemaResult.Message
	}
	if !schemaResult.Ready {
		return "Waiting for schema migration to complete"
	}
	if !identityResult.Ready {
		return identityResult.Message
	}
	if !routingResult.Ready {
		return routingResult.Message
	}
	return "One or more enabled workloads are not yet available"
}

func degradedReason(schemaResult SchemaResult) string {
	if schemaResult.Degraded {
		return schemaResult.Reason
	}
	return operatorstatus.ReasonNoError
}

func degradedMessage(schemaResult SchemaResult) string {
	if schemaResult.Degraded {
		return schemaResult.Message
	}
	return "No terminal reconcile error has been observed"
}

func schemaReason(schemaResult SchemaResult) string {
	if schemaResult.Reason != "" {
		return schemaResult.Reason
	}
	return "SchemaControllerReady"
}

func schemaMessage(schemaResult SchemaResult) string {
	if schemaResult.Message != "" {
		return schemaResult.Message
	}
	return "Schema controller completed for this reconcile"
}

// canonicalKindScheme resolves GroupVersionKinds for every type the operator
// manages. It mirrors the registrations performed for the manager scheme in
// cmd/main.go so that kind resolution never depends on hand-maintained lists.
var canonicalKindScheme = newCanonicalKindScheme()

func newCanonicalKindScheme() *runtime.Scheme {
	kindScheme := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(kindScheme))
	utilruntime.Must(tamossv1alpha1.AddToScheme(kindScheme))
	utilruntime.Must(cnpgv1.AddToScheme(kindScheme))
	utilruntime.Must(gatewayv1.Install(kindScheme))
	return kindScheme
}

func canonicalObjectKey(obj client.Object) string {
	return fmt.Sprintf("%s/%s/%s", canonicalObjectKind(obj), obj.GetNamespace(), obj.GetName())
}

func canonicalObjectKind(obj client.Object) string {
	return canonicalObjectGVK(obj).Kind
}

// canonicalObjectGVK resolves the GVK from the scheme rather than a
// hand-maintained switch. An unresolvable type is a programming error: a
// silent empty kind would corrupt desired-object keys and make the pruner
// delete still-desired objects, so fail loudly instead.
func canonicalObjectGVK(obj client.Object) schema.GroupVersionKind {
	gvk, err := apiutil.GVKForObject(obj, canonicalKindScheme)
	if err != nil {
		panic(fmt.Errorf("resolve GVK for %T: %w", obj, err))
	}
	return gvk
}

func tamossResourceName(tamoss *tamossv1alpha1.Tamoss, suffix string) string {
	return tamoss.ResourceName(suffix)
}

func gvrString(gvr schema.GroupVersionResource) string {
	if gvr.Group == "" {
		return fmt.Sprintf("%s/%s", gvr.Version, gvr.Resource)
	}
	return fmt.Sprintf("%s/%s/%s", gvr.Group, gvr.Version, gvr.Resource)
}
