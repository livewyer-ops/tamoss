package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
//+kubebuilder:printcolumn:name="Status",type=string,JSONPath=`.status.phase`
//+kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`
//+kubebuilder:printcolumn:name="DB",type=string,JSONPath=`.status.providers.db.provider`
//+kubebuilder:printcolumn:name="S3",type=string,JSONPath=`.status.providers.s3.provider`
//+kubebuilder:printcolumn:name="Routing",type=string,JSONPath=`.status.providers.routing.provider`
//+kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// Tamoss is the Schema for the tamosses API
type Tamoss struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   TamossSpec   `json:"spec,omitempty"`
	Status TamossStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// TamossList contains a list of Tamoss
type TamossList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Tamoss `json:"items"`
}

func init() {
	SchemeBuilder.Register(func(s *runtime.Scheme) error {
		s.AddKnownTypes(SchemeGroupVersion, &Tamoss{}, &TamossList{})
		return nil
	})
}
