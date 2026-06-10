package controller

import (
	"context"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	tamossv1alpha1 "github.com/livewyer-ops/tamoss/operator/api/v1alpha1"
	"github.com/livewyer-ops/tamoss/operator/internal/controller/auth/authentik"
	operatorstatus "github.com/livewyer-ops/tamoss/operator/internal/status"
)

func (r *TamossReconciler) pruneOwnedObjects(ctx context.Context, tamoss *tamossv1alpha1.Tamoss, desired map[string]struct{}) error {
	match := tamossManagedLabelSelector(tamoss)
	listOptions := []client.ListOption{client.InNamespace(tamoss.Namespace), match}

	for _, policy := range tamossManagedResourcePolicies() {
		if err := pruneTamossOwnedList(ctx, r.Client, tamoss, desired, policy.list); err != nil {
			return err
		}
	}
	middlewares := &unstructured.UnstructuredList{}
	middlewares.SetGroupVersionKind(schema.GroupVersionKind{Group: "traefik.io", Version: "v1alpha1", Kind: "MiddlewareList"})
	if err := pruneList(ctx, r.Client, tamoss, desired, middlewares, listOptions...); err != nil && !meta.IsNoMatchError(err) {
		return err
	}
	if err := r.pruneOptionalOwnedObjects(ctx, tamoss, desired, listOptions...); err != nil {
		return err
	}
	return nil
}

func (r *TamossReconciler) finalizeTamoss(ctx context.Context, tamoss *tamossv1alpha1.Tamoss) (ctrl.Result, error) {
	if !controllerutil.ContainsFinalizer(tamoss, tamossFinalizer) {
		return ctrl.Result{}, nil
	}
	if tamoss.Spec.Auth.Provider() == tamossv1alpha1.AuthProvidedByAuthentikBlueprints {
		if err := r.deleteAuthentikProxyOutpost(ctx, tamoss); err != nil {
			message := fmt.Sprintf("Authentik proxy outpost cleanup skipped for %s: %v", authentik.ProxyProviderName(tamoss), err)
			log.FromContext(ctx).Error(err, "failed to delete Authentik proxy outpost; continuing finalization", "proxyProvider", authentik.ProxyProviderName(tamoss))
			r.recordWarning(tamoss, operatorstatus.ReasonAuthentikManagedBlueprintDeleteFailed, message)
		}
		if err := r.deleteAuthentikManagedBlueprint(ctx, tamoss); err != nil {
			message := fmt.Sprintf("Authentik managed Blueprint cleanup skipped for %s: %v", authentik.ManagedBlueprintName(tamoss), err)
			log.FromContext(ctx).Error(err, "failed to delete Authentik managed Blueprint; continuing finalization", "managedBlueprint", authentik.ManagedBlueprintName(tamoss))
			r.recordWarning(tamoss, operatorstatus.ReasonAuthentikManagedBlueprintDeleteFailed, message)
		}
	}
	storageBackendsRemain, err := r.deleteOwnedStorageBackends(ctx, tamoss)
	if err != nil {
		return ctrl.Result{}, err
	}
	if storageBackendsRemain {
		return ctrl.Result{RequeueAfter: defaultFinalizerPollInterval}, nil
	}
	if err := r.pruneOwnedObjects(ctx, tamoss, map[string]struct{}{}); err != nil {
		return ctrl.Result{}, err
	}
	remaining, err := r.ownedObjectsRemain(ctx, tamoss)
	if err != nil {
		return ctrl.Result{}, err
	}
	if remaining {
		return ctrl.Result{RequeueAfter: defaultFinalizerPollInterval}, nil
	}
	original := tamoss.DeepCopy()
	controllerutil.RemoveFinalizer(tamoss, tamossFinalizer)
	return ctrl.Result{}, r.Client.Patch(ctx, tamoss, client.MergeFrom(original))
}

func (r *TamossReconciler) deleteOwnedStorageBackends(ctx context.Context, tamoss *tamossv1alpha1.Tamoss) (bool, error) {
	list := &tamossv1alpha1.StorageBackendList{}
	if err := listStorageBackendsForTamoss(ctx, r.Client, tamoss.Namespace, tamoss.Name, list); err != nil {
		return false, err
	}
	remaining := false
	for i := range list.Items {
		storageBackend := &list.Items[i]
		if !ownedByTamoss(storageBackend, tamoss) {
			continue
		}
		remaining = true
		propagation := metav1.DeletePropagationBackground
		if err := r.Client.Delete(ctx, storageBackend, client.PropagationPolicy(propagation)); err != nil && !apierrors.IsNotFound(err) {
			return true, err
		}
	}
	return remaining, nil
}

func (r *TamossReconciler) ownedObjectsRemain(ctx context.Context, tamoss *tamossv1alpha1.Tamoss) (bool, error) {
	match := tamossManagedLabelSelector(tamoss)
	listOptions := []client.ListOption{client.InNamespace(tamoss.Namespace), match}
	for _, policy := range tamossManagedResourcePolicies() {
		remaining, err := ownedObjectsRemainInTamossOwnedList(ctx, r.Client, tamoss, policy.list)
		if err != nil || remaining {
			return remaining, err
		}
	}
	middlewares := &unstructured.UnstructuredList{}
	middlewares.SetGroupVersionKind(schema.GroupVersionKind{Group: "traefik.io", Version: "v1alpha1", Kind: "MiddlewareList"})
	remaining, err := ownedObjectsRemainInList(ctx, r.Client, tamoss, middlewares, listOptions...)
	if err != nil && meta.IsNoMatchError(err) {
		return false, nil
	}
	if err != nil || remaining {
		return remaining, err
	}
	return r.optionalOwnedObjectsRemain(ctx, tamoss, listOptions...)
}

func (r *TamossReconciler) pruneOptionalOwnedObjects(ctx context.Context, tamoss *tamossv1alpha1.Tamoss, desired map[string]struct{}, listOptions ...client.ListOption) error {
	for _, list := range r.optionalOwnedObjectLists(ctx) {
		if err := pruneList(ctx, r.Client, tamoss, desired, list, listOptions...); err != nil {
			if isKubernetesNoMatchError(err) {
				continue
			}
			return err
		}
	}
	return nil
}

func (r *TamossReconciler) optionalOwnedObjectsRemain(ctx context.Context, tamoss *tamossv1alpha1.Tamoss, listOptions ...client.ListOption) (bool, error) {
	for _, list := range r.optionalOwnedObjectLists(ctx) {
		remaining, err := ownedObjectsRemainInList(ctx, r.Client, tamoss, list, listOptions...)
		if err != nil {
			if isKubernetesNoMatchError(err) {
				continue
			}
			return false, err
		}
		if remaining {
			return true, nil
		}
	}
	return false, nil
}

func (r *TamossReconciler) optionalOwnedObjectLists(ctx context.Context) []client.ObjectList {
	lists := []client.ObjectList{}
	for _, policy := range optionalTamossWatchPolicies() {
		if !r.optionalResourceCRDPresent(ctx, policy.gvr) {
			continue
		}
		lists = append(lists, policy.list)
	}
	return lists
}

func (r *TamossReconciler) optionalResourceCRDPresent(ctx context.Context, gvr schema.GroupVersionResource) bool {
	if r.Discovery == nil {
		return true
	}
	present, known := r.dependencyCRDPresent(ctx, gvr)
	return known && present
}

func ownedObjectsRemainInList(ctx context.Context, c client.Client, tamoss *tamossv1alpha1.Tamoss, list client.ObjectList, opts ...client.ListOption) (bool, error) {
	if err := c.List(ctx, list, opts...); err != nil {
		return false, err
	}
	return ownedObjectsRemainInLoadedList(tamoss, list)
}

func ownedObjectsRemainInTamossOwnedList(ctx context.Context, c client.Client, tamoss *tamossv1alpha1.Tamoss, list client.ObjectList) (bool, error) {
	if err := listTamossOwnedObjects(ctx, c, tamoss, list); err != nil {
		return false, err
	}
	return ownedObjectsRemainInLoadedList(tamoss, list)
}

func ownedObjectsRemainInLoadedList(tamoss *tamossv1alpha1.Tamoss, list client.ObjectList) (bool, error) {
	items, err := meta.ExtractList(list)
	if err != nil {
		return false, err
	}
	for _, item := range items {
		obj, ok := item.(client.Object)
		if ok && ownedByTamoss(obj, tamoss) {
			return true, nil
		}
	}
	return false, nil
}

func pruneList(ctx context.Context, c client.Client, tamoss *tamossv1alpha1.Tamoss, desired map[string]struct{}, list client.ObjectList, opts ...client.ListOption) error {
	if err := c.List(ctx, list, opts...); err != nil {
		return err
	}
	return pruneLoadedList(ctx, c, tamoss, desired, list)
}

func pruneTamossOwnedList(ctx context.Context, c client.Client, tamoss *tamossv1alpha1.Tamoss, desired map[string]struct{}, list client.ObjectList) error {
	if err := listTamossOwnedObjects(ctx, c, tamoss, list); err != nil {
		return err
	}
	return pruneLoadedList(ctx, c, tamoss, desired, list)
}

func pruneLoadedList(ctx context.Context, c client.Client, tamoss *tamossv1alpha1.Tamoss, desired map[string]struct{}, list client.ObjectList) error {
	items, err := meta.ExtractList(list)
	if err != nil {
		return err
	}
	for _, item := range items {
		obj, ok := item.(client.Object)
		if !ok || !ownedByTamoss(obj, tamoss) {
			continue
		}
		if _, keep := desired[canonicalObjectKey(obj)]; keep {
			continue
		}
		propagation := metav1.DeletePropagationBackground
		if err := c.Delete(ctx, obj, client.PropagationPolicy(propagation)); err != nil && !apierrors.IsNotFound(err) {
			return err
		}
	}
	return nil
}

func ownedByTamoss(obj client.Object, tamoss *tamossv1alpha1.Tamoss) bool {
	for _, owner := range obj.GetOwnerReferences() {
		if owner.UID != "" && owner.UID == tamoss.UID {
			return true
		}
	}
	return false
}
