package deleteprotection

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"strings"

	admissionv1 "k8s.io/api/admission/v1"
	authnv1 "k8s.io/api/authentication/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

const (
	ConfirmationAnnotation = "confirmation.tamoss.livewyer.io/deletion"

	TamossWebhookPath          = "/validate-tamoss-livewyer-io-v1alpha1-tamoss-delete"
	StorageBackendWebhookPath  = "/validate-tamoss-livewyer-io-v1alpha1-storagebackend-delete"
	FlowProfileWebhookPath     = "/validate-tamoss-livewyer-io-v1alpha1-flowprofile-delete"
	TamossHibernateWebhookPath = "/validate-tamoss-livewyer-io-v1alpha1-tamosshibernate-delete"
)

type Handler struct {
	Kind                 string
	OperatorCleanupUsers map[string]struct{}
	ImmutableSpec        bool
}

func NewImmutableSpecHandler(kind string, operatorCleanupUsers ...string) Handler {
	handler := NewHandler(kind, operatorCleanupUsers...)
	handler.ImmutableSpec = true
	return handler
}

func NewHandler(kind string, operatorCleanupUsers ...string) Handler {
	users := make(map[string]struct{}, len(operatorCleanupUsers))
	for _, user := range operatorCleanupUsers {
		if user = strings.TrimSpace(user); user != "" {
			users[user] = struct{}{}
		}
	}
	return Handler{Kind: kind, OperatorCleanupUsers: users}
}

func (h Handler) Handle(_ context.Context, req admission.Request) admission.Response {
	if req.Operation == admissionv1.Update && h.ImmutableSpec {
		oldSpec, err := objectSpec(req.OldObject.Raw)
		if err != nil {
			return admission.Errored(http.StatusBadRequest, err)
		}
		newSpec, err := objectSpec(req.Object.Raw)
		if err != nil {
			return admission.Errored(http.StatusBadRequest, err)
		}
		if !reflect.DeepEqual(oldSpec, newSpec) {
			return admission.Denied(fmt.Sprintf("%s spec is immutable; create a new resource instead.", h.Kind))
		}
		return admission.Allowed("immutable spec is unchanged")
	}
	if req.Operation != admissionv1.Delete {
		return admission.Allowed("delete protection only handles delete requests")
	}
	if h.operatorCleanupAllowed(req.UserInfo) {
		return admission.Allowed("operator cleanup is allowed")
	}

	metadata, err := oldObjectMetadata(req)
	if err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}
	if deletionConfirmed(metadata.Annotations) {
		return admission.Allowed("delete confirmation annotation is present")
	}

	name := req.Name
	if name == "" {
		name = metadata.Name
	}
	return admission.Denied(fmt.Sprintf(
		"%s %q is protected from deletion. Annotate it with %s=true before deleting.",
		h.Kind,
		name,
		ConfirmationAnnotation,
	))
}

func objectSpec(raw []byte) (any, error) {
	object := struct {
		Spec any `json:"spec"`
	}{}
	if len(raw) == 0 {
		return nil, fmt.Errorf("admission request did not include an object")
	}
	if err := json.Unmarshal(raw, &object); err != nil {
		return nil, fmt.Errorf("decode admission object: %w", err)
	}
	return object.Spec, nil
}

func deletionConfirmed(annotations map[string]string) bool {
	value := strings.TrimSpace(annotations[ConfirmationAnnotation])
	return strings.EqualFold(value, "true")
}

func (h Handler) operatorCleanupAllowed(user authnv1.UserInfo) bool {
	if len(h.OperatorCleanupUsers) == 0 {
		return false
	}
	_, ok := h.OperatorCleanupUsers[user.Username]
	return ok
}

func oldObjectMetadata(req admission.Request) (metav1.PartialObjectMetadata, error) {
	raw := req.OldObject.Raw
	if len(raw) == 0 {
		return metav1.PartialObjectMetadata{}, fmt.Errorf("delete request for %s %q did not include old object metadata", req.Kind.Kind, req.Name)
	}
	metadata := metav1.PartialObjectMetadata{}
	if err := json.Unmarshal(raw, &metadata); err != nil {
		return metav1.PartialObjectMetadata{}, fmt.Errorf("decode old object metadata: %w", err)
	}
	return metadata, nil
}
