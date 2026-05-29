package controller

import (
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

func envtestAssetsDirectory() string {
	if os.Getenv("KUBEBUILDER_ASSETS") != "" {
		return ""
	}
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return ""
	}
	return localEnvtestAssetsDirectory(filepath.Join(filepath.Dir(file), "..", ".."))
}

func localEnvtestAssetsDirectory(operatorRoot string) string {
	root := filepath.Join(operatorRoot, "bin", "k8s")
	var candidates []string
	_ = filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || !entry.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, runtime.GOOS+"-"+runtime.GOARCH) {
			return nil
		}
		if hasEnvtestBinaries(path) {
			candidates = append(candidates, path)
			return filepath.SkipDir
		}
		return nil
	})
	sort.Strings(candidates)
	if len(candidates) == 0 {
		return ""
	}
	return candidates[len(candidates)-1]
}

func hasEnvtestBinaries(path string) bool {
	for _, name := range []string{"etcd", "kube-apiserver", "kubectl"} {
		info, err := os.Stat(filepath.Join(path, name))
		if err != nil || info.IsDir() {
			return false
		}
	}
	return true
}
