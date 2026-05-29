package discovery

import "k8s.io/apimachinery/pkg/runtime/schema"

const CNPGInstallCommand = "kubectl apply --server-side -k deploy/platform/components/cnpg"

const (
	RustFSOperatorChartVersion = "0.1.0"
	RustFSOperatorCommit       = "ff80d847806eb7cfc9c4a33769715a6b0f3145dd"
)

const RustFSOperatorInstallCommand = "kubectl apply --server-side -k deploy/platform/components/rustfs-operator"

var CNPGClustersGVR = schema.GroupVersionResource{
	Group:    "postgresql.cnpg.io",
	Version:  "v1",
	Resource: "clusters",
}

var CNPGScheduledBackupsGVR = schema.GroupVersionResource{
	Group:    "postgresql.cnpg.io",
	Version:  "v1",
	Resource: "scheduledbackups",
}

var RustFSTenantsGVR = schema.GroupVersionResource{
	Group:    "rustfs.com",
	Version:  "v1alpha1",
	Resource: "tenants",
}

var GatewayHTTPRoutesGVR = schema.GroupVersionResource{
	Group:    "gateway.networking.k8s.io",
	Version:  "v1",
	Resource: "httproutes",
}

func OptionalResourceGVRs() []schema.GroupVersionResource {
	return []schema.GroupVersionResource{
		CNPGClustersGVR,
		CNPGScheduledBackupsGVR,
		RustFSTenantsGVR,
		GatewayHTTPRoutesGVR,
	}
}

type RemediationHint struct {
	ProvidedBy     string
	GVR            schema.GroupVersionResource
	DependencyName string
	InstallCommand string
}

var remediationHints = map[string]RemediationHint{
	"cnpg": {
		ProvidedBy:     "cnpg",
		GVR:            CNPGClustersGVR,
		DependencyName: "CloudNativePG",
		InstallCommand: CNPGInstallCommand,
	},
	"rustfs-operator": {
		ProvidedBy:     "rustfs-operator",
		GVR:            RustFSTenantsGVR,
		DependencyName: "RustFS Operator",
		InstallCommand: RustFSOperatorInstallCommand,
	},
}

func HintFor(providedBy string) (RemediationHint, bool) {
	hint, ok := remediationHints[providedBy]
	return hint, ok
}
