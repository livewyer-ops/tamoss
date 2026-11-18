package deleteprotection

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	admissionv1 "k8s.io/api/admission/v1"
	authnv1 "k8s.io/api/authentication/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

func TestHandlerDeniesDeleteWithoutConfirmation(t *testing.T) {
	response := NewHandler("Tamoss").Handle(context.Background(), deleteRequest("Tamoss", "example", nil, authnv1.UserInfo{Username: "alice"}))

	if response.Allowed {
		t.Fatalf("expected delete to be denied")
	}
	if !strings.Contains(response.Result.Message, ConfirmationAnnotation+"=true") {
		t.Fatalf("expected actionable annotation message, got %q", response.Result.Message)
	}
}

func TestHandlerAllowsDeleteWithConfirmation(t *testing.T) {
	response := NewHandler("Tamoss").Handle(context.Background(), deleteRequest("Tamoss", "example", map[string]string{
		ConfirmationAnnotation: "true",
	}, authnv1.UserInfo{Username: "alice"}))

	if !response.Allowed {
		t.Fatalf("expected confirmed delete to be allowed, got %q", response.Result.Message)
	}
}

func TestHandlerAllowsOperatorServiceAccountDelete(t *testing.T) {
	response := NewHandler("StorageBackend", "system:serviceaccount:tamoss-system:operator-controller-manager").Handle(context.Background(), deleteRequest("StorageBackend", "default", nil, authnv1.UserInfo{
		Username: "system:serviceaccount:tamoss-system:operator-controller-manager",
	}))

	if !response.Allowed {
		t.Fatalf("expected operator cleanup delete to be allowed, got %q", response.Result.Message)
	}
}

func TestHandlerRequiresExactOperatorServiceAccount(t *testing.T) {
	response := NewHandler("StorageBackend", "system:serviceaccount:tamoss-system:operator-controller-manager").Handle(context.Background(), deleteRequest("StorageBackend", "default", nil, authnv1.UserInfo{
		Username: "system:serviceaccount:custom-namespace:operator-controller-manager",
	}))

	if response.Allowed {
		t.Fatalf("expected operator cleanup delete from another namespace to be denied")
	}
}

func TestHandlerDoesNotBypassTamossDeleteForOperatorServiceAccount(t *testing.T) {
	response := NewHandler("Tamoss").Handle(context.Background(), deleteRequest("Tamoss", "example", nil, authnv1.UserInfo{
		Username: "system:serviceaccount:tamoss-system:operator-controller-manager",
	}))

	if response.Allowed {
		t.Fatalf("expected Tamoss delete to require confirmation even for operator service account")
	}
}

func TestHandlerDeniesFalseConfirmation(t *testing.T) {
	response := NewHandler("StorageBackend", "system:serviceaccount:tamoss-system:operator-controller-manager").Handle(context.Background(), deleteRequest("StorageBackend", "default", map[string]string{
		ConfirmationAnnotation: "false",
	}, authnv1.UserInfo{Username: "alice"}))

	if response.Allowed {
		t.Fatalf("expected false confirmation to be denied")
	}
}

func TestHandlerIgnoresNonDeleteOperations(t *testing.T) {
	req := deleteRequest("Tamoss", "example", nil, authnv1.UserInfo{Username: "alice"})
	req.Operation = admissionv1.Update

	response := NewHandler("Tamoss").Handle(context.Background(), req)

	if !response.Allowed {
		t.Fatalf("expected non-delete operation to be allowed, got %q", response.Result.Message)
	}
}

func TestImmutableSpecHandlerRejectsSpecChanges(t *testing.T) {
	response := NewImmutableSpecHandler("FlowProfile").Handle(context.Background(), admission.Request{AdmissionRequest: admissionv1.AdmissionRequest{
		Operation: admissionv1.Update,
		OldObject: runtime.RawExtension{Raw: []byte(`{"spec":{"label":"old"},"status":{"phase":"Ready"}}`)},
		Object:    runtime.RawExtension{Raw: []byte(`{"spec":{"label":"new"},"status":{"phase":"Ready"}}`)},
	}})
	if response.Allowed {
		t.Fatal("expected immutable FlowProfile spec update to be denied")
	}
}

func TestImmutableSpecHandlerAllowsStatusChanges(t *testing.T) {
	response := NewImmutableSpecHandler("FlowProfile").Handle(context.Background(), admission.Request{AdmissionRequest: admissionv1.AdmissionRequest{
		Operation: admissionv1.Update,
		OldObject: runtime.RawExtension{Raw: []byte(`{"spec":{"label":"same"},"status":{"phase":"Pending"}}`)},
		Object:    runtime.RawExtension{Raw: []byte(`{"spec":{"label":"same"},"status":{"phase":"Ready"}}`)},
	}})
	if !response.Allowed {
		t.Fatalf("expected status-only update to be allowed, got %q", response.Result.Message)
	}
}

func deleteRequest(kind, name string, annotations map[string]string, user authnv1.UserInfo) admission.Request {
	metadata := metav1.PartialObjectMetadata{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "tamoss.livewyer.io/v1alpha1",
			Kind:       kind,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Namespace:   "tams",
			Annotations: annotations,
		},
	}
	raw, err := json.Marshal(metadata)
	if err != nil {
		panic(err)
	}
	return admission.Request{AdmissionRequest: admissionv1.AdmissionRequest{
		Operation: admissionv1.Delete,
		Name:      name,
		Namespace: "tams",
		Kind: metav1.GroupVersionKind{
			Group:   "tamoss.livewyer.io",
			Version: "v1alpha1",
			Kind:    kind,
		},
		OldObject: runtime.RawExtension{Raw: raw},
		UserInfo:  user,
	}}
}
