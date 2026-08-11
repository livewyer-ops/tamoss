package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
)

type ServiceAccountSpec struct {
	//+kubebuilder:default=true
	Create bool `json:"create,omitempty"`
	//+kubebuilder:default=false
	Automount   bool              `json:"automount,omitempty"`
	Annotations map[string]string `json:"annotations,omitempty"`
	Name        string            `json:"name,omitempty"`
}

type ServiceSpec struct {
	//+kubebuilder:default=true
	Enabled bool `json:"enabled,omitempty"`
	//+kubebuilder:validation:Enum=ClusterIP;NodePort;LoadBalancer;ExternalName
	//+kubebuilder:default=ClusterIP
	Type    corev1.ServiceType `json:"type,omitempty"`
	API     ServicePortsSpec   `json:"api,omitempty"`
	UI      ServicePortsSpec   `json:"ui,omitempty"`
	Console ServicePortsSpec   `json:"console,omitempty"`
}

type ServicePortsSpec struct {
	Ports []corev1.ServicePort `json:"ports,omitempty"`
}

type NetworkPolicySpec struct {
	Enabled *bool                  `json:"enabled,omitempty"`
	API     NetworkPolicyRulesSpec `json:"api,omitempty"`
	UI      NetworkPolicyRulesSpec `json:"ui,omitempty"`
	Worker  NetworkPolicyRulesSpec `json:"worker,omitempty"`
	Console NetworkPolicyRulesSpec `json:"console,omitempty"`
	// KubernetesAPIIPBlocks contains the Kubernetes Service and API-server
	// endpoint CIDRs that an enabled Console may reach. It is an optional
	// tightening: when the list is empty the Console's Kubernetes API egress
	// rule is still rendered but permits any destination on those ports, because
	// the addresses are cluster-specific and cannot be derived from a Tamoss
	// spec. Destination-scoped egress is otherwise deferred; see
	// docs/development/ui-overhaul/0002-console-api-and-kubernetes.md.
	//+kubebuilder:validation:MaxItems=16
	KubernetesAPIIPBlocks []networkingv1.IPBlock `json:"kubernetesAPIIPBlocks,omitempty"`
}

type NetworkPolicyRulesSpec struct {
	Ingress []networkingv1.NetworkPolicyIngressRule `json:"ingress,omitempty"`
	Egress  []networkingv1.NetworkPolicyEgressRule  `json:"egress,omitempty"`
}

type IngressSpec struct {
	Enabled     *bool                     `json:"enabled,omitempty"`
	ClassName   string                    `json:"className,omitempty"`
	Annotations map[string]string         `json:"annotations,omitempty"`
	TLS         []networkingv1.IngressTLS `json:"tls,omitempty"`
	API         IngressHostSpec           `json:"api,omitempty"`
	UI          UIIngressSpec             `json:"ui,omitempty"`
}

type IngressHostSpec struct {
	Host  string                         `json:"host,omitempty"`
	Paths []networkingv1.HTTPIngressPath `json:"paths,omitempty"`
}

type UIIngressSpec struct {
	Web IngressHostSpec `json:"web,omitempty"`
}

type HTTPRouteSpec struct {
	//+kubebuilder:default=false
	Enabled     bool                 `json:"enabled,omitempty"`
	Annotations map[string]string    `json:"annotations,omitempty"`
	ParentRefs  []HTTPRouteParentRef `json:"parentRefs,omitempty"`
	API         HTTPRouteHostSpec    `json:"api,omitempty"`
	UI          HTTPRouteHostSpec    `json:"ui,omitempty"`
}

type HTTPRouteParentRef struct {
	Name        string `json:"name,omitempty"`
	Namespace   string `json:"namespace,omitempty"`
	Kind        string `json:"kind,omitempty"`
	SectionName string `json:"sectionName,omitempty"`
}

type HTTPRouteHostSpec struct {
	Hostnames      []string               `json:"hostnames,omitempty"`
	DefaultFilters []apiextensionsv1.JSON `json:"defaultFilters,omitempty"`
	Filters        []apiextensionsv1.JSON `json:"filters,omitempty"`
	Rules          []HTTPRouteRule        `json:"rules,omitempty"`
}

type HTTPRouteRule struct {
	Matches     []HTTPRouteMatch       `json:"matches,omitempty"`
	BackendRefs []HTTPRouteBackendRef  `json:"backendRefs,omitempty"`
	Filters     []apiextensionsv1.JSON `json:"filters,omitempty"`
}

type HTTPRouteMatch struct {
	Path *HTTPRoutePathMatch `json:"path,omitempty"`
}

type HTTPRoutePathMatch struct {
	//+kubebuilder:validation:Enum=Exact;PathPrefix;RegularExpression
	Type  string `json:"type,omitempty"`
	Value string `json:"value,omitempty"`
}

type HTTPRouteBackendRef struct {
	Name      string `json:"name,omitempty"`
	Namespace string `json:"namespace,omitempty"`
	Port      int32  `json:"port,omitempty"`
	Weight    *int32 `json:"weight,omitempty"`
}

type SecretsSpec struct {
	APIToken APITokenSecretSpec `json:"apiToken,omitempty"`
}

// +kubebuilder:validation:XValidation:rule="self.generate || (has(self.token) && self.token.size() > 0)",message="token is required when generate is false"
type APITokenSecretSpec struct {
	//+kubebuilder:default=true
	Generate bool   `json:"generate,omitempty"`
	Token    string `json:"token,omitempty"`
}
