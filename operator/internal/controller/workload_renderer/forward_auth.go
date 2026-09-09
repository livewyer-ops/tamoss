package workload_renderer

import (
	"encoding/json"
	"sort"
	"strings"
	"unicode"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	tamossv1alpha1 "github.com/livewyer-ops/tamoss/operator/api/v1alpha1"
	"github.com/livewyer-ops/tamoss/operator/internal/controller/auth/authentik"
)

const (
	ForwardAuthAPIProofSecretKey     = "api-proof"
	ForwardAuthConsoleProofSecretKey = "console-proof"

	forwardAuthProofVolumeName  = "forward-auth-proof"
	forwardAuthProofMountPath   = "/run/tamoss/forward-auth"
	maxForwardAuthGroupBindings = 256
)

var validForwardAuthPermissions = map[string]struct{}{
	"admin":         {},
	"viewer":        {},
	"operator":      {},
	"ingest-runner": {},
}

type normalizedForwardAuthBinding struct {
	GroupName   string   `json:"groupName"`
	Permissions []string `json:"permissions"`
}

func ForwardAuthProofSecretName(tamoss *tamossv1alpha1.Tamoss) string {
	return tamoss.ResourceName("forward-auth")
}

func ForwardAuthProofFilePath(key string) string {
	return forwardAuthProofMountPath + "/" + key
}

func forwardAuthEnabled(tamoss *tamossv1alpha1.Tamoss) bool {
	_, valid := normalizedForwardAuthGroupBindings(tamoss)
	return authentik.ForwardAuthRequired(tamoss) && valid
}

// BrowserAuthConfigured reports whether enabled browser surfaces have a
// supported authentication path. Machine-authenticated API access is separate.
func BrowserAuthConfigured(tamoss *tamossv1alpha1.Tamoss) bool {
	if !tamoss.Spec.UI.IsEnabled() && !tamoss.Spec.ConsoleEnabled() {
		return true
	}
	if !tamoss.Spec.Auth.RequiredForRuntime() {
		return true
	}
	return forwardAuthEnabled(tamoss)
}

func forwardAuthProofSecret(tamoss *tamossv1alpha1.Tamoss) *corev1.Secret {
	if !forwardAuthEnabled(tamoss) {
		return nil
	}
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ForwardAuthProofSecretName(tamoss),
			Namespace: tamoss.Namespace,
			Labels:    labels(tamoss, "forward-auth"),
		},
		Type: corev1.SecretTypeOpaque,
	}
}

func withForwardAuthProofVolume(spec tamossv1alpha1.WorkloadCommonSpec, tamoss *tamossv1alpha1.Tamoss, keys ...string) tamossv1alpha1.WorkloadCommonSpec {
	if !forwardAuthEnabled(tamoss) || len(keys) == 0 {
		return spec
	}
	mode := int32(0o444)
	items := make([]corev1.KeyToPath, 0, len(keys))
	seen := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		items = append(items, corev1.KeyToPath{Key: key, Path: key, Mode: &mode})
	}
	next := spec
	next.VolumeMounts = append([]corev1.VolumeMount{}, spec.VolumeMounts...)
	next.VolumeMounts = append(next.VolumeMounts, corev1.VolumeMount{
		Name:      forwardAuthProofVolumeName,
		MountPath: forwardAuthProofMountPath,
		ReadOnly:  true,
	})
	next.Volumes = append([]corev1.Volume{}, spec.Volumes...)
	next.Volumes = append(next.Volumes, corev1.Volume{
		Name: forwardAuthProofVolumeName,
		VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{
			SecretName:  ForwardAuthProofSecretName(tamoss),
			DefaultMode: &mode,
			Items:       items,
		}},
	})
	return next
}

func forwardAuthGroupBindingsJSON(tamoss *tamossv1alpha1.Tamoss) string {
	bindings, valid := normalizedForwardAuthGroupBindings(tamoss)
	if !valid {
		return "[]"
	}
	encoded, err := json.Marshal(bindings)
	if err != nil {
		return "[]"
	}
	return string(encoded)
}

func normalizedForwardAuthGroupBindings(tamoss *tamossv1alpha1.Tamoss) ([]normalizedForwardAuthBinding, bool) {
	if tamoss.Spec.Auth.AuthentikBlueprints == nil ||
		len(tamoss.Spec.Auth.AuthentikBlueprints.GroupBindings) == 0 ||
		len(tamoss.Spec.Auth.AuthentikBlueprints.GroupBindings) > maxForwardAuthGroupBindings {
		return nil, false
	}
	configuredBindings := tamoss.Spec.Auth.AuthentikBlueprints.GroupBindings
	bindings := make([]normalizedForwardAuthBinding, 0, len(configuredBindings))
	seenGroups := make(map[string]struct{}, len(configuredBindings))
	for _, configured := range configuredBindings {
		groupName := configured.GroupName
		if !validForwardAuthGroupName(groupName) {
			return nil, false
		}
		if _, exists := seenGroups[groupName]; exists {
			return nil, false
		}
		seenGroups[groupName] = struct{}{}

		permissions := make([]string, 0, len(configured.Permissions))
		seenPermissions := make(map[string]struct{}, len(configured.Permissions))
		for _, permission := range configured.Permissions {
			if _, valid := validForwardAuthPermissions[permission]; !valid {
				return nil, false
			}
			if _, exists := seenPermissions[permission]; exists {
				continue
			}
			seenPermissions[permission] = struct{}{}
			permissions = append(permissions, permission)
		}
		if len(permissions) == 0 {
			return nil, false
		}
		sort.Strings(permissions)
		bindings = append(bindings, normalizedForwardAuthBinding{GroupName: groupName, Permissions: permissions})
	}
	sort.Slice(bindings, func(i, j int) bool {
		return bindings[i].GroupName < bindings[j].GroupName
	})
	return bindings, true
}

func validForwardAuthGroupName(value string) bool {
	if value == "" || value != strings.TrimSpace(value) || len(value) > 512 || strings.Contains(value, "|") {
		return false
	}
	for _, character := range value {
		if !unicode.IsPrint(character) {
			return false
		}
	}
	return true
}
