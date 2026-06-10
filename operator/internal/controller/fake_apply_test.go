package controller

import (
	"context"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

// fakeApplyInterceptor emulates server-side apply for the controller-runtime
// fake client, which does not support apply patches on this controller-runtime
// release. The emulation is a whole-object upsert: good enough for unit tests
// that assert resulting content, while field-ownership semantics are covered
// by the envtest specs in managed_apply_test.go.
func fakeApplyInterceptor() interceptor.Funcs {
	return interceptor.Funcs{
		Patch: func(ctx context.Context, c client.WithWatch, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
			if patch != client.Apply { //nolint:staticcheck // client.Apply patches remain supported; migrating to client.Client.Apply(ApplyConfiguration) is a wider refactor than this upgrade.
				return c.Patch(ctx, obj, patch, opts...)
			}
			live := obj.DeepCopyObject().(client.Object)
			err := c.Get(ctx, client.ObjectKeyFromObject(obj), live)
			if apierrors.IsNotFound(err) {
				obj.SetResourceVersion("")
				return c.Create(ctx, obj)
			}
			if err != nil {
				return err
			}
			obj.SetResourceVersion(live.GetResourceVersion())
			return c.Update(ctx, obj)
		},
	}
}
