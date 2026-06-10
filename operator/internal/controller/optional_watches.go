package controller

import (
	"context"
	"errors"
	"fmt"
	"sync"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	crcontroller "sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/source"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	tamossv1alpha1 "github.com/livewyer-ops/tamoss/operator/api/v1alpha1"
	"github.com/livewyer-ops/tamoss/operator/internal/controller/backend/rustfs"
	operatordiscovery "github.com/livewyer-ops/tamoss/operator/internal/discovery"
)

type optionalWatchPolicy struct {
	name   string
	gvr    schema.GroupVersionResource
	gvk    schema.GroupVersionKind
	object client.Object
	list   client.ObjectList
}

type optionalWatchRegistrar struct {
	mu        sync.Mutex
	available func(optionalWatchPolicy) bool
	register  func(optionalWatchPolicy) error
	watched   map[schema.GroupVersionResource]struct{}
}

func optionalTamossWatchPolicies() []optionalWatchPolicy {
	return []optionalWatchPolicy{
		{name: "cnpg-cluster", gvr: operatordiscovery.CNPGClustersGVR, gvk: cnpgv1.SchemeGroupVersion.WithKind("Cluster"), object: &cnpgv1.Cluster{}, list: &cnpgv1.ClusterList{}},
		{name: "cnpg-scheduledbackup", gvr: operatordiscovery.CNPGScheduledBackupsGVR, gvk: cnpgv1.SchemeGroupVersion.WithKind("ScheduledBackup"), object: &cnpgv1.ScheduledBackup{}, list: &cnpgv1.ScheduledBackupList{}},
		{name: "rustfs-tenant", gvr: operatordiscovery.RustFSTenantsGVR, gvk: rustfs.TenantGVK, object: rustfs.NewTenant(), list: rustfs.NewTenantList()},
		{name: "gateway-httproute", gvr: operatordiscovery.GatewayHTTPRoutesGVR, gvk: httpRouteGVK, object: &gatewayv1.HTTPRoute{}, list: &gatewayv1.HTTPRouteList{}},
	}
}

func newOptionalWatchRegistrar(available func(optionalWatchPolicy) bool, register func(optionalWatchPolicy) error) *optionalWatchRegistrar {
	return &optionalWatchRegistrar{
		available: available,
		register:  register,
		watched:   map[schema.GroupVersionResource]struct{}{},
	}
}

func newTamossOptionalWatchRegistrar(mgr ctrl.Manager, controller crcontroller.Controller, discovery optionalResourceDiscovery) *optionalWatchRegistrar {
	available := func(policy optionalWatchPolicy) bool {
		if discovery != nil {
			present, known := discovery.HasCRD(policy.gvr)
			return known && present
		}
		return optionalWatchMapped(mgr, policy)
	}
	register := func(policy optionalWatchPolicy) error {
		return controller.Watch(source.Kind(
			mgr.GetCache(),
			policy.object,
			handler.EnqueueRequestForOwner(
				mgr.GetScheme(),
				mgr.GetRESTMapper(),
				&tamossv1alpha1.Tamoss{},
				handler.OnlyControllerOwner(),
			),
		))
	}
	return newOptionalWatchRegistrar(available, register)
}

func (r *optionalWatchRegistrar) RegisterAvailable() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	var result error
	for _, policy := range optionalTamossWatchPolicies() {
		if _, ok := r.watched[policy.gvr]; ok {
			continue
		}
		if !r.available(policy) {
			continue
		}
		if err := r.register(policy); err != nil {
			result = errors.Join(result, fmt.Errorf("register optional watch %s: %w", policy.name, err))
			continue
		}
		r.watched[policy.gvr] = struct{}{}
	}
	return result
}

func optionalWatchMapped(mgr ctrl.Manager, policy optionalWatchPolicy) bool {
	_, err := mgr.GetRESTMapper().RESTMapping(policy.gvk.GroupKind(), policy.gvk.Version)
	return err == nil
}

type optionalResourceDiscovery interface {
	HasCRD(schema.GroupVersionResource) (bool, bool)
}

func (r *TamossReconciler) RegisterOptionalWatches(context.Context) error {
	if r.optionalWatches == nil {
		return nil
	}
	return r.optionalWatches.RegisterAvailable()
}
