package controller

import (
	"context"
	"fmt"
	"strings"
	"time"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	tamossv1alpha1 "github.com/livewyer-ops/tamoss/operator/api/v1alpha1"
	"github.com/livewyer-ops/tamoss/operator/internal/controller/backend/cnpg"
	"github.com/livewyer-ops/tamoss/operator/internal/controller/backend/rustfs"
	operatordiscovery "github.com/livewyer-ops/tamoss/operator/internal/discovery"
	operatorstatus "github.com/livewyer-ops/tamoss/operator/internal/status"
)

func (r *TamossReconciler) reconcileProviderBackends(ctx context.Context, tamoss *tamossv1alpha1.Tamoss, desiredKeys map[string]struct{}) (providerBackendResult, error) {
	result := providerBackendResult{
		Ready:   true,
		Reason:  operatorstatus.ReasonProviderBackendsReady,
		Message: "Provider-managed backends are ready",
	}
	if tamoss.Spec.Backends.DB.Provider() == tamossv1alpha1.BackendProvidedByCNPG {
		dbResult, err := r.reconcileCNPG(ctx, tamoss)
		if err != nil || !dbResult.Ready {
			return dbResult, err
		}
		markCNPGDesiredObjects(tamoss, desiredKeys)
	}
	if tamoss.Spec.Backends.S3.Provider() != tamossv1alpha1.S3BackendProvidedByRustFSOperator {
		return result, nil
	}
	secretName, err := (rustfs.CredsSecretManager{Client: r.Client}).Ensure(ctx, tamoss)
	if err != nil {
		return providerBackendResult{}, err
	}
	if tamoss.S3Connection().Auth.ExistingSecret == secretName && secretName != "" {
		secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: secretName, Namespace: tamoss.Namespace}}
		desiredKeys[canonicalObjectKey(secret)] = struct{}{}
	}
	if err := rustfs.Reconcile(ctx, r.Client, tamoss, func(obj client.Object) error {
		return applyAdvancedResourcePatches(tamoss, obj)
	}); err != nil {
		return providerBackendResult{}, err
	}
	aliasService := rustfs.BuildServiceAlias(tamoss)
	if err := controllerutil.SetControllerReference(tamoss, aliasService, r.Scheme); err != nil {
		return providerBackendResult{}, err
	}
	if err := applyAdvancedResourcePatches(tamoss, aliasService); err != nil {
		return providerBackendResult{}, err
	}
	desiredKeys[canonicalObjectKey(aliasService)] = struct{}{}
	if _, err := applyManagedObject(ctx, r.Client, aliasService); err != nil {
		return providerBackendResult{}, err
	}

	tenant := rustfs.NewTenant()
	tenant.SetName(tamoss.ResourceName("s3"))
	tenant.SetNamespace(tamoss.Namespace)
	desiredKeys[canonicalObjectKey(tenant)] = struct{}{}
	if err := r.Client.Get(ctx, client.ObjectKeyFromObject(tenant), tenant); err != nil {
		if apierrors.IsNotFound(err) {
			return providerBackendResult{
				Ready:   false,
				Reason:  operatorstatus.ReasonTenantNotReady,
				Message: "RustFS Tenant has not been observed yet",
			}, nil
		}
		return providerBackendResult{}, err
	}
	condition, events := rustfs.RollupStatus(tenant)
	for _, event := range events {
		r.recordWarning(tamoss, event.Reason, event.Message)
	}
	if condition.Status != metav1.ConditionTrue {
		return providerBackendResult{
			Ready:    false,
			Reason:   condition.Reason,
			Message:  condition.Message,
			Degraded: len(events) > 0,
		}, nil
	}

	return r.reconcileDefaultStorageBackendBucket(ctx, tamoss, desiredKeys)
}

func markCNPGDesiredObjects(tamoss *tamossv1alpha1.Tamoss, desiredKeys map[string]struct{}) {
	desiredKeys[canonicalObjectKey(cnpg.BuildCluster(tamoss))] = struct{}{}
	if scheduledBackup := cnpg.BuildScheduledBackup(tamoss); scheduledBackup != nil {
		desiredKeys[canonicalObjectKey(scheduledBackup)] = struct{}{}
	}
}

func (r *TamossReconciler) reconcileCNPG(ctx context.Context, tamoss *tamossv1alpha1.Tamoss) (providerBackendResult, error) {
	if missing := cnpgBackupPolicyMissingFields(tamoss); len(missing) > 0 {
		return providerBackendResult{
			Ready:    false,
			Reason:   operatorstatus.ReasonBackupPolicyIncomplete,
			Message:  fmt.Sprintf("CNPG backup policy is missing required fields: %s", strings.Join(missing, ", ")),
			Degraded: true,
		}, nil
	}
	// A resolved hibernation artifact renders the recovery bootstrap
	// directly, so the emitted cluster spec is stable across reconciles
	// without preserving live fields.
	injectResolvedRestore(tamoss)
	mutators := []cnpg.ObjectMutator{func(obj client.Object) error {
		return applyAdvancedResourcePatches(tamoss, obj)
	}}
	if err := cnpg.Reconcile(ctx, r.Client, tamoss, mutators...); err != nil {
		return providerBackendResult{}, err
	}

	cluster := &cnpgv1.Cluster{}
	clusterKey := types.NamespacedName{Name: tamoss.ResourceName("db"), Namespace: tamoss.Namespace}
	if err := r.Client.Get(ctx, clusterKey, cluster); err != nil {
		if apierrors.IsNotFound(err) {
			return providerBackendResult{
				Ready:   false,
				Reason:  operatorstatus.ReasonClusterNotReady,
				Message: fmt.Sprintf("CNPG Cluster %s has not been observed yet", clusterKey.Name),
			}, nil
		}
		return providerBackendResult{}, err
	}

	condition, events := cnpg.RollupStatus(cluster)
	for _, event := range events {
		r.recordWarning(tamoss, event.Reason, event.Message)
	}
	if condition.Status != metav1.ConditionTrue {
		return providerBackendResult{
			Ready:    false,
			Reason:   condition.Reason,
			Message:  condition.Message,
			Degraded: len(events) > 0,
		}, nil
	}

	_, secretReadiness, err := (cnpg.SecretReader{Client: r.Client}).Read(ctx, tamoss)
	if err != nil {
		return providerBackendResult{}, err
	}
	if !secretReadiness.Ready {
		return providerBackendResult{
			Ready:   false,
			Reason:  secretReadiness.Reason,
			Message: secretReadiness.Message,
		}, nil
	}
	return providerBackendResult{
		Ready:   true,
		Reason:  operatorstatus.ReasonCNPGClusterReady,
		Message: fmt.Sprintf("CNPG Cluster %s is ready", cluster.Name),
	}, nil
}

// injectResolvedRestore copies the persisted restore source into the resolved
// spec so the CNPG renderer emits the recovery bootstrap and external cluster
// on every reconcile. It mutates the resolved deep copy only, never the
// user's object.
func injectResolvedRestore(tamoss *tamossv1alpha1.Tamoss) {
	restore := tamoss.Status.Lifecycle.ResolvedRestore
	if restore == nil || !restore.Restore.Enabled {
		return
	}
	if tamoss.Spec.Backends.DB.CNPG == nil {
		tamoss.Spec.Backends.DB.CNPG = &tamossv1alpha1.DBCNPGSpec{}
	}
	tamoss.Spec.Backends.DB.CNPG.Restore = restore.Restore
}

func cnpgBackupPolicyMissingFields(tamoss *tamossv1alpha1.Tamoss) []string {
	if tamoss.Spec.Backends.DB.Provider() != tamossv1alpha1.BackendProvidedByCNPG ||
		tamoss.Spec.Backends.DB.CNPG == nil {
		return nil
	}
	return tamoss.Spec.Backends.DB.CNPG.Backup.MissingRequiredFields()
}

func (r *TamossReconciler) reconcileDefaultStorageBackendBucket(ctx context.Context, tamoss *tamossv1alpha1.Tamoss, desiredKeys map[string]struct{}) (providerBackendResult, error) {
	storageBackend, err := r.ensureDefaultStorageBackend(ctx, tamoss, desiredKeys)
	if err != nil {
		return providerBackendResult{}, err
	}
	condition := meta.FindStatusCondition(storageBackend.Status.Conditions, operatorstatus.ConditionBucketReady)
	if condition == nil || condition.Status != metav1.ConditionTrue {
		return providerBackendResult{
			Ready:   false,
			Reason:  operatorstatus.ConditionReason(condition, operatorstatus.ReasonStorageBackendBucketNotReady),
			Message: operatorstatus.ConditionMessage(condition, fmt.Sprintf("StorageBackend %s has not created its bucket yet", storageBackend.Name)),
		}, nil
	}
	return providerBackendResult{
		Ready:   true,
		Reason:  operatorstatus.ReasonStorageBackendBucketReady,
		Message: fmt.Sprintf("StorageBackend %s bucket is ready", storageBackend.Name),
	}, nil
}

func (r *TamossReconciler) checkDefaultStorageBackendReady(ctx context.Context, tamoss *tamossv1alpha1.Tamoss) (providerBackendResult, error) {
	if !tamossUsesManagedS3(tamoss) {
		return providerBackendResult{Ready: true, Reason: operatorstatus.ReasonStorageBackendNotManaged, Message: "No managed StorageBackend is required"}, nil
	}
	storageBackend := &tamossv1alpha1.StorageBackend{}
	key := types.NamespacedName{Name: defaultStorageBackendName(tamoss), Namespace: tamoss.Namespace}
	if err := r.Client.Get(ctx, key, storageBackend); err != nil {
		if apierrors.IsNotFound(err) {
			return providerBackendResult{
				Ready:   false,
				Reason:  operatorstatus.ReasonStorageBackendNotReady,
				Message: fmt.Sprintf("StorageBackend %s has not been observed yet", key.Name),
			}, nil
		}
		return providerBackendResult{}, err
	}
	condition := meta.FindStatusCondition(storageBackend.Status.Conditions, operatorstatus.ConditionReady)
	if condition == nil || condition.Status != metav1.ConditionTrue {
		return providerBackendResult{
			Ready:    false,
			Reason:   operatorstatus.ConditionReason(condition, operatorstatus.ReasonStorageBackendNotReady),
			Message:  operatorstatus.ConditionMessage(condition, fmt.Sprintf("StorageBackend %s is not ready", storageBackend.Name)),
			Degraded: storageBackend.Status.Phase == operatorstatus.PhaseDegraded,
		}, nil
	}
	return providerBackendResult{
		Ready:   true,
		Reason:  operatorstatus.ReasonStorageBackendReady,
		Message: fmt.Sprintf("StorageBackend %s is ready", storageBackend.Name),
	}, nil
}

func (r *TamossReconciler) ensureDefaultStorageBackend(ctx context.Context, tamoss *tamossv1alpha1.Tamoss, desiredKeys map[string]struct{}) (*tamossv1alpha1.StorageBackend, error) {
	desired := defaultStorageBackend(tamoss)
	if err := controllerutil.SetControllerReference(tamoss, desired, r.Scheme); err != nil {
		return nil, err
	}
	if err := applyAdvancedResourcePatches(tamoss, desired); err != nil {
		return nil, err
	}
	desiredKeys[canonicalObjectKey(desired)] = struct{}{}
	if _, err := applyManagedObject(ctx, r.Client, desired); err != nil {
		return nil, err
	}
	live := &tamossv1alpha1.StorageBackend{}
	if err := r.Client.Get(ctx, types.NamespacedName{Name: desired.Name, Namespace: desired.Namespace}, live); err != nil {
		return desired, client.IgnoreNotFound(err)
	}
	return live, nil
}

func defaultStorageBackend(tamoss *tamossv1alpha1.Tamoss) *tamossv1alpha1.StorageBackend {
	s3 := tamoss.S3Connection()
	spec := tamossv1alpha1.StorageBackendSpec{
		ID:             tamossv1alpha1.DefaultStorageBackendID,
		TamossRef:      tamossv1alpha1.TamossReferenceSpec{Name: tamoss.Name},
		Provider:       tamossv1alpha1.StorageBackendProviderRustFS,
		DefaultStorage: true,
		Label:          fmt.Sprintf("tamoss.%s:s3:%s", s3.Region, s3.Bucket),
		Region:         s3.Region,
		StoreProduct:   "s3",
		StoreType:      "http_object_store",
		Tags:           tamoss.Spec.Backends.S3.Tags,
		BucketName:     s3.Bucket,
		Endpoint:       s3.Endpoint,
		Credentials:    s3.Auth,
	}
	spec.ApplyDefaults(tamoss.Namespace, defaultStorageBackendName(tamoss))
	return &tamossv1alpha1.StorageBackend{
		ObjectMeta: metav1.ObjectMeta{
			Name:      defaultStorageBackendName(tamoss),
			Namespace: tamoss.Namespace,
			Labels: map[string]string{
				"app.kubernetes.io/name":       tamossAppName,
				appInstanceLabel:               tamoss.Name,
				appComponentLabel:              "storage-backend",
				"app.kubernetes.io/managed-by": "tamoss-operator",
			},
		},
		Spec: spec,
	}
}

func defaultStorageBackendName(tamoss *tamossv1alpha1.Tamoss) string {
	return tamoss.ResourceName("storage-default")
}

func tamossUsesManagedS3(tamoss *tamossv1alpha1.Tamoss) bool {
	provider := tamoss.Spec.Backends.S3.Provider()
	return provider == tamossv1alpha1.S3BackendProvidedByRustFSOperator
}

type backendReadinessResult struct {
	Ready    bool
	Reason   string
	Message  string
	Degraded bool
}

type (
	backendReferenceResult = backendReadinessResult
	providerBackendResult  = backendReadinessResult
)

type backendDependencyGateResult struct {
	Allowed bool
	Known   bool
	Reason  string
	Message string
}

func (r *TamossReconciler) backendDependencyGate(ctx context.Context, tamoss *tamossv1alpha1.Tamoss) backendDependencyGateResult {
	result := backendDependencyGateResult{
		Allowed: true,
		Known:   true,
		Reason:  operatorstatus.ReasonBackendDependenciesAvailable,
		Message: "Backend dependency operators are available",
	}
	providers := []string{
		string(tamoss.Spec.Backends.DB.Provider()),
		string(tamoss.Spec.Backends.S3.Provider()),
	}
	for _, provider := range providers {
		hint, ok := operatordiscovery.HintFor(provider)
		if !ok {
			continue
		}
		present, known := r.dependencyCRDPresent(ctx, hint.GVR)
		if !known {
			return backendDependencyGateResult{Known: false}
		}
		if present {
			continue
		}
		return backendDependencyGateResult{
			Allowed: false,
			Known:   true,
			Reason:  operatorstatus.ReasonMissingDependencyOperator,
			Message: fmt.Sprintf("%s requires %s (%s). Install it with: %s", provider, hint.DependencyName, gvrString(hint.GVR), hint.InstallCommand),
		}
	}
	return result
}

func (r *TamossReconciler) dependencyCRDPresent(ctx context.Context, gvr schema.GroupVersionResource) (bool, bool) {
	if r.Discovery == nil {
		return false, true
	}
	return r.Discovery.HasCRD(gvr)
}

func (r *TamossReconciler) dependencyProbeInterval() time.Duration {
	if r.DependencyProbeInterval > 0 {
		return r.DependencyProbeInterval
	}
	return defaultDependencyProbeInterval
}

func tamossUsesProviderManagedBackends(tamoss *tamossv1alpha1.Tamoss) bool {
	return tamoss.Spec.Backends.DB.Provider() == tamossv1alpha1.BackendProvidedByCNPG ||
		tamoss.Spec.Backends.S3.Provider() == tamossv1alpha1.S3BackendProvidedByRustFSOperator
}

func (r *TamossReconciler) checkBackendReferences(ctx context.Context, tamoss *tamossv1alpha1.Tamoss) (backendReferenceResult, error) {
	result := backendReferenceResult{
		Ready:   true,
		Reason:  operatorstatus.ReasonBackendReferencesConfigured,
		Message: "Backend secret references are configured",
	}
	for _, requirement := range backendConfigRequirements(tamoss) {
		if strings.TrimSpace(requirement.value) != "" {
			continue
		}
		result.Ready = false
		result.Reason = operatorstatus.ReasonMissingProviderConfiguration
		result.Message = fmt.Sprintf("Required configuration %s for %s is missing", requirement.field, requirement.purpose)
	}
	if !result.Ready {
		return result, nil
	}
	for _, requirement := range backendSecretRequirements(tamoss) {
		if requirement.name == "" {
			continue
		}
		secret := &corev1.Secret{}
		err := r.Client.Get(ctx, types.NamespacedName{Name: requirement.name, Namespace: tamoss.Namespace}, secret)
		if apierrors.IsNotFound(err) {
			result.Ready = false
			result.Reason = operatorstatus.ReasonMissingSecret
			result.Message = fmt.Sprintf("Required secret %s for %s was not found", requirement.name, requirement.purpose)
			continue
		}
		if err != nil {
			return backendReferenceResult{}, err
		}
		for _, key := range requirement.keys {
			if key == "" {
				continue
			}
			if _, ok := secret.Data[key]; !ok {
				result.Ready = false
				result.Reason = operatorstatus.ReasonMissingSecret
				result.Message = fmt.Sprintf("Required key %s is missing from secret %s for %s", key, requirement.name, requirement.purpose)
			}
		}
	}
	return result, nil
}

type backendSecretRequirement struct {
	purpose string
	name    string
	keys    []string
}

type backendConfigRequirement struct {
	purpose string
	field   string
	value   string
}

func backendConfigRequirements(tamoss *tamossv1alpha1.Tamoss) []backendConfigRequirement {
	requirements := []backendConfigRequirement{}
	if tamoss.Spec.Backends.DB.Provider() == tamossv1alpha1.BackendProvidedByExternal {
		db := tamoss.Spec.Backends.DB.External
		if db == nil {
			return append(requirements, backendConfigRequirement{
				purpose: "postgres",
				field:   ".spec.backends.db.external",
			})
		}
		requirements = append(requirements,
			backendConfigRequirement{purpose: "postgres", field: ".spec.backends.db.external.host", value: db.Host},
			backendConfigRequirement{purpose: "postgres", field: ".spec.backends.db.external.port", value: db.Port},
			backendConfigRequirement{purpose: "postgres", field: ".spec.backends.db.external.database", value: db.Database},
			backendConfigRequirement{purpose: "postgres", field: ".spec.backends.db.external.auth.existingSecret", value: db.Auth.ExistingSecret},
			backendConfigRequirement{purpose: "postgres", field: ".spec.backends.db.external.auth.secretKeys.username", value: db.Auth.SecretKeys.Username},
			backendConfigRequirement{purpose: "postgres", field: ".spec.backends.db.external.auth.secretKeys.password", value: db.Auth.SecretKeys.Password},
		)
	}
	if tamoss.Spec.Backends.S3.Provider() == tamossv1alpha1.S3BackendProvidedByExternal {
		s3 := tamoss.Spec.Backends.S3.External
		if s3 == nil {
			return append(requirements, backendConfigRequirement{
				purpose: "s3",
				field:   ".spec.backends.s3.external",
			})
		}
		requirements = append(requirements,
			backendConfigRequirement{purpose: "s3", field: ".spec.backends.s3.external.endpoint.default.url", value: s3.Endpoint.Default.URL},
			backendConfigRequirement{purpose: "s3", field: ".spec.backends.s3.external.region", value: s3.Region},
			backendConfigRequirement{purpose: "s3", field: ".spec.backends.s3.external.bucket", value: s3.Bucket},
			backendConfigRequirement{purpose: "s3", field: ".spec.backends.s3.external.auth.existingSecret", value: s3.Auth.ExistingSecret},
			backendConfigRequirement{purpose: "s3", field: ".spec.backends.s3.external.auth.secretKeys.accessKey", value: s3.Auth.SecretKeys.AccessKey},
			backendConfigRequirement{purpose: "s3", field: ".spec.backends.s3.external.auth.secretKeys.secretKey", value: s3.Auth.SecretKeys.SecretKey},
		)
	}
	return requirements
}

func backendSecretRequirements(tamoss *tamossv1alpha1.Tamoss) []backendSecretRequirement {
	db := tamoss.DBConnection()
	s3 := tamoss.S3Connection()
	requirements := []backendSecretRequirement{}
	if tamoss.Spec.Backends.DB.Provider() != tamossv1alpha1.BackendProvidedByCNPG {
		requirements = append(requirements, backendSecretRequirement{
			purpose: "postgres",
			name:    db.Auth.ExistingSecret,
			keys: []string{
				db.Auth.SecretKeys.Username,
				db.Auth.SecretKeys.Password,
			},
		})
	}
	requirements = append(requirements,
		backendSecretRequirement{
			purpose: "s3",
			name:    s3.Auth.ExistingSecret,
			keys: []string{
				s3.Auth.SecretKeys.AccessKey,
				s3.Auth.SecretKeys.SecretKey,
			},
		},
	)
	return requirements
}
