package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

// TamossSpec defines the desired state of Tamoss
// +kubebuilder:validation:XValidation:rule="has(self.fullnameOverride) == has(oldSelf.fullnameOverride) && (!has(self.fullnameOverride) || self.fullnameOverride == oldSelf.fullnameOverride)",message="spec.fullnameOverride is immutable"
type TamossSpec struct {
	// Paused stops reconcile writes while still allowing status updates.
	//+kubebuilder:default=false
	Paused bool `json:"paused,omitempty"`

	// Profile selects operator-owned defaults for a common installation shape.
	// Explicit fields in the Tamoss spec override profile defaults.
	//+kubebuilder:validation:Enum=local-kind;single-server;multi-server;edge
	Profile TamossProfile `json:"profile,omitempty"`

	// PublicEndpoint defines the public DNS defaults used by profile defaulting.
	PublicEndpoint PublicEndpointSpec `json:"publicEndpoint,omitempty"`

	// ServiceIdentity sets the human-readable name and description this
	// instance advertises on the TAMS `/service` endpoint. Setting either field
	// makes identity operator-managed: the API serves these values and rejects
	// `POST /service`. Leaving both empty keeps identity settable through the
	// API. This is unrelated to the Tamoss resource name, which is a Kubernetes
	// object name.
	ServiceIdentity ServiceIdentitySpec `json:"serviceIdentity,omitempty"`

	NameOverride     string `json:"nameOverride,omitempty"`
	FullnameOverride string `json:"fullnameOverride,omitempty"`

	//+kubebuilder:default={image:{repository:livewyer/tamoss-api,pullPolicy:IfNotPresent}}
	API APIComponentSpec `json:"api,omitempty"`
	//+kubebuilder:default={}
	Worker WorkerComponentSpec `json:"worker,omitempty"`
	//+kubebuilder:default={image:{repository:livewyer/tamoss-ui,pullPolicy:IfNotPresent}}
	UI UIComponentSpec `json:"ui,omitempty"`
	// Console exposes the namespace- and instance-scoped Kubernetes runtime API
	// used by the UI. It remains opt-in until end-user authorization is enforced.
	//+kubebuilder:default={image:{repository:livewyer/tamoss-console-api,pullPolicy:IfNotPresent}}
	Console ConsoleComponentSpec `json:"console,omitempty"`

	Images ComponentImagesSpec `json:"images,omitempty"`

	Backends BackendsSpec `json:"backends,omitempty"`
	//+kubebuilder:default={providedBy:external,required:true,trustForwardAuthHeaders:false,external:{oauth2:{enabled:false,algorithms:{RS256}}}}
	Auth AuthSpec `json:"auth,omitempty"`

	// Hibernation declares the desired lifecycle state of this instance and,
	// optionally, a hibernation artifact to bootstrap the database from.
	Hibernation TamossHibernationSpec `json:"hibernation,omitempty"`

	// Ingest approves the media an IngestRun may read. An empty list resolves
	// no input at all, which is the safe default.
	Ingest IngestSpec `json:"ingest,omitempty"`

	Ingress IngressSpec `json:"ingress,omitempty"`
	//+kubebuilder:default={enabled:false}
	HTTPRoute     HTTPRouteSpec     `json:"httpRoute,omitempty"`
	NetworkPolicy NetworkPolicySpec `json:"networkPolicy,omitempty"`
	//+kubebuilder:default={enabled:true,type:ClusterIP}
	Service ServiceSpec `json:"service,omitempty"`
	//+kubebuilder:default={create:true,automount:false}
	ServiceAccount ServiceAccountSpec `json:"serviceAccount,omitempty"`
	//+kubebuilder:default={apiToken:{generate:true}}
	Secrets SecretsSpec `json:"secrets,omitempty"`

	ImagePullSecrets []corev1.LocalObjectReference `json:"imagePullSecrets,omitempty"`

	// Advanced provides explicit operator-facing escape hatches for emitted resources.
	// Prefer first-class spec fields when they exist.
	Advanced AdvancedSpec `json:"advanced,omitempty"`
}

type TamossProfile string

const (
	TamossProfileLocalKind    TamossProfile = "local-kind"
	TamossProfileSingleServer TamossProfile = "single-server"
	TamossProfileMultiServer  TamossProfile = "multi-server"
	TamossProfileEdge         TamossProfile = "edge"
)

type PublicEndpointSpec struct {
	// BaseDomain is used to derive api.<baseDomain>, app.<baseDomain>, s3.<baseDomain>, and auth.<baseDomain>.
	//+kubebuilder:validation:MinLength=1
	BaseDomain string `json:"baseDomain,omitempty"`
	// UIURL is the exact public UI origin, including a non-standard external port when required.
	//+kubebuilder:validation:MinLength=1
	//+kubebuilder:validation:Pattern=`^https?://[^/?#]+/?$`
	UIURL string `json:"uiURL,omitempty"`
	// TLSSecretName is the default TLS Secret for API and UI public routes.
	TLSSecretName string `json:"tlsSecretName,omitempty"`
	// S3TLSSecretName is the default TLS Secret for managed S3 public routes.
	S3TLSSecretName string `json:"s3TLSSecretName,omitempty"`
}

// ServiceIdentitySpec is the editorial identity advertised on `/service`.
// It describes what the store holds or who owns it, and changes on a different
// cadence to the deployment itself.
type ServiceIdentitySpec struct {
	// Name is a very short human-readable label shown in listings of TAMS
	// service instances, such as "Reuters External".
	//+kubebuilder:validation:MaxLength=64
	Name string `json:"name,omitempty"`
	// Description is a longer explanation shown in detailed views.
	//+kubebuilder:validation:MaxLength=1024
	Description string `json:"description,omitempty"`
}

type AdvancedSpec struct {
	// ResourcePatches are JSON merge patches applied to matching TAMOSS-emitted resources before server-side apply.
	ResourcePatches []AdvancedResourcePatch `json:"resourcePatches,omitempty"`
	// ExtraResources are additional Kubernetes resources owned by this Tamoss instance.
	ExtraResources []apiextensionsv1.JSON `json:"extraResources,omitempty"`
}

type AdvancedResourcePatch struct {
	// Target selects the emitted resource to patch.
	Target AdvancedResourcePatchTarget `json:"target"`
	// Patch is a JSON merge patch. It may set metadata.labels, metadata.annotations, and resource spec fields.
	Patch apiextensionsv1.JSON `json:"patch"`
}

type AdvancedResourcePatchTarget struct {
	// APIVersion optionally disambiguates resources with the same kind and name.
	APIVersion string `json:"apiVersion,omitempty"`
	// Kind is the Kubernetes kind to patch.
	//+kubebuilder:validation:MinLength=1
	Kind string `json:"kind"`
	// Name is the generated resource name to patch.
	//+kubebuilder:validation:MinLength=1
	Name string `json:"name"`
}

type ImageSpec struct {
	//+kubebuilder:validation:MinLength=1
	Repository string `json:"repository,omitempty"`
	Tag        string `json:"tag,omitempty"`
	//+kubebuilder:validation:Enum=Always;IfNotPresent;Never
	//+kubebuilder:default=IfNotPresent
	PullPolicy corev1.PullPolicy `json:"pullPolicy,omitempty"`
}

type ComponentImagesSpec struct {
	// SchemaMigrationPostgresClient is the image used by storage backend database registration Jobs.
	SchemaMigrationPostgresClient string `json:"schemaMigrationPostgresClient,omitempty"`
}

type WorkloadCommonSpec struct {
	//+kubebuilder:validation:Minimum=0
	ReplicaCount *int32 `json:"replicaCount,omitempty"`

	Env     map[string]string      `json:"env,omitempty"`
	EnvFrom []corev1.EnvFromSource `json:"envFrom,omitempty"`

	PodAnnotations map[string]string `json:"podAnnotations,omitempty"`
	PodLabels      map[string]string `json:"podLabels,omitempty"`

	PodSecurityContext *corev1.PodSecurityContext  `json:"podSecurityContext,omitempty"`
	SecurityContext    *corev1.SecurityContext     `json:"securityContext,omitempty"`
	Resources          corev1.ResourceRequirements `json:"resources,omitempty"`

	LivenessProbe  *corev1.Probe `json:"livenessProbe,omitempty"`
	ReadinessProbe *corev1.Probe `json:"readinessProbe,omitempty"`
	StartupProbe   *corev1.Probe `json:"startupProbe,omitempty"`

	//+kubebuilder:validation:Minimum=0
	TerminationGracePeriodSeconds *int64 `json:"terminationGracePeriodSeconds,omitempty"`
	//+kubebuilder:validation:Minimum=0
	PreStopSleepSeconds *int32 `json:"preStopSleepSeconds,omitempty"`

	PDB         PDBSpec         `json:"pdb,omitempty"`
	Autoscaling AutoscalingSpec `json:"autoscaling,omitempty"`

	Volumes      []corev1.Volume      `json:"volumes,omitempty"`
	VolumeMounts []corev1.VolumeMount `json:"volumeMounts,omitempty"`
	NodeSelector map[string]string    `json:"nodeSelector,omitempty"`
	Tolerations  []corev1.Toleration  `json:"tolerations,omitempty"`
	Affinity     *corev1.Affinity     `json:"affinity,omitempty"`
}

type APIComponentSpec struct {
	Enabled *bool `json:"enabled,omitempty"`
	//+kubebuilder:default={repository:livewyer/tamoss-api,pullPolicy:IfNotPresent}
	Image              ImageSpec   `json:"image,omitempty"`
	CORS               APICORSSpec `json:"cors,omitempty"`
	WorkloadCommonSpec `json:",inline"`
}

type APICORSSpec struct {
	// AllowedOrigins lists exact browser origins allowed by TAMOSS-managed CORS.
	AllowedOrigins []string `json:"allowedOrigins,omitempty"`
	// AllowedOriginRegexes lists browser origin regexes allowed by TAMOSS-managed CORS where regex matching is supported.
	// These regexes are applied to the API runtime and Traefik CORS middleware for managed S3 ingress, but not to bucket CORS rules.
	AllowedOriginRegexes []string `json:"allowedOriginRegexes,omitempty"`
}

type WorkerComponentSpec struct {
	Enabled            *bool `json:"enabled,omitempty"`
	WorkloadCommonSpec `json:",inline"`
}

type UIComponentSpec struct {
	Enabled *bool `json:"enabled,omitempty"`
	//+kubebuilder:default={repository:livewyer/tamoss-ui,pullPolicy:IfNotPresent}
	Image              ImageSpec              `json:"image,omitempty"`
	Ports              []corev1.ContainerPort `json:"ports,omitempty"`
	WorkloadCommonSpec `json:",inline"`
}

type ConsoleComponentSpec struct {
	//+kubebuilder:default=false
	Enabled *bool `json:"enabled,omitempty"`
	//+kubebuilder:default={repository:livewyer/tamoss-console-api,pullPolicy:IfNotPresent}
	Image              ImageSpec `json:"image,omitempty"`
	WorkloadCommonSpec `json:",inline"`
}

type PDBSpec struct {
	Enabled        *bool               `json:"enabled,omitempty"`
	MinAvailable   *intstr.IntOrString `json:"minAvailable,omitempty"`
	MaxUnavailable *intstr.IntOrString `json:"maxUnavailable,omitempty"`
}

// +kubebuilder:validation:XValidation:rule="!self.enabled || self.maxReplicas >= self.minReplicas",message="maxReplicas must be greater than or equal to minReplicas when autoscaling is enabled"
type AutoscalingSpec struct {
	//+kubebuilder:default=false
	Enabled bool `json:"enabled,omitempty"`
	//+kubebuilder:validation:Minimum=1
	//+kubebuilder:default=1
	MinReplicas int32 `json:"minReplicas,omitempty"`
	//+kubebuilder:validation:Minimum=1
	//+kubebuilder:default=100
	MaxReplicas int32 `json:"maxReplicas,omitempty"`
	//+kubebuilder:validation:Minimum=1
	//+kubebuilder:validation:Maximum=100
	TargetCPUUtilizationPercentage *int32 `json:"targetCPUUtilizationPercentage,omitempty"`
	//+kubebuilder:validation:Minimum=1
	//+kubebuilder:validation:Maximum=100
	TargetMemoryUtilizationPercentage *int32 `json:"targetMemoryUtilizationPercentage,omitempty"`
}
