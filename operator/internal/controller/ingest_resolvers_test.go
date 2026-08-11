package controller

import (
	"context"
	"strings"
	"testing"

	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	tamossv1alpha1 "github.com/livewyer-ops/tamoss/operator/api/v1alpha1"
)

const testApprovedMediaURL = "https://media.example.test/sintel_trailer-480p.mp4"

func tamossWithApprovedInput(input tamossv1alpha1.ApprovedIngestInputSpec) *tamossv1alpha1.Tamoss {
	tamoss := testIngestTamoss()
	tamoss.Spec.Ingest.ApprovedInputs = []tamossv1alpha1.ApprovedIngestInputSpec{input}
	return tamoss
}

func TestApprovedInputResolverReturnsOwnerApprovedLocations(t *testing.T) {
	scheme := ingestRunTestScheme(t)
	tamoss := tamossWithApprovedInput(tamossv1alpha1.ApprovedIngestInputSpec{
		ID:   "staged-123",
		Kind: "ApprovedHTTP",
		URLs: []string{testApprovedMediaURL},
	})
	resolver := ApprovedInputResolver{Client: fake.NewClientBuilder().WithScheme(scheme).WithObjects(tamoss).Build()}

	resolved, err := resolver.Resolve(context.Background(), tamoss.Namespace, tamoss.Name,
		tamossv1alpha1.IngestInputReference{Kind: "ApprovedHTTP", ID: "staged-123"}, 1000)
	if err != nil {
		t.Fatalf("resolve approved input: %v", err)
	}
	if len(resolved.Selectors) != 1 || resolved.Selectors[0] != testApprovedMediaURL {
		t.Fatalf("selectors = %#v, want the approved location", resolved.Selectors)
	}
	if resolved.ExpectedInputs != 1 {
		t.Fatalf("expectedInputs = %d, want 1", resolved.ExpectedInputs)
	}
	// The controller revalidates whatever a resolver returns, so an approval
	// can never introduce a selector the ingest boundary would refuse.
	if err := validateResolvedIngestInputs(resolved, 1000); err != nil {
		t.Fatalf("approved locations must satisfy the ingest boundary: %v", err)
	}
}

// An unapproved id is the ordinary case for a forged or stale reference.
func TestApprovedInputResolverRefusesUnapprovedIdentifiers(t *testing.T) {
	scheme := ingestRunTestScheme(t)
	tamoss := tamossWithApprovedInput(tamossv1alpha1.ApprovedIngestInputSpec{
		ID:   "staged-123",
		Kind: "ApprovedHTTP",
		URLs: []string{testApprovedMediaURL},
	})
	resolver := ApprovedInputResolver{Client: fake.NewClientBuilder().WithScheme(scheme).WithObjects(tamoss).Build()}

	if _, err := resolver.Resolve(context.Background(), tamoss.Namespace, tamoss.Name,
		tamossv1alpha1.IngestInputReference{Kind: "ApprovedHTTP", ID: "not-approved"}, 1000); err == nil {
		t.Fatal("expected an unapproved input reference to be refused")
	}
}

// One approval must not satisfy a reference of a different kind.
func TestApprovedInputResolverRefusesKindMismatch(t *testing.T) {
	scheme := ingestRunTestScheme(t)
	tamoss := tamossWithApprovedInput(tamossv1alpha1.ApprovedIngestInputSpec{
		ID:   "staged-123",
		Kind: "ApprovedHTTP",
		URLs: []string{testApprovedMediaURL},
	})
	resolver := ApprovedInputResolver{Client: fake.NewClientBuilder().WithScheme(scheme).WithObjects(tamoss).Build()}

	_, err := resolver.Resolve(context.Background(), tamoss.Namespace, tamoss.Name,
		tamossv1alpha1.IngestInputReference{Kind: "ApprovedS3", ID: "staged-123"}, 1000)
	if err == nil || !strings.Contains(err.Error(), "kind") {
		t.Fatalf("expected a kind mismatch to be refused, got %v", err)
	}
}

func TestApprovedInputResolverHonoursTheRunInputLimit(t *testing.T) {
	scheme := ingestRunTestScheme(t)
	tamoss := tamossWithApprovedInput(tamossv1alpha1.ApprovedIngestInputSpec{
		ID:   "staged-123",
		Kind: "ApprovedHTTP",
		URLs: []string{testApprovedMediaURL, "https://media.example.test/second.mp4"},
	})
	resolver := ApprovedInputResolver{Client: fake.NewClientBuilder().WithScheme(scheme).WithObjects(tamoss).Build()}

	if _, err := resolver.Resolve(context.Background(), tamoss.Namespace, tamoss.Name,
		tamossv1alpha1.IngestInputReference{Kind: "ApprovedHTTP", ID: "staged-123"}, 1); err == nil {
		t.Fatal("expected an approval wider than the run's limit to be refused")
	}
}

// An instance with no approvals must resolve nothing at all.
func TestApprovedInputResolverFailsClosedWithoutApprovals(t *testing.T) {
	scheme := ingestRunTestScheme(t)
	tamoss := testIngestTamoss()
	resolver := ApprovedInputResolver{Client: fake.NewClientBuilder().WithScheme(scheme).WithObjects(tamoss).Build()}

	if _, err := resolver.Resolve(context.Background(), tamoss.Namespace, tamoss.Name,
		tamossv1alpha1.IngestInputReference{Kind: "ApprovedHTTP", ID: "staged-123"}, 1000); err == nil {
		t.Fatal("expected an instance with no approved inputs to resolve nothing")
	}
}

func TestPublishedEndpointResolverReturnsThePublishedEndpoint(t *testing.T) {
	scheme := ingestRunTestScheme(t)
	tamoss := testIngestTamoss()
	tamoss.Status.Endpoints.API = "https://api.example.test"
	resolver := PublishedEndpointResolver{Client: fake.NewClientBuilder().WithScheme(scheme).WithObjects(tamoss).Build()}

	endpoint, err := resolver.Resolve(context.Background(), tamoss.Namespace, tamoss.Name)
	if err != nil {
		t.Fatalf("resolve endpoint: %v", err)
	}
	if endpoint != "https://api.example.test" {
		t.Fatalf("endpoint = %q, want the published API endpoint", endpoint)
	}
	if _, err := validateIngestEndpoint(endpoint); err != nil {
		t.Fatalf("published endpoint must satisfy the ingest boundary: %v", err)
	}
}

// Tamsin carries a full-access bearer token, so a plaintext route must not be
// silently upgraded to https by the resolver; it has to fail the boundary.
func TestPublishedEndpointResolverDoesNotUpgradePlaintextRoutes(t *testing.T) {
	scheme := ingestRunTestScheme(t)
	tamoss := testIngestTamoss()
	tamoss.Status.Endpoints.API = "http://api.example.test"
	resolver := PublishedEndpointResolver{Client: fake.NewClientBuilder().WithScheme(scheme).WithObjects(tamoss).Build()}

	endpoint, err := resolver.Resolve(context.Background(), tamoss.Namespace, tamoss.Name)
	if err != nil {
		t.Fatalf("resolve endpoint: %v", err)
	}
	if endpoint != "http://api.example.test" {
		t.Fatalf("endpoint = %q, want the published value verbatim", endpoint)
	}
	if _, err := validateIngestEndpoint(endpoint); err == nil {
		t.Fatal("a plaintext endpoint must be refused by the ingest boundary")
	}
}

func TestPublishedEndpointResolverFailsWithoutAPublishedEndpoint(t *testing.T) {
	scheme := ingestRunTestScheme(t)
	tamoss := testIngestTamoss()
	resolver := PublishedEndpointResolver{Client: fake.NewClientBuilder().WithScheme(scheme).WithObjects(tamoss).Build()}

	if _, err := resolver.Resolve(context.Background(), tamoss.Namespace, tamoss.Name); err == nil {
		t.Fatal("expected an instance with no published endpoint to fail")
	}
}
