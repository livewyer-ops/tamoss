package v1alpha1

// IngestSpec declares what an IngestRun in this namespace is allowed to read.
type IngestSpec struct {
	// ApprovedInputs are the media locations an IngestRun may name by id. An
	// IngestRun carries an opaque id and never a media locator, so this list is
	// the boundary between what a run asks for and what it can reach.
	//+kubebuilder:validation:MaxItems=16
	ApprovedInputs []ApprovedIngestInputSpec `json:"approvedInputs,omitempty"`
}

// ApprovedIngestInputSpec binds one opaque input id to fixed media locations.
type ApprovedIngestInputSpec struct {
	// ID is the value an IngestRun's spec.inputRef.id must carry.
	//+kubebuilder:validation:MinLength=1
	//+kubebuilder:validation:MaxLength=128
	//+kubebuilder:validation:Pattern=`^[A-Za-z0-9][A-Za-z0-9._-]*$`
	ID string `json:"id"`

	// Kind must match the referencing run. Only ApprovedHTTP resolves today;
	// staged objects, manifests, and S3 prefixes need a credential boundary
	// that does not exist yet.
	//+kubebuilder:validation:Enum=ApprovedHTTP
	Kind string `json:"kind"`

	// URLs are absolute HTTPS media locations. Credentials, query strings, and
	// fragments are rejected so an approved input cannot smuggle a signed URL.
	//+kubebuilder:validation:MinItems=1
	//+kubebuilder:validation:MaxItems=16
	//+kubebuilder:validation:items:MaxLength=2048
	//+kubebuilder:validation:items:Pattern=`^https://[^/?#@]+(/[^?#]*)?$`
	URLs []string `json:"urls"`
}
