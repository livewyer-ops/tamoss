package workload_renderer

import (
	"testing"

	tamossv1alpha1 "github.com/livewyer-ops/tamoss/operator/api/v1alpha1"
)

func TestServiceIdentityAbsentWhenUnset(t *testing.T) {
	objects := Render(rendererFixture())

	env := deploymentByName(t, objects, "example-api").Spec.Template.Spec.Containers[0].Env
	for _, name := range []string{
		"TAMOSS_SERVICE_IDENTITY_MANAGED",
		"TAMOSS_SERVICE_NAME",
		"TAMOSS_SERVICE_DESCRIPTION",
	} {
		if hasEnv(env, name) {
			t.Fatalf("%s should be absent while spec.serviceIdentity is empty", name)
		}
	}
}

func TestServiceIdentityPublishedToAPI(t *testing.T) {
	tamoss := rendererFixture()
	tamoss.Spec.ServiceIdentity = tamossv1alpha1.ServiceIdentitySpec{
		Name:        "Reuters External",
		Description: "External-facing wire content, retained for 90 days.",
	}

	objects := Render(tamoss)

	env := deploymentByName(t, objects, "example-api").Spec.Template.Spec.Containers[0].Env
	if got := envValue(env, "TAMOSS_SERVICE_IDENTITY_MANAGED"); got != "true" {
		t.Fatalf("expected identity to be managed, got %q", got)
	}
	if got := envValue(env, "TAMOSS_SERVICE_NAME"); got != "Reuters External" {
		t.Fatalf("unexpected service name %q", got)
	}
	if got := envValue(env, "TAMOSS_SERVICE_DESCRIPTION"); got != "External-facing wire content, retained for 90 days." {
		t.Fatalf("unexpected service description %q", got)
	}
}

// A description on its own still claims identity, but must not blank the name:
// the API keeps its built-in default for whichever field the spec omits.
func TestServiceIdentityOmitsUnsetField(t *testing.T) {
	tamoss := rendererFixture()
	tamoss.Spec.ServiceIdentity = tamossv1alpha1.ServiceIdentitySpec{
		Description: "External-facing wire content.",
	}

	objects := Render(tamoss)

	env := deploymentByName(t, objects, "example-api").Spec.Template.Spec.Containers[0].Env
	if got := envValue(env, "TAMOSS_SERVICE_IDENTITY_MANAGED"); got != "true" {
		t.Fatalf("expected identity to be managed, got %q", got)
	}
	if hasEnv(env, "TAMOSS_SERVICE_NAME") {
		t.Fatal("TAMOSS_SERVICE_NAME should be absent when the spec omits the name")
	}
}

func TestServiceIdentityNotOverridableByAPIEnv(t *testing.T) {
	tamoss := rendererFixture()
	tamoss.Spec.ServiceIdentity = tamossv1alpha1.ServiceIdentitySpec{Name: "Reuters External"}
	tamoss.Spec.API.Env = map[string]string{
		"TAMOSS_SERVICE_NAME":             "Impostor",
		"TAMOSS_SERVICE_IDENTITY_MANAGED": "false",
	}

	objects := Render(tamoss)

	env := deploymentByName(t, objects, "example-api").Spec.Template.Spec.Containers[0].Env
	if got := envCount(env, "TAMOSS_SERVICE_NAME"); got != 1 {
		t.Fatalf("expected exactly one TAMOSS_SERVICE_NAME, got %d", got)
	}
	if got := envValue(env, "TAMOSS_SERVICE_NAME"); got != "Reuters External" {
		t.Fatalf("spec.api.env must not override managed identity, got %q", got)
	}
	if got := envValue(env, "TAMOSS_SERVICE_IDENTITY_MANAGED"); got != "true" {
		t.Fatalf("spec.api.env must not clear the managed flag, got %q", got)
	}
}

func TestServiceIdentityIsManaged(t *testing.T) {
	cases := map[string]struct {
		identity tamossv1alpha1.ServiceIdentitySpec
		want     bool
	}{
		"empty":            {tamossv1alpha1.ServiceIdentitySpec{}, false},
		"name only":        {tamossv1alpha1.ServiceIdentitySpec{Name: "Reuters External"}, true},
		"description only": {tamossv1alpha1.ServiceIdentitySpec{Description: "Wire content."}, true},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if got := tc.identity.IsManaged(); got != tc.want {
				t.Fatalf("IsManaged() = %v, want %v", got, tc.want)
			}
		})
	}
}
