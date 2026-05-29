package controller

import (
	"errors"
	"testing"

	"k8s.io/apimachinery/pkg/runtime/schema"

	operatordiscovery "github.com/livewyer-ops/tamoss/operator/internal/discovery"
)

func TestOptionalWatchRegistrarRegistersAvailableResourcesOnce(t *testing.T) {
	available := map[schema.GroupVersionResource]bool{
		operatordiscovery.CNPGClustersGVR:         true,
		operatordiscovery.CNPGScheduledBackupsGVR: true,
		operatordiscovery.GatewayHTTPRoutesGVR:    true,
	}
	registered := []schema.GroupVersionResource{}
	registrar := newOptionalWatchRegistrar(
		func(policy optionalWatchPolicy) bool {
			return available[policy.gvr]
		},
		func(policy optionalWatchPolicy) error {
			registered = append(registered, policy.gvr)
			return nil
		},
	)

	if err := registrar.RegisterAvailable(); err != nil {
		t.Fatalf("register available watches: %v", err)
	}
	if err := registrar.RegisterAvailable(); err != nil {
		t.Fatalf("register available watches again: %v", err)
	}

	if len(registered) != 3 {
		t.Fatalf("expected 3 unique optional watches, got %#v", registered)
	}
	if containsGVR(registered, operatordiscovery.RustFSTenantsGVR) {
		t.Fatal("did not expect unavailable RustFS Tenant watch to be registered")
	}
}

func TestOptionalWatchRegistrarRegistersLateResource(t *testing.T) {
	available := map[schema.GroupVersionResource]bool{}
	registered := []schema.GroupVersionResource{}
	registrar := newOptionalWatchRegistrar(
		func(policy optionalWatchPolicy) bool {
			return available[policy.gvr]
		},
		func(policy optionalWatchPolicy) error {
			registered = append(registered, policy.gvr)
			return nil
		},
	)

	if err := registrar.RegisterAvailable(); err != nil {
		t.Fatalf("register with no resources available: %v", err)
	}
	if len(registered) != 0 {
		t.Fatalf("expected no registrations, got %#v", registered)
	}

	available[operatordiscovery.RustFSTenantsGVR] = true
	if err := registrar.RegisterAvailable(); err != nil {
		t.Fatalf("register late RustFS Tenant watch: %v", err)
	}
	if err := registrar.RegisterAvailable(); err != nil {
		t.Fatalf("register late RustFS Tenant watch again: %v", err)
	}
	if len(registered) != 1 || registered[0] != operatordiscovery.RustFSTenantsGVR {
		t.Fatalf("expected one late RustFS Tenant registration, got %#v", registered)
	}
}

func TestOptionalWatchRegistrarRetriesAfterRegistrationFailure(t *testing.T) {
	attempts := 0
	registrar := newOptionalWatchRegistrar(
		func(policy optionalWatchPolicy) bool {
			return policy.gvr == operatordiscovery.CNPGClustersGVR
		},
		func(optionalWatchPolicy) error {
			attempts++
			if attempts == 1 {
				return errors.New("cache not ready")
			}
			return nil
		},
	)

	if err := registrar.RegisterAvailable(); err == nil {
		t.Fatal("expected first registration attempt to fail")
	}
	if err := registrar.RegisterAvailable(); err != nil {
		t.Fatalf("expected second registration attempt to succeed: %v", err)
	}
	if attempts != 2 {
		t.Fatalf("expected failed registration to be retried, got %d attempts", attempts)
	}
}

func TestOptionalWatchPoliciesCoverProviderAndGatewayResources(t *testing.T) {
	policies := optionalTamossWatchPolicies()
	for _, gvr := range operatordiscovery.OptionalResourceGVRs() {
		if !policyListContainsGVR(policies, gvr) {
			t.Fatalf("expected optional watch policy for %s", gvr.String())
		}
	}
}

func containsGVR(values []schema.GroupVersionResource, target schema.GroupVersionResource) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func policyListContainsGVR(values []optionalWatchPolicy, target schema.GroupVersionResource) bool {
	for _, value := range values {
		if value.gvr == target {
			return true
		}
	}
	return false
}
