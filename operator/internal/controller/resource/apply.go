package resource

import (
	"context"
	"encoding/json"
	"fmt"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ApplyObject writes obj with server-side apply through the first-class
// client.Client.Apply API that controller-runtime v0.22 introduced as the
// replacement for the deprecated client.Apply patch type. The desired object
// is converted to an unstructured apply configuration with the same plain
// JSON encoding the deprecated patch used for its request body (json.Marshal
// in both controller-runtime's applyPatch.Data and client-go's
// apply.NewRequest), so the request — and with it every field-ownership
// claim — is exactly what the patch-based mechanism sent. Zero-value
// sections a typed desired object cannot omit (creationTimestamp, empty
// status structs) marshal as null or empty maps just as before and claim no
// fields under server-side apply. The returned object carries the server's
// response, including resourceVersion and generation.
func ApplyObject(ctx context.Context, c client.Client, obj client.Object, opts ...client.ApplyOption) (*unstructured.Unstructured, error) {
	desired, err := applyConfigurationForObject(obj)
	if err != nil {
		return nil, err
	}
	if err := c.Apply(ctx, client.ApplyConfigurationFromUnstructured(desired), opts...); err != nil {
		return nil, err
	}
	return desired, nil
}

// applyConfigurationForObject converts a desired object into the unstructured
// form client.ApplyConfigurationFromUnstructured accepts. The object must
// already carry apiVersion and kind: server-side apply requests are rejected
// without them, exactly as with the deprecated apply patch.
func applyConfigurationForObject(obj client.Object) (*unstructured.Unstructured, error) {
	if u, ok := obj.(*unstructured.Unstructured); ok {
		return u.DeepCopy(), nil
	}
	raw, err := json.Marshal(obj)
	if err != nil {
		return nil, fmt.Errorf("encode %T as apply configuration: %w", obj, err)
	}
	u := &unstructured.Unstructured{}
	if err := u.UnmarshalJSON(raw); err != nil {
		return nil, fmt.Errorf("decode %T apply configuration: %w", obj, err)
	}
	return u, nil
}
