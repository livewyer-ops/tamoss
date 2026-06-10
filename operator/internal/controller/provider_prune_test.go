package controller

import (
	"context"
	"testing"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	fakediscovery "k8s.io/client-go/discovery/fake"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	k8stesting "k8s.io/client-go/testing"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	tamossv1alpha1 "github.com/livewyer-ops/tamoss/operator/api/v1alpha1"
	"github.com/livewyer-ops/tamoss/operator/internal/controller/backend/cnpg"
	"github.com/livewyer-ops/tamoss/operator/internal/controller/backend/rustfs"
	"github.com/livewyer-ops/tamoss/operator/internal/controller/resource"
	operatordiscovery "github.com/livewyer-ops/tamoss/operator/internal/discovery"
)

func TestOptionalOwnedPruneDeletesNoLongerDesiredObjects(t *testing.T) {
	ctx := context.Background()
	scheme := providerPruneScheme(t)
	tamoss := providerPruneTamoss()
	cluster := cnpg.BuildCluster(tamoss)
	scheduledBackup := cnpg.BuildScheduledBackup(tamoss)
	tenant := providerPruneTenant(tamoss)
	reconciler := TamossReconciler{
		Client: fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(cluster, scheduledBackup, tenant).
			Build(),
		Scheme: scheme,
	}

	err := reconciler.pruneOwnedObjects(ctx, tamoss, map[string]struct{}{})
	if err != nil {
		t.Fatalf("expected provider prune to succeed: %v", err)
	}

	for _, probe := range []client.Object{
		&cnpgv1.Cluster{ObjectMeta: metav1.ObjectMeta{Name: cluster.Name, Namespace: cluster.Namespace}},
		&cnpgv1.ScheduledBackup{ObjectMeta: metav1.ObjectMeta{Name: scheduledBackup.Name, Namespace: scheduledBackup.Namespace}},
		providerPruneTenant(tamoss),
	} {
		err := reconciler.Client.Get(ctx, client.ObjectKeyFromObject(probe), probe)
		if !apierrors.IsNotFound(err) {
			t.Fatalf("expected %T %s to be deleted, got %v", probe, probe.GetName(), err)
		}
	}
}

func TestOptionalOwnedPruneDeletesDisabledBackupPolicy(t *testing.T) {
	ctx := context.Background()
	scheme := providerPruneScheme(t)
	tamoss := providerPruneTamoss()
	cluster := cnpg.BuildCluster(tamoss)
	scheduledBackup := cnpg.BuildScheduledBackup(tamoss)
	tamoss.Spec.Backends.DB.CNPG.Backup.Enabled = false
	desired := map[string]struct{}{}
	markCNPGDesiredObjects(tamoss, desired)
	reconciler := TamossReconciler{
		Client: fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(cluster, scheduledBackup).
			Build(),
		Scheme: scheme,
	}

	err := reconciler.pruneOwnedObjects(ctx, tamoss, desired)
	if err != nil {
		t.Fatalf("expected provider prune to succeed: %v", err)
	}

	probeCluster := &cnpgv1.Cluster{ObjectMeta: metav1.ObjectMeta{Name: cluster.Name, Namespace: cluster.Namespace}}
	if err := reconciler.Client.Get(ctx, client.ObjectKeyFromObject(probeCluster), probeCluster); err != nil {
		t.Fatalf("expected CNPG Cluster to remain desired: %v", err)
	}
	probeBackup := &cnpgv1.ScheduledBackup{
		ObjectMeta: metav1.ObjectMeta{Name: scheduledBackup.Name, Namespace: scheduledBackup.Namespace},
	}
	err = reconciler.Client.Get(ctx, client.ObjectKeyFromObject(probeBackup), probeBackup)
	if !apierrors.IsNotFound(err) {
		t.Fatalf("expected disabled CNPG ScheduledBackup to be deleted, got %v", err)
	}
}

func TestOptionalOwnedPruneKeepsDesiredObjects(t *testing.T) {
	ctx := context.Background()
	scheme := providerPruneScheme(t)
	tamoss := providerPruneTamoss()
	cluster := cnpg.BuildCluster(tamoss)
	tenant := providerPruneTenant(tamoss)
	desired := map[string]struct{}{
		canonicalObjectKey(cluster): {},
		canonicalObjectKey(tenant):  {},
	}
	reconciler := TamossReconciler{
		Client: fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(cluster, tenant).
			Build(),
		Scheme: scheme,
	}

	err := reconciler.pruneOwnedObjects(ctx, tamoss, desired)
	if err != nil {
		t.Fatalf("expected provider prune to succeed: %v", err)
	}
	for _, probe := range []client.Object{
		&cnpgv1.Cluster{ObjectMeta: metav1.ObjectMeta{Name: cluster.Name, Namespace: cluster.Namespace}},
		providerPruneTenant(tamoss),
	} {
		if err := reconciler.Client.Get(ctx, client.ObjectKeyFromObject(probe), probe); err != nil {
			t.Fatalf("expected %T %s to remain: %v", probe, probe.GetName(), err)
		}
	}
}

func TestOptionalOwnedPruneCleansUpManagedResourcesAfterExternalProviderSwitch(t *testing.T) {
	ctx := context.Background()
	scheme := providerPruneScheme(t)
	previous := providerPruneTamoss()
	cluster := cnpg.BuildCluster(previous)
	scheduledBackup := cnpg.BuildScheduledBackup(previous)
	tenant := providerPruneTenant(previous)
	current := providerPruneExternalTamoss()
	reconciler := TamossReconciler{
		Client: fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(cluster, scheduledBackup, tenant).
			Build(),
		Scheme: scheme,
	}

	err := reconciler.pruneOwnedObjects(ctx, current, map[string]struct{}{})
	if err != nil {
		t.Fatalf("expected provider prune to succeed: %v", err)
	}
	for _, probe := range []client.Object{
		&cnpgv1.Cluster{ObjectMeta: metav1.ObjectMeta{Name: cluster.Name, Namespace: cluster.Namespace}},
		&cnpgv1.ScheduledBackup{ObjectMeta: metav1.ObjectMeta{Name: scheduledBackup.Name, Namespace: scheduledBackup.Namespace}},
		providerPruneTenant(current),
	} {
		err := reconciler.Client.Get(ctx, client.ObjectKeyFromObject(probe), probe)
		if !apierrors.IsNotFound(err) {
			t.Fatalf("expected %T %s to be deleted after provider switch, got %v", probe, probe.GetName(), err)
		}
	}
}

func TestOptionalOwnedObjectListsSkipKnownAbsentOptionalCRDs(t *testing.T) {
	ctx := context.Background()
	discovery := operatordiscovery.NewManager(
		&fakediscovery.FakeDiscovery{Fake: &k8stesting.Fake{}},
		[]schema.GroupVersionResource{
			operatordiscovery.CNPGClustersGVR,
			operatordiscovery.RustFSTenantsGVR,
		},
	)
	_ = discovery.Refresh(ctx)
	reconciler := TamossReconciler{Discovery: discovery}

	if got := reconciler.optionalOwnedObjectLists(ctx); len(got) != 0 {
		t.Fatalf("expected absent optional CRDs to be skipped, got %d lists", len(got))
	}
}

func providerPruneScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		t.Fatalf("add client-go scheme: %v", err)
	}
	if err := tamossv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("add tamoss scheme: %v", err)
	}
	if err := cnpgv1.AddToScheme(scheme); err != nil {
		t.Fatalf("add cnpg scheme: %v", err)
	}
	if err := gatewayv1.Install(scheme); err != nil {
		t.Fatalf("add gateway scheme: %v", err)
	}
	return scheme
}

func providerPruneTamoss() *tamossv1alpha1.Tamoss {
	return &tamossv1alpha1.Tamoss{
		TypeMeta: metav1.TypeMeta{
			APIVersion: tamossv1alpha1.GroupVersion.String(),
			Kind:       "Tamoss",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "example",
			Namespace: "media",
			UID:       types.UID("tamoss-uid"),
		},
		Spec: tamossv1alpha1.TamossSpec{
			Backends: tamossv1alpha1.BackendsSpec{
				DB: tamossv1alpha1.DBBackendSpec{
					ProvidedBy: tamossv1alpha1.BackendProvidedByCNPG,
					CNPG: &tamossv1alpha1.DBCNPGSpec{
						Backup: tamossv1alpha1.DBCNPGBackupSpec{
							Enabled:  true,
							Schedule: "0 0 * * *",
						},
					},
				},
				S3: tamossv1alpha1.S3BackendSpec{
					ProvidedBy:     tamossv1alpha1.S3BackendProvidedByRustFSOperator,
					RustFSOperator: &tamossv1alpha1.S3RustFSOperatorSpec{},
				},
			},
		},
	}
}

func providerPruneExternalTamoss() *tamossv1alpha1.Tamoss {
	tamoss := providerPruneTamoss()
	tamoss.Spec.Backends.DB = tamossv1alpha1.DBBackendSpec{
		ProvidedBy: tamossv1alpha1.BackendProvidedByExternal,
		External:   &tamossv1alpha1.DBExternalSpec{},
	}
	tamoss.Spec.Backends.S3 = tamossv1alpha1.S3BackendSpec{
		ProvidedBy: tamossv1alpha1.S3BackendProvidedByExternal,
		External:   &tamossv1alpha1.S3ExternalSpec{},
	}
	return tamoss
}

func providerPruneTenant(tamoss *tamossv1alpha1.Tamoss) *unstructured.Unstructured {
	tenant := rustfs.NewTenant()
	tenant.SetName(tamoss.ResourceName("s3"))
	tenant.SetNamespace(tamoss.Namespace)
	tenant.SetLabels(resource.TamossLabels(tamoss, "s3"))
	tenant.SetOwnerReferences(resource.TamossOwnerReferences(tamoss))
	return tenant
}
