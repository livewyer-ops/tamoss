package workload_renderer

import (
	"reflect"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"

	"github.com/livewyer-ops/tamoss/operator/internal/controller/defaults"
)

func TestWorkerDeploymentRendersHealthCommandProbes(t *testing.T) {
	tamoss := rendererFixture()
	tamoss.Spec.Worker.Enabled = ptr.To(true)
	defaults.Apply(tamoss)

	deployment := deploymentByName(t, Render(tamoss), "example-worker")
	container := deployment.Spec.Template.Spec.Containers[0]
	want := []string{"/bin/uv", "run", "python", "-m", "tamoss.worker", "health"}

	assertProbeCommand(t, "readiness", container.ReadinessProbe, want)
	assertProbeCommand(t, "liveness", container.LivenessProbe, want)
	assertProbeCommand(t, "startup", container.StartupProbe, want)
}

func assertProbeCommand(t *testing.T, name string, probe *corev1.Probe, want []string) {
	t.Helper()
	if probe == nil || probe.Exec == nil {
		t.Fatalf("expected %s probe exec command", name)
	}
	if !reflect.DeepEqual(probe.Exec.Command, want) {
		t.Fatalf("expected %s probe command %#v, got %#v", name, want, probe.Exec.Command)
	}
}
