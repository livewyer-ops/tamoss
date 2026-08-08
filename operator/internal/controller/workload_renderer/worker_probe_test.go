package workload_renderer

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"

	"github.com/livewyer-ops/tamoss/operator/internal/controller/defaults"
)

func TestWorkerDeploymentRendersHTTPProbes(t *testing.T) {
	tamoss := rendererFixture()
	tamoss.Spec.Worker.Enabled = ptr.To(true)
	defaults.Apply(tamoss)

	deployment := deploymentByName(t, Render(tamoss), "example-worker")
	container := deployment.Spec.Template.Spec.Containers[0]
	assertHTTPProbe(t, "readiness", container.ReadinessProbe, "/readyz")
	assertHTTPProbe(t, "liveness", container.LivenessProbe, "/healthz")
	assertHTTPProbe(t, "startup", container.StartupProbe, "/healthz")
}

func assertHTTPProbe(t *testing.T, name string, probe *corev1.Probe, wantPath string) {
	t.Helper()
	if probe == nil || probe.HTTPGet == nil {
		t.Fatalf("expected %s HTTP probe", name)
	}
	if probe.HTTPGet.Path != wantPath {
		t.Fatalf("expected %s probe path %q, got %q", name, wantPath, probe.HTTPGet.Path)
	}
	if probe.HTTPGet.Port.StrVal != "metrics" {
		t.Fatalf("expected %s probe port metrics, got %#v", name, probe.HTTPGet.Port)
	}
	if probe.TimeoutSeconds != 5 {
		t.Fatalf("expected %s probe timeout 5, got %d", name, probe.TimeoutSeconds)
	}
}
