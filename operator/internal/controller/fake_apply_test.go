package controller

import (
	"context"
	"encoding/json"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

// fakeApplyInterceptor emulates server-side apply for the controller-runtime
// fake client. The fake client's own apply support cannot honour the
// managed-fields reclamation step — its update path silently discards
// managedFields sent by the client (fake.fakeClient.update retains the stored
// entries unconditionally) — so foreign claims could never be transferred and
// pruned. The emulation is therefore a whole-object upsert: good enough for
// unit tests that assert resulting content, while field-ownership semantics
// are covered by the envtest specs in managed_apply_test.go.
func fakeApplyInterceptor() interceptor.Funcs {
	return interceptor.Funcs{
		Apply: func(ctx context.Context, c client.WithWatch, obj runtime.ApplyConfiguration, opts ...client.ApplyOption) error {
			raw, err := json.Marshal(obj)
			if err != nil {
				return err
			}
			desired := &unstructured.Unstructured{}
			if err := desired.UnmarshalJSON(raw); err != nil {
				return err
			}
			live := desired.DeepCopy()
			err = c.Get(ctx, client.ObjectKeyFromObject(desired), live)
			switch {
			case apierrors.IsNotFound(err):
				desired.SetResourceVersion("")
				if err := c.Create(ctx, desired); err != nil {
					return err
				}
			case err != nil:
				return err
			default:
				desired.SetResourceVersion(live.GetResourceVersion())
				if err := c.Update(ctx, desired); err != nil {
					return err
				}
			}
			// Write the upsert result back into the apply configuration so
			// callers observe the server response, as the real client does.
			applied, err := desired.MarshalJSON()
			if err != nil {
				return err
			}
			into, ok := obj.(json.Unmarshaler)
			if !ok {
				return fmt.Errorf("apply configuration %T cannot receive the apply response", obj)
			}
			return into.UnmarshalJSON(applied)
		},
	}
}
