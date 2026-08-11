package controller

import (
	"context"
	"fmt"

	"sigs.k8s.io/controller-runtime/pkg/client"

	tamossv1alpha1 "github.com/livewyer-ops/tamoss/operator/api/v1alpha1"
)

// ApprovedInputResolver turns an opaque IngestRun input reference into media
// locations the instance owner has approved on spec.ingest.approvedInputs.
//
// An IngestRun never carries a locator, so this lookup is the boundary between
// what a run asks for and what it can reach. The resolver only widens the
// request to locations already written into the Tamoss spec; it never accepts
// one from the run. The controller independently revalidates whatever comes
// back, so a mistaken approval still cannot introduce credentials or a signed
// URL.
type ApprovedInputResolver struct {
	Client client.Reader
}

func (r ApprovedInputResolver) Resolve(
	ctx context.Context,
	namespace string,
	instance string,
	ref tamossv1alpha1.IngestInputReference,
	maxInputs int32,
) (ResolvedIngestInputs, error) {
	tamoss := &tamossv1alpha1.Tamoss{}
	if err := r.Client.Get(ctx, client.ObjectKey{Namespace: namespace, Name: instance}, tamoss); err != nil {
		return ResolvedIngestInputs{}, fmt.Errorf("read approved inputs: %w", err)
	}

	for _, approved := range tamoss.Spec.Ingest.ApprovedInputs {
		if approved.ID != ref.ID {
			continue
		}
		// A run naming an approved id under the wrong kind is a mismatch, not a
		// near miss: resolving it anyway would let one approval satisfy a
		// reference the owner never granted.
		if approved.Kind != ref.Kind {
			return ResolvedIngestInputs{}, fmt.Errorf(
				"approved input %q is kind %q, not %q", ref.ID, approved.Kind, ref.Kind)
		}
		// Bound the count before narrowing it: the schema caps an approval at
		// far fewer locations than the selector limit, so anything above that
		// is a malformed object rather than a large batch.
		count := len(approved.URLs)
		if count > maxIngestSelectors {
			return ResolvedIngestInputs{}, fmt.Errorf(
				"approved input %q resolves %d locations, above the %d selector limit",
				ref.ID, count, maxIngestSelectors)
		}
		if int64(count) > int64(maxInputs) {
			return ResolvedIngestInputs{}, fmt.Errorf(
				"approved input %q resolves %d locations, above the run's %d limit",
				ref.ID, count, maxInputs)
		}
		return ResolvedIngestInputs{
			Selectors:      append([]string(nil), approved.URLs...),
			ExpectedInputs: int32(count),
		}, nil
	}
	return ResolvedIngestInputs{}, fmt.Errorf("no approved input %q for %s/%s", ref.ID, namespace, instance)
}

// PublishedEndpointResolver selects the instance's own published TAMS endpoint.
//
// It returns the endpoint exactly as reconcile published it. An instance routed
// over plaintext has no approved endpoint rather than an upgraded one: Tamsin
// carries a full-access bearer token, so inferring TLS that the route does not
// actually terminate would put that token on the wire in clear.
type PublishedEndpointResolver struct {
	Client client.Reader
}

func (r PublishedEndpointResolver) Resolve(ctx context.Context, namespace, name string) (string, error) {
	tamoss := &tamossv1alpha1.Tamoss{}
	if err := r.Client.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, tamoss); err != nil {
		return "", fmt.Errorf("read published endpoint: %w", err)
	}
	endpoint := tamoss.Status.Endpoints.API
	if endpoint == "" {
		return "", fmt.Errorf("instance %s/%s has not published an API endpoint", namespace, name)
	}
	return endpoint, nil
}
