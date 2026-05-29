package controller

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	tamossv1alpha1 "github.com/livewyer-ops/tamoss/operator/api/v1alpha1"
	"github.com/livewyer-ops/tamoss/operator/internal/controller/backend/rustfs"
	"github.com/livewyer-ops/tamoss/operator/internal/controller/defaults"
	operatorstatus "github.com/livewyer-ops/tamoss/operator/internal/status"
)

const (
	storageBackendFinalizer = "storagebackend.tamoss.livewyer.io/finalizer"

	storageBackendDesiredHashAnnotation = "tamoss.livewyer.io/desired-hash"

	storageBackendStateReadyKey      = "ready"
	storageBackendStateBackendIDKey  = "backendID"
	storageBackendStateBucketKey     = "bucketName"
	storageBackendStateEndpointKey   = "endpointURL"
	storageBackendStateDesiredHash   = "desiredHash"
	storageBackendStateJobUIDKey     = "jobUID"
	storageBackendStateUpdatedAtKey  = "lastUpdatedAt"
	storageBackendStateGenerationKey = "observedGeneration"
)

type StorageBackendReconciler struct {
	Client          client.Client
	Scheme          *runtime.Scheme
	Recorder        record.EventRecorder
	WatchNamespaces map[string]struct{}
	WarningEvents   operatorstatus.WarningEventDeduper
	HTTPClient      *http.Client
	BucketClient    rustfs.BucketClient
}

//+kubebuilder:rbac:groups=tamoss.livewyer.io,resources=storagebackends,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=tamoss.livewyer.io,resources=storagebackends/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=tamoss.livewyer.io,resources=storagebackends/finalizers,verbs=update
//+kubebuilder:rbac:groups=tamoss.livewyer.io,resources=tamosses,verbs=get;list;watch
//+kubebuilder:rbac:groups=batch,namespace=system,resources=jobs,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="",namespace=system,resources=configmaps;events;secrets,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="",namespace=system,resources=pods,verbs=list;delete

func (r *StorageBackendReconciler) Reconcile(ctx context.Context, req ctrl.Request) (result ctrl.Result, err error) {
	start := time.Now()
	defer func() {
		recordControllerReconcile("storagebackend", result, err, time.Since(start))
	}()
	storageBackend, found, err := r.loadStorageBackend(ctx, req.NamespacedName)
	if err != nil || !found {
		return ctrl.Result{}, err
	}
	if control, err := r.prepareStorageBackendLifecycle(ctx, storageBackend); shouldStop(control, err) {
		return control.Result, err
	}
	spec, control, err := r.resolveStorageBackendSpec(ctx, storageBackend)
	if shouldStop(control, err) {
		return control.Result, err
	}
	tamoss, control, err := r.loadStorageBackendTamossStage(ctx, storageBackend, spec)
	if shouldStop(control, err) {
		return control.Result, err
	}
	if control, err := r.reconcileStorageBackendCredentialsStage(ctx, storageBackend, tamoss, spec); shouldStop(control, err) {
		return control.Result, err
	}
	if control, err := r.reconcileStorageBackendBucketStage(ctx, storageBackend, tamoss, spec); shouldStop(control, err) {
		return control.Result, err
	}
	if control, err := r.reconcileStorageBackendDatabaseStage(ctx, storageBackend, tamoss, spec); shouldStop(control, err) {
		return control.Result, err
	}
	if err := r.reconcileRuntimeCredentialsSecret(ctx, tamoss); err != nil {
		return ctrl.Result{}, err
	}
	diagnostic := r.externalS3Diagnostic(ctx, tamoss, spec)

	return r.updateStorageBackendStatus(ctx, storageBackend, storageBackendStatusInput{
		Ready:         true,
		BucketReady:   true,
		DatabaseReady: true,
		Reason:        operatorstatus.ReasonStorageBackendReady,
		Message:       "StorageBackend bucket and database registration are ready",
		BackendID:     spec.ID,
		BucketName:    spec.BucketName,
		Diagnostic:    diagnostic,
	})
}

func (r *StorageBackendReconciler) loadStorageBackend(ctx context.Context, key client.ObjectKey) (*tamossv1alpha1.StorageBackend, bool, error) {
	storageBackend := &tamossv1alpha1.StorageBackend{}
	if err := r.Client.Get(ctx, key, storageBackend); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, false, nil
		}
		return nil, false, err
	}
	return storageBackend, true, nil
}

func (r *StorageBackendReconciler) prepareStorageBackendLifecycle(ctx context.Context, storageBackend *tamossv1alpha1.StorageBackend) (reconcileControl, error) {
	if !r.namespaceAllowed(storageBackend.Namespace) {
		log.FromContext(ctx).Info("ignoring StorageBackend outside configured watch scope", "namespace", storageBackend.Namespace)
		return stopReconcileNow(), nil
	}
	if !storageBackend.ObjectMeta.DeletionTimestamp.IsZero() {
		result, err := r.finalizeStorageBackend(ctx, storageBackend)
		return stopReconcile(result), err
	}
	original := storageBackend.DeepCopy()
	if controllerutil.AddFinalizer(storageBackend, storageBackendFinalizer) {
		if err := r.Client.Patch(ctx, storageBackend, client.MergeFrom(original)); err != nil {
			return stopReconcileNow(), err
		}
		return stopReconcile(ctrl.Result{Requeue: true}), nil
	}
	return continueReconcile(), nil
}

func (r *StorageBackendReconciler) resolveStorageBackendSpec(ctx context.Context, storageBackend *tamossv1alpha1.StorageBackend) (tamossv1alpha1.StorageBackendSpec, reconcileControl, error) {
	spec := storageBackend.Spec
	spec.ApplyDefaults(storageBackend.Namespace, storageBackend.Name)
	if spec.Provider != tamossv1alpha1.StorageBackendProviderRustFS && !spec.IsExternalObjectStore() {
		result, err := r.updateStorageBackendStatus(ctx, storageBackend, storageBackendStatusInput{
			Ready:         false,
			BucketReady:   false,
			DatabaseReady: false,
			Reason:        operatorstatus.ReasonUnsupportedProvider,
			Message:       fmt.Sprintf("StorageBackend provider %q is not supported", spec.Provider),
			Degraded:      true,
			BackendID:     spec.ID,
			BucketName:    spec.BucketName,
		})
		return tamossv1alpha1.StorageBackendSpec{}, stopReconcile(result), err
	}
	if missing := missingStorageBackendFields(spec); len(missing) > 0 {
		result, err := r.updateStorageBackendStatus(ctx, storageBackend, storageBackendStatusInput{
			Ready:         false,
			BucketReady:   false,
			DatabaseReady: false,
			Reason:        operatorstatus.ReasonMissingProviderConfiguration,
			Message:       fmt.Sprintf("Required StorageBackend fields are missing: %s", strings.Join(missing, ", ")),
			Degraded:      true,
			BackendID:     spec.ID,
			BucketName:    spec.BucketName,
		})
		return tamossv1alpha1.StorageBackendSpec{}, stopReconcile(result), err
	}
	return spec, continueReconcile(), nil
}

func (r *StorageBackendReconciler) loadStorageBackendTamossStage(ctx context.Context, storageBackend *tamossv1alpha1.StorageBackend, spec tamossv1alpha1.StorageBackendSpec) (*tamossv1alpha1.Tamoss, reconcileControl, error) {
	tamoss := &tamossv1alpha1.Tamoss{}
	tamossKey := types.NamespacedName{Name: spec.TamossRef.Name, Namespace: storageBackend.Namespace}
	if err := r.Client.Get(ctx, tamossKey, tamoss); err != nil {
		if apierrors.IsNotFound(err) {
			result, statusErr := r.updateStorageBackendStatus(ctx, storageBackend, storageBackendStatusInput{
				Ready:         false,
				BucketReady:   false,
				DatabaseReady: false,
				Reason:        operatorstatus.ReasonTamossNotFound,
				Message:       fmt.Sprintf("Referenced Tamoss %s was not found", tamossKey.Name),
				BackendID:     spec.ID,
				BucketName:    spec.BucketName,
			})
			return nil, stopReconcile(result), statusErr
		}
		return nil, stopReconcileNow(), err
	}
	resolvedTamoss := tamoss.DeepCopy()
	defaults.Apply(resolvedTamoss)
	return resolvedTamoss, continueReconcile(), nil
}

func (r *StorageBackendReconciler) reconcileStorageBackendCredentialsStage(ctx context.Context, storageBackend *tamossv1alpha1.StorageBackend, tamoss *tamossv1alpha1.Tamoss, spec tamossv1alpha1.StorageBackendSpec) (reconcileControl, error) {
	secretReady, reason, message, err := r.storageBackendCredentials(ctx, storageBackend.Namespace, spec)
	if err != nil {
		return stopReconcileNow(), err
	}
	if secretReady {
		return continueReconcile(), nil
	}
	if err := r.reconcileRuntimeCredentialsSecret(ctx, tamoss); err != nil {
		return stopReconcileNow(), err
	}
	result, err := r.updateStorageBackendStatus(ctx, storageBackend, storageBackendStatusInput{
		Ready:         false,
		BucketReady:   false,
		DatabaseReady: false,
		Reason:        reason,
		Message:       message,
		BackendID:     spec.ID,
		BucketName:    spec.BucketName,
	})
	return stopReconcile(result), err
}

func (r *StorageBackendReconciler) reconcileStorageBackendBucketStage(ctx context.Context, storageBackend *tamossv1alpha1.StorageBackend, tamoss *tamossv1alpha1.Tamoss, spec tamossv1alpha1.StorageBackendSpec) (reconcileControl, error) {
	bucketResult, err := r.reconcileStorageBackendBucket(ctx, storageBackend, tamoss, spec)
	if err != nil {
		return stopReconcileNow(), err
	}
	if bucketResult.Ready {
		return continueReconcile(), nil
	}
	result, err := r.updateStorageBackendStatus(ctx, storageBackend, storageBackendStatusInput{
		Ready:         false,
		BucketReady:   false,
		DatabaseReady: false,
		Reason:        bucketResult.Reason,
		Message:       bucketResult.Message,
		Degraded:      bucketResult.Degraded,
		BackendID:     spec.ID,
		BucketName:    spec.BucketName,
	})
	return stopReconcile(result), err
}

func (r *StorageBackendReconciler) reconcileStorageBackendDatabaseStage(ctx context.Context, storageBackend *tamossv1alpha1.StorageBackend, tamoss *tamossv1alpha1.Tamoss, spec tamossv1alpha1.StorageBackendSpec) (reconcileControl, error) {
	if !r.schemaStateReady(ctx, tamoss) {
		result, err := r.updateStorageBackendStatus(ctx, storageBackend, storageBackendStatusInput{
			Ready:         false,
			BucketReady:   true,
			DatabaseReady: false,
			Reason:        operatorstatus.ReasonSchemaNotReady,
			Message:       fmt.Sprintf("Tamoss %s schema state is not ready", tamoss.Name),
			BackendID:     spec.ID,
			BucketName:    spec.BucketName,
		})
		return stopReconcile(result), err
	}
	dbResult, err := r.reconcileStorageBackendDatabase(ctx, storageBackend, tamoss, spec)
	if err != nil {
		return stopReconcileNow(), err
	}
	if dbResult.Ready {
		return continueReconcile(), nil
	}
	result, err := r.updateStorageBackendStatus(ctx, storageBackend, storageBackendStatusInput{
		Ready:         false,
		BucketReady:   true,
		DatabaseReady: false,
		Reason:        dbResult.Reason,
		Message:       dbResult.Message,
		Degraded:      dbResult.Degraded,
		BackendID:     spec.ID,
		BucketName:    spec.BucketName,
	})
	return stopReconcile(result), err
}

func (r *StorageBackendReconciler) namespaceAllowed(namespace string) bool {
	if len(r.WatchNamespaces) == 0 {
		return true
	}
	_, ok := r.WatchNamespaces[namespace]
	return ok
}
