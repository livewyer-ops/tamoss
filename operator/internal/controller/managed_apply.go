package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/livewyer-ops/tamoss/operator/internal/controller/resource"
)

type applyResult struct {
	Changed bool
	Created bool
}

// applyManagedObject reconciles a managed child with server-side apply as the
// single write mechanism. The desired object must contain exactly the fields
// the operator manages: server-side apply claims ownership of those fields
// (forcing conflicting values back to the desired state) and leaves fields
// owned by other managers — kubelet defaults, third-party annotations,
// API-server allocations — untouched.
func applyManagedObject(ctx context.Context, c client.Client, desired client.Object) (applyResult, error) {
	ensureTypeMeta(desired)

	live := emptyObjectLike(desired)
	key := types.NamespacedName{Name: desired.GetName(), Namespace: desired.GetNamespace()}
	if err := c.Get(ctx, key, live); err != nil {
		if apierrors.IsNotFound(err) {
			// Creating through apply keeps a single write mechanism and
			// records a clean apply entry from the start, so the next
			// reconcile is a no-op instead of a field-ownership migration.
			return applyResult{Changed: true, Created: true}, applyObjectPatch(ctx, c, desired)
		}
		return applyResult{}, err
	}
	if err := reclaimAuthoritativeFields(ctx, c, live); err != nil {
		return applyResult{}, err
	}
	if err := applyObjectPatch(ctx, c, desired); err != nil {
		return applyResult{}, err
	}
	return applyResult{Changed: managedObjectChanged(live, desired)}, nil
}

func applyObjectPatch(ctx context.Context, c client.Client, desired client.Object) error {
	return c.Patch(ctx, desired, client.Apply, client.FieldOwner(resource.FieldOwner), client.ForceOwnership) //nolint:staticcheck // client.Apply patches remain supported; migrating to client.Client.Apply(ApplyConfiguration) is a wider refactor than this upgrade.
}

// managedObjectChanged reports whether the apply produced a new revision of
// the object. A server-side apply that matches the live state is a no-op and
// leaves resourceVersion untouched, so comparing revisions around the apply
// detects drift without any client-side diffing. Generation is preferred for
// kinds that track it so concurrent status-only writes do not register as
// drift; metadata-only corrections on those kinds are still applied but
// reported as unchanged.
func managedObjectChanged(live, applied client.Object) bool {
	if live.GetGeneration() > 0 && applied.GetGeneration() > 0 {
		return applied.GetGeneration() != live.GetGeneration()
	}
	return applied.GetResourceVersion() != live.GetResourceVersion()
}

// reclaimedObjectSections are the top-level sections of a managed child the
// operator is authoritative for. Foreign claims to these sections are
// transferred to the operator before each apply.
var reclaimedObjectSections = []string{"f:spec", "f:data", "f:binaryData", "f:stringData", "f:type"}

// serviceAllocatedFieldClaims are Service spec fields the API server
// allocates or defaults server-side. Claims to them are never reclaimed:
// they are not drift, and removing an allocated value (for example
// clusterIP) would be rejected as an immutable-field change.
var serviceAllocatedFieldClaims = []string{
	"f:clusterIP",
	"f:clusterIPs",
	"f:ipFamilies",
	"f:ipFamilyPolicy",
	"f:healthCheckNodePort",
	"f:internalTrafficPolicy",
	"f:sessionAffinity",
	"f:sessionAffinityConfig",
	"f:allocateLoadBalancerNodePorts",
	"f:loadBalancerClass",
}

// reclaimAuthoritativeFields transfers foreign field-manager claims on the
// operator-authoritative sections of a managed child to the operator's apply
// entry. Server-side apply only removes fields its own field manager owns, so
// without this transfer an out-of-band addition (for example `kubectl patch`
// appending a Service port) would survive reconciliation forever. After the
// transfer the next apply prunes everything the desired object does not
// include — the SSA-native way of expressing exclusive authority. Claims made
// through subresources (status, scale) are deliberately left alone: they
// belong to status writers and the horizontal pod autoscaler.
func reclaimAuthoritativeFields(ctx context.Context, c client.Client, live client.Object) error {
	apiVersion := canonicalObjectGVK(live).GroupVersion().String()
	entries := live.GetManagedFields()
	next := make([]metav1.ManagedFieldsEntry, 0, len(entries))
	reclaimed := map[string]interface{}{}
	ownerIndex := -1
	for _, entry := range entries {
		if entry.Manager == resource.FieldOwner && entry.Operation == metav1.ManagedFieldsOperationApply && entry.Subresource == "" {
			ownerIndex = len(next)
			next = append(next, entry)
			continue
		}
		if entry.Subresource != "" || entry.FieldsV1 == nil || entry.APIVersion != apiVersion {
			next = append(next, entry)
			continue
		}
		fields := map[string]interface{}{}
		if err := json.Unmarshal(entry.FieldsV1.GetRawBytes(), &fields); err != nil {
			return fmt.Errorf("decode managed fields of %s by %q: %w", live.GetName(), entry.Manager, err)
		}
		moved := false
		for _, section := range reclaimedObjectSections {
			claim, found := fields[section].(map[string]interface{})
			if !found {
				continue
			}
			if _, isService := live.(*corev1.Service); isService && section == "f:spec" {
				for _, allocated := range serviceAllocatedFieldClaims {
					delete(claim, allocated)
				}
				if len(claim) == 0 {
					continue
				}
			}
			mergeFieldsV1(reclaimed, map[string]interface{}{section: claim})
			delete(fields, section)
			moved = true
		}
		if !moved {
			next = append(next, entry)
			continue
		}
		if len(fields) > 0 {
			raw, err := json.Marshal(fields)
			if err != nil {
				return err
			}
			entry.FieldsV1 = &metav1.FieldsV1{Raw: raw}
			next = append(next, entry)
		}
	}
	if len(reclaimed) == 0 {
		return nil
	}
	if ownerIndex < 0 {
		ownerIndex = len(next)
		next = append(next, metav1.ManagedFieldsEntry{
			Manager:    resource.FieldOwner,
			Operation:  metav1.ManagedFieldsOperationApply,
			APIVersion: apiVersion,
			FieldsType: "FieldsV1",
			FieldsV1:   metav1.NewFieldsV1("{}"),
		})
	}
	ownerFields := map[string]interface{}{}
	if raw := next[ownerIndex].FieldsV1; raw != nil {
		if err := json.Unmarshal(raw.GetRawBytes(), &ownerFields); err != nil {
			return fmt.Errorf("decode managed fields of %s by %q: %w", live.GetName(), resource.FieldOwner, err)
		}
	}
	mergeFieldsV1(ownerFields, reclaimed)
	raw, err := json.Marshal(ownerFields)
	if err != nil {
		return err
	}
	ownerFieldsV1 := &metav1.FieldsV1{}
	ownerFieldsV1.SetRawBytes(raw)
	next[ownerIndex].FieldsV1 = ownerFieldsV1
	live.SetManagedFields(next)
	return c.Update(ctx, live)
}

func mergeFieldsV1(dst, src map[string]interface{}) {
	for key, value := range src {
		srcMap, srcIsMap := value.(map[string]interface{})
		dstMap, dstIsMap := dst[key].(map[string]interface{})
		if srcIsMap && dstIsMap {
			mergeFieldsV1(dstMap, srcMap)
			continue
		}
		dst[key] = value
	}
}

// ensureTypeMeta stamps the scheme-resolved GroupVersionKind onto obj so that
// server-side apply patches always carry apiVersion and kind.
func ensureTypeMeta(obj client.Object) {
	obj.GetObjectKind().SetGroupVersionKind(canonicalObjectGVK(obj))
}

func emptyObjectLike(obj client.Object) client.Object {
	if typed, ok := obj.(*unstructured.Unstructured); ok {
		empty := &unstructured.Unstructured{}
		empty.SetGroupVersionKind(typed.GroupVersionKind())
		return empty
	}
	return reflect.New(reflect.TypeOf(obj).Elem()).Interface().(client.Object)
}
