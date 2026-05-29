package controller

import (
	"strings"
	"testing"

	tamossv1alpha1 "github.com/livewyer-ops/tamoss/operator/api/v1alpha1"
)

func TestPostgresPSQLScriptWrapsSharedWaitAndSQL(t *testing.T) {
	script := postgresPSQLScript([]string{`-v item="${ITEM}"`}, "SELECT :'item';")

	for _, expected := range []string{
		`pg_isready -h "${POSTGRES_HOST}"`,
		`psql "host=${POSTGRES_HOST} port=${POSTGRES_PORT} dbname=${POSTGRES_DB} user=${POSTGRES_USER}"`,
		`-v item="${ITEM}"`,
		"SELECT :'item';",
	} {
		if !strings.Contains(script, expected) {
			t.Fatalf("expected script to contain %q, got %s", expected, script)
		}
	}
}

func TestStorageBackendDatabaseScriptsUseSharedPostgresWrapper(t *testing.T) {
	for name, script := range map[string]string{
		"register":   storageBackendRegistrationScript(),
		"deregister": storageBackendDeregistrationScript(),
	} {
		if !strings.Contains(script, postgresWaitForReadyScript()) {
			t.Fatalf("expected %s script to include shared Postgres wait block", name)
		}
		if !strings.Contains(script, "tamoss_storage_backends") {
			t.Fatalf("expected %s script to touch tamoss_storage_backends, got %s", name, script)
		}
	}
	if !strings.Contains(storageBackendRegistrationScript(), "BEGIN;") ||
		!strings.Contains(storageBackendRegistrationScript(), "COMMIT;") {
		t.Fatalf("expected registration script to run in a transaction: %s", storageBackendRegistrationScript())
	}
	if !strings.Contains(storageBackendDeregistrationScript(), "to_regclass('public.tamoss_storage_backends')") {
		t.Fatalf("expected deregistration script to tolerate missing schema: %s", storageBackendDeregistrationScript())
	}
}

func TestSchemaMigrationJobUsesRuntimeMigrationCommand(t *testing.T) {
	tamoss := &tamossv1alpha1.Tamoss{
		Spec: tamossv1alpha1.TamossSpec{
			API: tamossv1alpha1.APIComponentSpec{
				Image: tamossv1alpha1.ImageSpec{
					Repository: "registry.example.com/tamoss-api",
					Tag:        "v1.2.3",
				},
			},
			Backends: tamossv1alpha1.BackendsSpec{
				DB: tamossv1alpha1.DBBackendSpec{ProvidedBy: tamossv1alpha1.BackendProvidedByCNPG},
			},
		},
	}

	job := schemaMigrationJob(tamoss, true)
	container := job.Spec.Template.Spec.Containers[0]

	if image := container.Image; image != "registry.example.com/tamoss-api:v1.2.3" {
		t.Fatalf("expected configured API image, got %q", image)
	}
	if got := strings.Join(append(container.Command, container.Args...), " "); got != "uv run tamoss-db migrate --apply-fixtures --apply-cnpg-ownership" {
		t.Fatalf("expected migration CLI command, got %q", got)
	}
}
