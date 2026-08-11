package consoleapi

const RuntimeSchemaVersion = "1.0"

// RuntimeSnapshot is a deliberately small, read-only projection of Kubernetes
// state. It avoids exposing workload specs, environment variables, Secret
// references, pod IPs, or arbitrary Kubernetes objects to the browser.
type RuntimeSnapshot struct {
	SchemaVersion  string            `json:"schemaVersion"`
	ObservedAt     string            `json:"observedAt"`
	Stale          bool              `json:"stale"`
	Instance       Instance          `json:"instance"`
	Workloads      []Workload        `json:"workloads"`
	Services       []Service         `json:"services"`
	EndpointSlices []EndpointSlice   `json:"endpointSlices"`
	Pods           []Pod             `json:"pods"`
	Jobs           []Job             `json:"jobs"`
	Events         []KubernetesEvent `json:"events"`
}

type Service struct {
	Name              string        `json:"name"`
	Component         string        `json:"component,omitempty"`
	Type              string        `json:"type"`
	SelectorComponent string        `json:"selectorComponent,omitempty"`
	Ports             []ServicePort `json:"ports"`
}

type ServicePort struct {
	Name       string `json:"name,omitempty"`
	Protocol   string `json:"protocol"`
	Port       int32  `json:"port"`
	TargetPort string `json:"targetPort"`
}

// EndpointSlice deliberately exposes readiness counts and resolved ports, not
// endpoint addresses, node names, zones, or target references.
type EndpointSlice struct {
	Name                 string              `json:"name"`
	ServiceName          string              `json:"serviceName"`
	Component            string              `json:"component,omitempty"`
	AddressType          string              `json:"addressType"`
	Ports                []EndpointSlicePort `json:"ports"`
	TotalEndpoints       int32               `json:"totalEndpoints"`
	ReadyEndpoints       int32               `json:"readyEndpoints"`
	NotReadyEndpoints    int32               `json:"notReadyEndpoints"`
	TerminatingEndpoints int32               `json:"terminatingEndpoints"`
}

type EndpointSlicePort struct {
	Name     string `json:"name,omitempty"`
	Protocol string `json:"protocol,omitempty"`
	Port     int32  `json:"port,omitempty"`
}

type Instance struct {
	Name               string              `json:"name"`
	Namespace          string              `json:"namespace"`
	UID                string              `json:"uid"`
	Generation         int64               `json:"generation"`
	ObservedGeneration int64               `json:"observedGeneration"`
	Phase              string              `json:"phase"`
	Conditions         []InstanceCondition `json:"conditions"`
}

type InstanceCondition struct {
	Type               string `json:"type"`
	Status             string `json:"status"`
	Reason             string `json:"reason,omitempty"`
	Message            string `json:"message,omitempty"`
	ObservedGeneration int64  `json:"observedGeneration,omitempty"`
	LastTransitionTime string `json:"lastTransitionTime,omitempty"`
}

type Workload struct {
	Kind               string              `json:"kind"`
	Name               string              `json:"name"`
	Component          string              `json:"component,omitempty"`
	Status             string              `json:"status"`
	Generation         int64               `json:"generation"`
	ObservedGeneration int64               `json:"observedGeneration"`
	DesiredReplicas    int32               `json:"desiredReplicas"`
	ReadyReplicas      int32               `json:"readyReplicas"`
	AvailableReplicas  int32               `json:"availableReplicas"`
	UpdatedReplicas    int32               `json:"updatedReplicas"`
	Conditions         []ResourceCondition `json:"conditions"`
}

type ResourceCondition struct {
	Type               string `json:"type"`
	Status             string `json:"status"`
	Reason             string `json:"reason,omitempty"`
	Message            string `json:"message,omitempty"`
	LastTransitionTime string `json:"lastTransitionTime,omitempty"`
}

type Pod struct {
	Name      string `json:"name"`
	Component string `json:"component,omitempty"`
	Phase     string `json:"phase"`
	Ready     bool   `json:"ready"`
	Restarts  int32  `json:"restarts"`
	Reason    string `json:"reason,omitempty"`
	Message   string `json:"message,omitempty"`
	StartedAt string `json:"startedAt,omitempty"`
	Deleting  bool   `json:"deleting"`
}

type Job struct {
	Name           string              `json:"name"`
	Component      string              `json:"component,omitempty"`
	Status         string              `json:"status"`
	Active         int32               `json:"active"`
	Succeeded      int32               `json:"succeeded"`
	Failed         int32               `json:"failed"`
	StartTime      string              `json:"startTime,omitempty"`
	CompletionTime string              `json:"completionTime,omitempty"`
	Conditions     []ResourceCondition `json:"conditions"`
}

type KubernetesEvent struct {
	Type            string          `json:"type"`
	Reason          string          `json:"reason,omitempty"`
	Message         string          `json:"message,omitempty"`
	Regarding       ObjectReference `json:"regarding"`
	Count           int32           `json:"count"`
	FirstObservedAt string          `json:"firstObservedAt,omitempty"`
	LastObservedAt  string          `json:"lastObservedAt,omitempty"`
}

type ObjectReference struct {
	Kind string `json:"kind"`
	Name string `json:"name"`
}
