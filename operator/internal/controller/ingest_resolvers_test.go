package controller

import (
	"context"
	"fmt"
	"net"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	tamossv1alpha1 "github.com/livewyer-ops/tamoss/operator/api/v1alpha1"
)

type staticIngestHostResolver map[string][]net.IPAddr

func (r staticIngestHostResolver) LookupIPAddr(_ context.Context, host string) ([]net.IPAddr, error) {
	addresses, found := r[host]
	if !found {
		return nil, fmt.Errorf("unexpected DNS lookup for %s", host)
	}
	return addresses, nil
}

func sourcePolicyResolverFor(t *testing.T, objects ...runtime.Object) SourcePolicyResolver {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := tamossv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	return SourcePolicyResolver{
		Client: fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(objects...).Build(),
		HostResolver: staticIngestHostResolver{
			"media.example.test":   {{IP: net.ParseIP("93.184.216.34")}},
			"objects.example.test": {{IP: net.ParseIP("93.184.216.35")}},
		},
	}
}

func sourcePolicyTamoss(mode tamossv1alpha1.IngestSourcePolicyMode, sources ...tamossv1alpha1.IngestSourceSpec) *tamossv1alpha1.Tamoss {
	return &tamossv1alpha1.Tamoss{
		ObjectMeta: metav1.ObjectMeta{Name: "media", Namespace: "tams"},
		Spec: tamossv1alpha1.TamossSpec{Ingest: tamossv1alpha1.IngestSpec{
			SourcePolicy: tamossv1alpha1.IngestSourcePolicySpec{Mode: mode}, Sources: sources,
		}},
	}
}

func TestSourcePolicyResolverDefaultsToDisabled(t *testing.T) {
	tamoss := sourcePolicyTamoss("")
	_, err := sourcePolicyResolverFor(t).Resolve(context.Background(), tamoss, tamossv1alpha1.IngestRunInput{
		Kind: tamossv1alpha1.IngestInputKindHTTP, URI: "https://media.example.test/clip.mp4",
	}, 1000)
	if err == nil || !strings.Contains(err.Error(), "Disabled") {
		t.Fatalf("Resolve() error = %v, want disabled policy", err)
	}
}

func TestSourcePolicyResolverAllowsUnnamedPublicHTTPS(t *testing.T) {
	tamoss := sourcePolicyTamoss(tamossv1alpha1.IngestSourcePolicyPublicHTTPS)
	resolved, err := sourcePolicyResolverFor(t).Resolve(context.Background(), tamoss, tamossv1alpha1.IngestRunInput{
		Kind: tamossv1alpha1.IngestInputKindHTTP, URI: "https://media.example.test/library/clip.mp4",
	}, 1000)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.SourceName != "public-https" || resolved.ExpectedInputs != 1 || resolved.Selectors[0] != "https://media.example.test/library/clip.mp4" {
		t.Fatalf("resolved = %#v", resolved)
	}
	if len(resolved.PolicyDigest) != 64 {
		t.Fatalf("policy digest = %q", resolved.PolicyDigest)
	}
}

func TestSourcePolicyResolverRejectsUnsafePublicHTTPS(t *testing.T) {
	tamoss := sourcePolicyTamoss(tamossv1alpha1.IngestSourcePolicyPublicHTTPS)
	for _, uri := range []string{
		"http://media.example.test/clip.mp4",
		"https://user@media.example.test/clip.mp4",
		"https://media.example.test:8443/clip.mp4",
		"https://127.0.0.1/clip.mp4",
		"https://media.example.test/clip.mp4?token=secret",
	} {
		t.Run(uri, func(t *testing.T) {
			_, err := sourcePolicyResolverFor(t).Resolve(context.Background(), tamoss, tamossv1alpha1.IngestRunInput{
				Kind: tamossv1alpha1.IngestInputKindHTTP, URI: uri,
			}, 1000)
			if err == nil {
				t.Fatalf("Resolve(%q) succeeded", uri)
			}
		})
	}
}

func TestSourcePolicyResolverRejectsPrivateDNSUnlessNamedSourceAllowsIt(t *testing.T) {
	resolver := sourcePolicyResolverFor(t)
	resolver.HostResolver = staticIngestHostResolver{
		"private.example.test": {{IP: net.ParseIP("10.0.0.8")}},
	}
	public := sourcePolicyTamoss(tamossv1alpha1.IngestSourcePolicyPublicHTTPS)
	input := tamossv1alpha1.IngestRunInput{
		Kind: tamossv1alpha1.IngestInputKindHTTP, URI: "https://private.example.test/clip.mp4",
	}
	if _, err := resolver.Resolve(context.Background(), public, input, 1000); err == nil || !strings.Contains(err.Error(), "non-public") {
		t.Fatalf("public Resolve() error = %v, want non-public address rejection", err)
	}

	source := tamossv1alpha1.IngestSourceSpec{
		Name: "private", Kind: tamossv1alpha1.IngestSourceKindHTTP,
		HTTP: &tamossv1alpha1.HTTPIngestSourceSpec{
			Origin: "https://private.example.test", AllowPrivateAddresses: true,
		},
	}
	input.SourceRef = &tamossv1alpha1.IngestSourceReference{Name: source.Name}
	if _, err := resolver.Resolve(context.Background(), sourcePolicyTamoss(tamossv1alpha1.IngestSourcePolicyRestricted, source), input, 1000); err != nil {
		t.Fatalf("private named source Resolve() error = %v", err)
	}
}

func TestSourcePolicyResolverRestrictsNamedHTTPSourceAndCredentials(t *testing.T) {
	secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "review-http", Namespace: "tams"}, Data: map[string][]byte{
		httpCredentialSecretKey: []byte(`["Authorization: Bearer redacted"]`),
	}}
	source := tamossv1alpha1.IngestSourceSpec{
		Name: "review", Kind: tamossv1alpha1.IngestSourceKindHTTP,
		HTTP: &tamossv1alpha1.HTTPIngestSourceSpec{
			Origin: "https://media.example.test", PathPrefixes: []string{"/approved/"}, AllowPrivateAddresses: true,
			CredentialSecretRef: &corev1.LocalObjectReference{Name: secret.Name},
		},
	}
	tamoss := sourcePolicyTamoss(tamossv1alpha1.IngestSourcePolicyRestricted, source)
	resolver := sourcePolicyResolverFor(t, secret)
	resolved, err := resolver.Resolve(context.Background(), tamoss, tamossv1alpha1.IngestRunInput{
		Kind: tamossv1alpha1.IngestInputKindHTTP, URI: "https://media.example.test/approved/clip.mp4",
		SourceRef: &tamossv1alpha1.IngestSourceReference{Name: source.Name},
	}, 1000)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.CredentialSecretName != secret.Name || resolved.CredentialKind != tamossv1alpha1.IngestSourceKindHTTP {
		t.Fatalf("credential handoff = %#v", resolved)
	}
	if len(resolved.PolicyDigest) != 64 {
		t.Fatalf("policy digest = %q", resolved.PolicyDigest)
	}

	for _, uri := range []string{
		"https://other.example.test/approved/clip.mp4",
		"https://media.example.test/private/clip.mp4",
		"https://media.example.test/approved/%2e%2e/private/clip.mp4",
	} {
		_, err := resolver.Resolve(context.Background(), tamoss, tamossv1alpha1.IngestRunInput{
			Kind: tamossv1alpha1.IngestInputKindHTTP, URI: uri, SourceRef: &tamossv1alpha1.IngestSourceReference{Name: source.Name},
		}, 1000)
		if err == nil {
			t.Fatalf("Resolve(%q) succeeded outside policy", uri)
		}
	}
}

func TestSourcePolicyResolverRequiresNamedS3SourceAndBoundsPrefix(t *testing.T) {
	source := tamossv1alpha1.IngestSourceSpec{
		Name: "archive", Kind: tamossv1alpha1.IngestSourceKindS3,
		S3: &tamossv1alpha1.S3IngestSourceSpec{
			Endpoint: "https://objects.example.test", Region: "eu-west-2", Bucket: "archive",
			KeyPrefixes: []string{"incoming/"}, PathStyle: true,
		},
	}
	tamoss := sourcePolicyTamoss(tamossv1alpha1.IngestSourcePolicyPublicHTTPS, source)
	resolver := sourcePolicyResolverFor(t)
	resolved, err := resolver.Resolve(context.Background(), tamoss, tamossv1alpha1.IngestRunInput{
		Kind: tamossv1alpha1.IngestInputKindS3, URI: "s3://archive/incoming/day-1/",
		SourceRef: &tamossv1alpha1.IngestSourceReference{Name: source.Name},
	}, 1000)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.ExpectedInputs != 0 || resolved.S3Endpoint != source.S3.Endpoint || resolved.S3Region != source.S3.Region || !resolved.S3PathStyle {
		t.Fatalf("resolved = %#v", resolved)
	}

	for _, input := range []tamossv1alpha1.IngestRunInput{
		{Kind: tamossv1alpha1.IngestInputKindS3, URI: "s3://archive/incoming/day-1/"},
		{Kind: tamossv1alpha1.IngestInputKindS3, URI: "s3://archive/private/day-1/", SourceRef: &tamossv1alpha1.IngestSourceReference{Name: source.Name}},
		{Kind: tamossv1alpha1.IngestInputKindS3, URI: "s3://other/incoming/day-1/", SourceRef: &tamossv1alpha1.IngestSourceReference{Name: source.Name}},
	} {
		if _, err := resolver.Resolve(context.Background(), tamoss, input, 1000); err == nil {
			t.Fatalf("Resolve(%#v) succeeded", input)
		}
	}
}

func TestSourcePolicyResolverRejectsMissingCredentialKeys(t *testing.T) {
	secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "archive", Namespace: "tams"}, Data: map[string][]byte{
		s3AccessKeySecretKey: []byte("access"),
	}}
	source := tamossv1alpha1.IngestSourceSpec{
		Name: "archive", Kind: tamossv1alpha1.IngestSourceKindS3,
		S3: &tamossv1alpha1.S3IngestSourceSpec{
			Endpoint: "https://objects.example.test", Region: "eu-west-2", Bucket: "archive",
			CredentialSecretRef: &corev1.LocalObjectReference{Name: secret.Name},
		},
	}
	_, err := sourcePolicyResolverFor(t, secret).Resolve(context.Background(), sourcePolicyTamoss(tamossv1alpha1.IngestSourcePolicyRestricted, source), tamossv1alpha1.IngestRunInput{
		Kind: tamossv1alpha1.IngestInputKindS3, URI: "s3://archive/object.ts", SourceRef: &tamossv1alpha1.IngestSourceReference{Name: source.Name},
	}, 1000)
	if err == nil || !strings.Contains(err.Error(), s3SecretKeySecretKey) {
		t.Fatalf("Resolve() error = %v", err)
	}
}

func TestSourcePolicyResolverRejectsMalformedHTTPCredentialHeaders(t *testing.T) {
	secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "review", Namespace: "tams"}, Data: map[string][]byte{
		httpCredentialSecretKey: []byte(`["missing-colon"]`),
	}}
	source := tamossv1alpha1.IngestSourceSpec{
		Name: "review", Kind: tamossv1alpha1.IngestSourceKindHTTP,
		HTTP: &tamossv1alpha1.HTTPIngestSourceSpec{
			Origin: "https://media.example.test", CredentialSecretRef: &corev1.LocalObjectReference{Name: secret.Name},
		},
	}
	_, err := sourcePolicyResolverFor(t, secret).Resolve(context.Background(), sourcePolicyTamoss(tamossv1alpha1.IngestSourcePolicyRestricted, source), tamossv1alpha1.IngestRunInput{
		Kind: tamossv1alpha1.IngestInputKindHTTP, URI: "https://media.example.test/clip.mp4", SourceRef: &tamossv1alpha1.IngestSourceReference{Name: source.Name},
	}, 1000)
	if err == nil || !strings.Contains(err.Error(), "invalid HTTP header") {
		t.Fatalf("Resolve() error = %v", err)
	}
}
