package discovery

import (
	"context"
	"errors"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	fakediscovery "k8s.io/client-go/discovery/fake"
	k8stesting "k8s.io/client-go/testing"
)

func TestManagerHasCRDPresentFromStart(t *testing.T) {
	manager := NewManager(fakeDiscoveryWithCNPG(), []schema.GroupVersionResource{CNPGClustersGVR})

	if err := manager.Refresh(context.Background()); err != nil {
		t.Fatalf("refresh failed: %v", err)
	}
	expectCRDState(t, manager, CNPGClustersGVR, true, true)
}

func TestManagerHasRustFSTenantCRDPresentFromStart(t *testing.T) {
	manager := NewManager(fakeDiscoveryWithRustFS(), []schema.GroupVersionResource{RustFSTenantsGVR})

	if err := manager.Refresh(context.Background()); err != nil {
		t.Fatalf("refresh failed: %v", err)
	}
	expectCRDState(t, manager, RustFSTenantsGVR, true, true)
}

func TestManagerHasCRDUnknownBeforeFirstRefresh(t *testing.T) {
	manager := NewManager(fakeDiscoveryWithCNPG(), []schema.GroupVersionResource{CNPGClustersGVR})

	expectCRDState(t, manager, CNPGClustersGVR, false, false)
}

func TestManagerHasCRDKnownAbsentAfterFirstRefresh(t *testing.T) {
	manager := NewManager(fakeDiscovery(), []schema.GroupVersionResource{CNPGClustersGVR})

	if err := manager.Refresh(context.Background()); err == nil {
		t.Fatalf("expected first refresh to report missing group version")
	}
	expectCRDState(t, manager, CNPGClustersGVR, false, true)
}

func TestManagerDetectsCRDAddedOnRefresh(t *testing.T) {
	client := fakeDiscovery()
	manager := NewManager(client, []schema.GroupVersionResource{CNPGClustersGVR})

	if err := manager.Refresh(context.Background()); err == nil {
		t.Fatalf("expected first refresh to report missing group version")
	}
	expectCRDState(t, manager, CNPGClustersGVR, false, true)

	client.Resources = cnpgResources()
	if err := manager.Refresh(context.Background()); err != nil {
		t.Fatalf("refresh after adding CNPG failed: %v", err)
	}
	expectCRDState(t, manager, CNPGClustersGVR, true, true)
}

func TestManagerDetectsCRDRemovedOnRefresh(t *testing.T) {
	client := fakeDiscoveryWithCNPG()
	manager := NewManager(client, []schema.GroupVersionResource{CNPGClustersGVR})

	if err := manager.Refresh(context.Background()); err != nil {
		t.Fatalf("refresh failed: %v", err)
	}
	client.Resources = nil
	if err := manager.Refresh(context.Background()); err == nil {
		t.Fatalf("expected refresh after removing CNPG to report missing group version")
	}
	expectCRDState(t, manager, CNPGClustersGVR, false, true)
}

func TestManagerTreatsAPIServerErrorAsAbsent(t *testing.T) {
	manager := NewManager(failingDiscovery{}, []schema.GroupVersionResource{CNPGClustersGVR})

	if err := manager.Refresh(context.Background()); err == nil {
		t.Fatalf("expected API server error")
	}
	expectCRDState(t, manager, CNPGClustersGVR, false, true)
}

func TestManagerNotifiesObserversAfterRunRefresh(t *testing.T) {
	manager := NewManager(fakeDiscoveryWithCNPG(), []schema.GroupVersionResource{CNPGClustersGVR})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	called := 0
	manager.AddObserver(func(context.Context) {
		called++
		expectCRDState(t, manager, CNPGClustersGVR, true, true)
		cancel()
	})

	manager.Run(ctx, time.Hour)

	if called != 1 {
		t.Fatalf("expected one observer notification, got %d", called)
	}
}

func TestOptionalResourceGVRsIncludeProviderAndGatewayResources(t *testing.T) {
	values := OptionalResourceGVRs()
	for _, gvr := range []schema.GroupVersionResource{
		CNPGClustersGVR,
		CNPGScheduledBackupsGVR,
		RustFSTenantsGVR,
		GatewayHTTPRoutesGVR,
	} {
		if !gvrListContains(values, gvr) {
			t.Fatalf("expected optional resources to include %s", gvr.String())
		}
	}
}

func expectCRDState(t *testing.T, manager *Manager, gvr schema.GroupVersionResource, wantPresent bool, wantKnown bool) {
	t.Helper()
	gotPresent, gotKnown := manager.HasCRD(gvr)
	if gotPresent != wantPresent || gotKnown != wantKnown {
		t.Fatalf("expected %s present=%t known=%t, got present=%t known=%t", gvr.String(), wantPresent, wantKnown, gotPresent, gotKnown)
	}
}

func gvrListContains(values []schema.GroupVersionResource, target schema.GroupVersionResource) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func fakeDiscoveryWithCNPG() *fakediscovery.FakeDiscovery {
	client := fakeDiscovery()
	client.Resources = cnpgResources()
	return client
}

func fakeDiscoveryWithRustFS() *fakediscovery.FakeDiscovery {
	client := fakeDiscovery()
	client.Resources = rustfsResources()
	return client
}

func fakeDiscovery() *fakediscovery.FakeDiscovery {
	return &fakediscovery.FakeDiscovery{Fake: &k8stesting.Fake{}}
}

func cnpgResources() []*metav1.APIResourceList {
	return []*metav1.APIResourceList{{
		GroupVersion: "postgresql.cnpg.io/v1",
		APIResources: []metav1.APIResource{
			{Name: "clusters"},
			{Name: "scheduledbackups"},
		},
	}}
}

func rustfsResources() []*metav1.APIResourceList {
	return []*metav1.APIResourceList{{
		GroupVersion: "rustfs.com/v1alpha1",
		APIResources: []metav1.APIResource{{
			Name: "tenants",
		}},
	}}
}

type failingDiscovery struct {
	*fakediscovery.FakeDiscovery
}

func (failingDiscovery) ServerResourcesForGroupVersion(string) (*metav1.APIResourceList, error) {
	return nil, errors.New("api server unavailable")
}
