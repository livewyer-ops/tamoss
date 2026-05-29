package v1alpha1

import (
	"crypto/sha256"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type StorageBackendProvider string

const (
	StorageBackendProviderRustFS     StorageBackendProvider = "rustfs"
	StorageBackendProviderExternalS3 StorageBackendProvider = "external-s3"
	DefaultStorageBackendID          string                 = "f1ab5b54-9703-42ed-b181-11ba1c794a7f"
)

// +kubebuilder:validation:XValidation:rule="has(self.id) == has(oldSelf.id) && (!has(self.id) || self.id == oldSelf.id)",message="spec.id is immutable"
// +kubebuilder:validation:XValidation:rule="self.provider == oldSelf.provider",message="spec.provider is immutable"
// +kubebuilder:validation:XValidation:rule="has(self.bucketName) == has(oldSelf.bucketName) && (!has(self.bucketName) || self.bucketName == oldSelf.bucketName)",message="spec.bucketName is immutable"
// +kubebuilder:validation:XValidation:rule="has(self.tamossRef) && has(self.tamossRef.name) && self.tamossRef.name.size() > 0",message="spec.tamossRef.name is required"
// +kubebuilder:validation:XValidation:rule="self.tamossRef.name == oldSelf.tamossRef.name",message="spec.tamossRef.name is immutable"
// +kubebuilder:validation:XValidation:rule="self.provider != 'external-s3' || (has(self.endpoint) && has(self.endpoint.default) && has(self.endpoint.default.url) && self.endpoint.default.url.size() > 0)",message="spec.endpoint.default.url is required when spec.provider is external-s3"
type StorageBackendSpec struct {
	// ID is the BBC TAMS storage backend UUID advertised to clients.
	// Empty lets the operator derive a deterministic UUID from namespace/name.
	//+kubebuilder:validation:Format=uuid
	ID string `json:"id,omitempty"`

	// TamossRef identifies the Tamoss instance that owns the TAMS database registration.
	TamossRef TamossReferenceSpec `json:"tamossRef,omitempty"`

	// Provider selects the storage implementation reconciled by this resource.
	//+kubebuilder:validation:Enum=rustfs;external-s3
	//+kubebuilder:default=rustfs
	Provider StorageBackendProvider `json:"provider,omitempty"`

	//+kubebuilder:default=false
	DefaultStorage bool `json:"defaultStorage,omitempty"`

	//+kubebuilder:validation:MinLength=1
	Label string `json:"label,omitempty"`

	//+kubebuilder:default=us-east-1
	//+kubebuilder:validation:MinLength=1
	Region string `json:"region,omitempty"`

	//+kubebuilder:default=s3
	//+kubebuilder:validation:MinLength=1
	StoreProduct string `json:"storeProduct,omitempty"`

	//+kubebuilder:default=http_object_store
	//+kubebuilder:validation:MinLength=1
	StoreType string `json:"storeType,omitempty"`

	//+kubebuilder:validation:MinLength=1
	BucketName string `json:"bucketName,omitempty"`

	Endpoint S3EndpointSpec `json:"endpoint,omitempty"`

	Credentials SecretReferenceSpec `json:"credentials,omitempty"`
}

type TamossReferenceSpec struct {
	//+kubebuilder:validation:MinLength=1
	Name string `json:"name,omitempty"`
}

type StorageBackendStatus struct {
	ObservedGeneration int64                        `json:"observedGeneration,omitempty"`
	Conditions         []metav1.Condition           `json:"conditions,omitempty"`
	BackendID          string                       `json:"backendID,omitempty"`
	BucketName         string                       `json:"bucketName,omitempty"`
	Resolved           StorageBackendResolvedStatus `json:"resolved,omitempty"`
	//+kubebuilder:validation:Enum=Pending;Progressing;Ready;Degraded
	Phase string `json:"phase,omitempty"`
}

type StorageBackendResolvedStatus struct {
	BackendID         string                 `json:"backendID,omitempty"`
	BucketName        string                 `json:"bucketName,omitempty"`
	Provider          StorageBackendProvider `json:"provider,omitempty"`
	EndpointURL       string                 `json:"endpointURL,omitempty"`
	PublicEndpointURL string                 `json:"publicEndpointURL,omitempty"`
	CredentialsSecret string                 `json:"credentialsSecret,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
//+kubebuilder:resource:scope=Namespaced,path=storagebackends,shortName=sb
//+kubebuilder:printcolumn:name="Tamoss",type=string,JSONPath=`.spec.tamossRef.name`
//+kubebuilder:printcolumn:name="Provider",type=string,JSONPath=`.spec.provider`
//+kubebuilder:printcolumn:name="Bucket",type=string,JSONPath=`.spec.bucketName`
//+kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`
//+kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// StorageBackend is the Schema for declarative TAMS storage backend registration.
type StorageBackend struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   StorageBackendSpec   `json:"spec,omitempty"`
	Status StorageBackendStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// StorageBackendList contains a list of StorageBackend.
type StorageBackendList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []StorageBackend `json:"items"`
}

func init() {
	SchemeBuilder.Register(&StorageBackend{}, &StorageBackendList{})
}

func (s *StorageBackendSpec) ApplyDefaults(namespace, name string) {
	if s.Provider == "" {
		s.Provider = StorageBackendProviderRustFS
	}
	if s.Region == "" {
		s.Region = "us-east-1"
	}
	if s.StoreProduct == "" {
		s.StoreProduct = "s3"
	}
	if s.StoreType == "" {
		s.StoreType = "http_object_store"
	}
	if s.Endpoint.Public.URL == "" {
		s.Endpoint.Public.URL = DefaultStorageBackendPublicEndpoint(*s)
	}
	if s.Label == "" && s.BucketName != "" {
		s.Label = "tamoss." + s.Region + ":s3:" + s.BucketName
	}
	if s.ID == "" {
		s.ID = DeterministicStorageBackendID(namespace, name)
	}
}

func (s StorageBackendSpec) IsExternalObjectStore() bool {
	return s.Provider == StorageBackendProviderExternalS3
}

func DefaultStorageBackendPublicEndpoint(s StorageBackendSpec) string {
	return s.Endpoint.Default.URL
}

func DeterministicStorageBackendID(namespace, name string) string {
	sum := sha256.Sum256([]byte("tamoss-storagebackend:" + namespace + "/" + name))
	value := sum[:16]
	value[6] = (value[6] & 0x0f) | 0x50
	value[8] = (value[8] & 0x3f) | 0x80
	return fmt.Sprintf(
		"%x-%x-%x-%x-%x",
		value[0:4],
		value[4:6],
		value[6:8],
		value[8:10],
		value[10:16],
	)
}
