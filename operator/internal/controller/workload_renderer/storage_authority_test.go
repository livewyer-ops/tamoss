package workload_renderer

import (
	"testing"

	"k8s.io/utils/ptr"
)

func TestRenderDisablesRuntimeStorageBackendRegistration(t *testing.T) {
	tamoss := rendererFixture()
	tamoss.Spec.Worker.Enabled = ptr.To(true)

	objects := Render(tamoss)
	for _, name := range []string{"example-api", "example-worker"} {
		deployment := deploymentByName(t, objects, name)
		container := deployment.Spec.Template.Spec.Containers[0]
		if got := envValue(container.Env, "TAMOSS_STORAGE_BACKEND_REGISTRATION_ENABLED"); got != "false" {
			t.Fatalf("%s should disable runtime storage backend registration, got %q", name, got)
		}
	}
}
