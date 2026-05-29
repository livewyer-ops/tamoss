package workload_renderer

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	yamlv3 "gopkg.in/yaml.v3"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"

	tamossv1alpha1 "github.com/livewyer-ops/tamoss/operator/api/v1alpha1"
	"github.com/livewyer-ops/tamoss/operator/internal/controller/defaults"
)

func TestKindSampleRendersOperatorOnlyResources(t *testing.T) {
	repoRoot := filepath.Clean(filepath.Join("..", "..", "..", ".."))
	samplePath := filepath.Join(repoRoot, "deploy", "instances", "local-kind", "tamoss.yaml")
	tamoss := readTamossSample(t, samplePath)
	defaults.Apply(tamoss)
	applySampleDefaults(tamoss)

	want := []string{
		"Deployment/api",
		"Deployment/ui",
		"Deployment/worker",
		"Ingress/api",
		"Ingress/ui",
		"Secret/api-token",
		"Secret/backends",
		"Service/api",
		"Service/ui",
		"ServiceAccount/workload",
	}
	sort.Strings(want)
	assertIDsEqual(t, "renderer", want, logicalIDsFromObjects(Render(tamoss), tamoss.ResourceName("")))
}

func readTamossSample(t *testing.T, path string) *tamossv1alpha1.Tamoss {
	t.Helper()
	sampleBytes, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	decoder := yamlv3.NewDecoder(bytes.NewReader(sampleBytes))
	for {
		doc := map[string]interface{}{}
		err := decoder.Decode(&doc)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil || len(doc) == 0 || doc["kind"] != "Tamoss" {
			continue
		}
		docBytes, err := yaml.Marshal(doc)
		if err != nil {
			t.Fatal(err)
		}
		tamoss := &tamossv1alpha1.Tamoss{}
		if err := yaml.Unmarshal(docBytes, tamoss); err != nil {
			t.Fatal(err)
		}
		return tamoss
	}
	t.Fatalf("no Tamoss document found in %s", path)
	return nil
}

func applySampleDefaults(tamoss *tamossv1alpha1.Tamoss) {
	if tamoss.Spec.Service.Type == "" {
		tamoss.Spec.Service.Type = corev1.ServiceTypeClusterIP
	}
	tamoss.Spec.Service.Enabled = true
	tamoss.Spec.ServiceAccount.Create = true
	tamoss.Spec.Secrets.APIToken.Generate = true
}

func logicalIDsFromObjects(objects []client.Object, base string) []string {
	ids := make([]string, 0, len(objects))
	for _, obj := range objects {
		if id := logicalID(kindForObject(obj), obj.GetName(), base); id != "" {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	return ids
}

func kindForObject(obj client.Object) string {
	id := objectID(obj)
	if before, _, ok := strings.Cut(id, "/"); ok {
		return before
	}
	return ""
}

func logicalID(kind, name, base string) string {
	switch kind {
	case "Deployment":
		switch name {
		case base, base + "-api":
			return "Deployment/api"
		case base + "-ui":
			return "Deployment/ui"
		case base + "-worker":
			return "Deployment/worker"
		}
	case "HorizontalPodAutoscaler":
		return componentSuffixID(kind, name, base)
	case "HTTPRoute":
		if name == base+"-web" || name == base+"-ui" {
			return "HTTPRoute/ui"
		}
		return componentSuffixID(kind, name, base)
	case "Ingress", "NetworkPolicy", "PodDisruptionBudget", "Service":
		return componentSuffixID(kind, name, base)
	case "Secret":
		switch name {
		case base + "-api-token":
			return "Secret/api-token"
		case base + "-backends":
			return "Secret/backends"
		}
	case "ServiceAccount":
		if name == base || name == base+"-workload" {
			return "ServiceAccount/workload"
		}
	}
	return ""
}

func componentSuffixID(kind, name, base string) string {
	for _, component := range []string{"api", "ui", "worker"} {
		if name == base+"-"+component {
			return kind + "/" + component
		}
	}
	return ""
}

func assertIDsEqual(t *testing.T, source string, want, got []string) {
	t.Helper()
	if strings.Join(want, "\n") == strings.Join(got, "\n") {
		return
	}
	t.Fatalf("%s logical resources mismatch\nwant:\n%s\n\ngot:\n%s", source, strings.Join(want, "\n"), strings.Join(got, "\n"))
}
