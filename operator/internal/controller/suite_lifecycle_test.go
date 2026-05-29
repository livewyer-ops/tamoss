package controller

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"sigs.k8s.io/controller-runtime/pkg/envtest"
)

func TestShouldStopTestEnv(t *testing.T) {
	tests := []struct {
		name    string
		env     *envtest.Environment
		started bool
		want    bool
	}{
		{name: "nil env", env: nil, started: true, want: false},
		{name: "not started", env: &envtest.Environment{}, started: false, want: false},
		{name: "started", env: &envtest.Environment{}, started: true, want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shouldStopTestEnv(tt.env, tt.started); got != tt.want {
				t.Fatalf("shouldStopTestEnv() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestLocalEnvtestAssetsDirectory(t *testing.T) {
	root := t.TempDir()
	assetDir := filepath.Join(root, "bin", "k8s", "1.29.5-"+runtime.GOOS+"-"+runtime.GOARCH)
	if err := os.MkdirAll(assetDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"etcd", "kube-apiserver", "kubectl"} {
		if err := os.WriteFile(filepath.Join(assetDir, name), []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	if got := localEnvtestAssetsDirectory(root); got != assetDir {
		t.Fatalf("localEnvtestAssetsDirectory() = %q, want %q", got, assetDir)
	}
}

func TestLocalEnvtestAssetsDirectoryRequiresCompleteBinarySet(t *testing.T) {
	root := t.TempDir()
	assetDir := filepath.Join(root, "bin", "k8s", "1.29.5-"+runtime.GOOS+"-"+runtime.GOARCH)
	if err := os.MkdirAll(assetDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(assetDir, "etcd"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	if got := localEnvtestAssetsDirectory(root); got != "" {
		t.Fatalf("localEnvtestAssetsDirectory() = %q, want empty path", got)
	}
}
