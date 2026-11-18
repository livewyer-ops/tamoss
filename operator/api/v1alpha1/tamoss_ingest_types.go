package v1alpha1

import corev1 "k8s.io/api/core/v1"

type (
	IngestSourcePolicyMode string
	IngestSourceKind       string
)

const (
	IngestSourcePolicyDisabled    IngestSourcePolicyMode = "Disabled"
	IngestSourcePolicyPublicHTTPS IngestSourcePolicyMode = "PublicHTTPS"
	IngestSourcePolicyRestricted  IngestSourcePolicyMode = "Restricted"

	IngestSourceKindHTTP IngestSourceKind = "HTTP"
	IngestSourceKindS3   IngestSourceKind = "S3"
)

// IngestSpec declares what an IngestRun in this namespace is allowed to read.
// It is an instance-owned source policy, not an asset catalogue.
type IngestSpec struct {
	SourcePolicy IngestSourcePolicySpec `json:"sourcePolicy,omitempty"`

	// Sources are reusable, named trust boundaries. Credentials belong to a
	// source and cannot be selected independently by an IngestRun.
	//+kubebuilder:validation:MaxItems=32
	Sources []IngestSourceSpec `json:"sources,omitempty"`
}

type IngestSourcePolicySpec struct {
	// Mode defaults to Disabled when omitted. The local-kind profile explicitly
	// defaults it to PublicHTTPS for the development ingest workflow.
	//+kubebuilder:validation:Enum=Disabled;PublicHTTPS;Restricted
	Mode IngestSourcePolicyMode `json:"mode,omitempty"`
}

// +kubebuilder:validation:XValidation:rule="self.kind == 'HTTP' ? has(self.http) && !has(self.s3) : has(self.s3) && !has(self.http)",message="HTTP sources require http only; S3 sources require s3 only"
type IngestSourceSpec struct {
	//+kubebuilder:validation:MinLength=1
	//+kubebuilder:validation:MaxLength=63
	//+kubebuilder:validation:Pattern=`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`
	Name string `json:"name"`

	//+kubebuilder:validation:Enum=HTTP;S3
	Kind IngestSourceKind `json:"kind"`

	HTTP *HTTPIngestSourceSpec `json:"http,omitempty"`
	S3   *S3IngestSourceSpec   `json:"s3,omitempty"`
}

type HTTPIngestSourceSpec struct {
	// Origin is an exact HTTPS scheme and authority. User information, paths,
	// queries and fragments are rejected by the controller.
	//+kubebuilder:validation:MinLength=1
	//+kubebuilder:validation:MaxLength=2048
	Origin string `json:"origin"`

	// PathPrefixes restrict URL paths within Origin. Empty allows every path.
	//+kubebuilder:validation:MaxItems=32
	//+kubebuilder:validation:items:MinLength=1
	//+kubebuilder:validation:items:MaxLength=1024
	PathPrefixes []string `json:"pathPrefixes,omitempty"`

	// AllowPrivateAddresses permits private, loopback and link-local resolved
	// addresses for this explicitly named source.
	AllowPrivateAddresses bool `json:"allowPrivateAddresses,omitempty"`

	// CredentialSecretRef names a source-bound Secret. The Secret must contain
	// TAMSIN_SOURCE_HTTP_HEADERS as a JSON array of HTTP header values.
	CredentialSecretRef *corev1.LocalObjectReference `json:"credentialSecretRef,omitempty"`
}

type S3IngestSourceSpec struct {
	// Endpoint is the exact HTTPS endpoint TAMSin may contact.
	//+kubebuilder:validation:MinLength=1
	//+kubebuilder:validation:MaxLength=2048
	Endpoint string `json:"endpoint"`

	//+kubebuilder:validation:MinLength=1
	//+kubebuilder:validation:MaxLength=128
	Region string `json:"region"`

	//+kubebuilder:validation:MinLength=1
	//+kubebuilder:validation:MaxLength=255
	Bucket string `json:"bucket"`

	// KeyPrefixes restrict object keys within Bucket. Empty allows every key.
	//+kubebuilder:validation:MaxItems=32
	//+kubebuilder:validation:items:MinLength=1
	//+kubebuilder:validation:items:MaxLength=1024
	KeyPrefixes []string `json:"keyPrefixes,omitempty"`

	PathStyle bool `json:"pathStyle,omitempty"`

	AllowPrivateAddresses bool `json:"allowPrivateAddresses,omitempty"`

	// CredentialSecretRef names a source-bound Secret containing
	// AWS_ACCESS_KEY_ID and AWS_SECRET_ACCESS_KEY. AWS_SESSION_TOKEN is optional.
	CredentialSecretRef *corev1.LocalObjectReference `json:"credentialSecretRef,omitempty"`
}
