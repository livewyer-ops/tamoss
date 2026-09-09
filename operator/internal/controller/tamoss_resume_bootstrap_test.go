package controller

import (
	"context"
	"fmt"
	"strings"
	"testing"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	tamossv1alpha1 "github.com/livewyer-ops/tamoss/operator/api/v1alpha1"
	schemabundle "github.com/livewyer-ops/tamoss/operator/internal/schema"
	operatorstatus "github.com/livewyer-ops/tamoss/operator/internal/status"
)

const bootstrapTestChecksum = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

type fakeHibernationManifestReader struct {
	calls    int
	key      string
	manifest hibernationManifest
	checksum string
	err      error
}

func (f *fakeHibernationManifestReader) Read(_ context.Context, _ string, _ tamossv1alpha1.StorageBackendSpec, key string) (hibernationManifest, string, error) {
	f.calls++
	f.key = key
	if f.err != nil {
		return hibernationManifest{}, "", f.err
	}
	return f.manifest, f.checksum, nil
}

func bootstrapManifestFixture() hibernationManifest {
	return hibernationManifest{
		ManifestVersion: hibernationManifestVersion,
		Driver:          string(tamossv1alpha1.HibernationDriverCNPGPhysical),
		SourceTamoss: hibernationManifestTamoss{
			Name:      "example",
			Namespace: "media",
		},
		Schema: hibernationManifestSchema{
			Version:      schemabundle.SchemaVersion,
			TAMSAPI:      schemabundle.SupportedTAMSAPIVersion,
			ManifestKind: "TamossHibernate",
		},
		Database: hibernationManifestDatabase{
			Provider: string(tamossv1alpha1.BackendProvidedByCNPG),
			Cluster:  "source-db",
		},
		Artifact: hibernationManifestArtifact{
			ManifestKey: "hibernate/example/snap-1/manifest.json",
			ManifestURI: "s3://archive/hibernate/example/snap-1/manifest.json",
		},
		CNPG: hibernationManifestCNPG{
			BackupName:      "snap-1",
			BackupID:        "20260707T100000",
			DestinationPath: "s3://archive/hibernate/example/snap-1/cnpg",
			ServerName:      "source-db",
			Phase:           string(cnpgv1.BackupPhaseCompleted),
		},
		StorageBackend: hibernationManifestStorageBackend{
			Name:        "archive",
			Bucket:      "archive",
			EndpointURL: "https://s3.eu-west-2.amazonaws.com",
			Region:      "eu-west-2",
		},
	}
}

func bootstrapTamossFixture() *tamossv1alpha1.Tamoss {
	tamoss := hibernateTamossFixture()
	tamoss.Spec.Hibernation = tamossv1alpha1.TamossHibernationSpec{
		ResumeFrom: &tamossv1alpha1.TamossResumeSource{
			Artifact: &tamossv1alpha1.TamossResumeArtifactSource{
				StorageBackendRef: tamossv1alpha1.LocalObjectReference{Name: "archive"},
				ManifestKey:       "hibernate/example/snap-1/manifest.json",
				Checksum:          bootstrapTestChecksum,
			},
		},
	}
	return tamoss
}

func TestResumeBootstrapResolvesArtifactSource(t *testing.T) {
	ctx := context.Background()
	scheme := hibernateTestScheme(t)
	tamoss := bootstrapTamossFixture()
	destination := hibernateDestinationFixture()
	reader := &fakeHibernationManifestReader{manifest: bootstrapManifestFixture(), checksum: bootstrapTestChecksum}

	reconciler := &TamossReconciler{
		Client: fake.NewClientBuilder().WithInterceptorFuncs(fakeApplyInterceptor()).
			WithScheme(scheme).
			WithStatusSubresource(&tamossv1alpha1.Tamoss{}).
			WithObjects(tamoss, destination).
			Build(),
		Scheme:         scheme,
		ManifestReader: reader,
	}

	control, err := reconciler.reconcileResumeBootstrap(ctx, tamoss)
	if err != nil {
		t.Fatalf("expected bootstrap resolution without error, got %v", err)
	}
	if control.Stop {
		t.Fatalf("expected reconciliation to continue after resolution, got %#v", control)
	}
	if reader.key != "hibernate/example/snap-1/manifest.json" {
		t.Fatalf("expected manifest read for the declared key, got %q", reader.key)
	}

	updated := &tamossv1alpha1.Tamoss{}
	if err := reconciler.Client.Get(ctx, types.NamespacedName{Name: tamoss.Name, Namespace: tamoss.Namespace}, updated); err != nil {
		t.Fatalf("get updated Tamoss: %v", err)
	}
	restore := updated.Status.Lifecycle.ResolvedRestore
	if restore == nil {
		t.Fatal("expected resolved restore to be persisted")
	}
	if !restore.Restore.Enabled ||
		restore.Restore.Source != "source-db" ||
		restore.Restore.ObjectStore.DestinationPath != "s3://archive/hibernate/example/snap-1/cnpg" ||
		restore.Restore.ObjectStore.Bucket != "archive" ||
		restore.Restore.ObjectStore.ExistingSecret != "archive-s3" {
		t.Fatalf("unexpected resolved restore: %#v", restore.Restore)
	}
	if restore.Checksum != bootstrapTestChecksum || restore.StorageBackendName != "archive" {
		t.Fatalf("unexpected restore bookkeeping: %#v", restore)
	}
	if updated.Status.Lifecycle.Phase != string(tamossv1alpha1.TamossLifecyclePhaseResuming) {
		t.Fatalf("expected lifecycle Resuming during restore, got %#v", updated.Status.Lifecycle)
	}

	// The renderer sees the restore via injection into the resolved copy.
	injectResolvedRestore(updated)
	if updated.Spec.Backends.DB.CNPG == nil || !updated.Spec.Backends.DB.CNPG.Restore.Enabled {
		t.Fatalf("expected restore injected into the resolved spec, got %#v", updated.Spec.Backends.DB.CNPG)
	}
}

func TestResumeBootstrapFailureHandling(t *testing.T) {
	tests := []struct {
		name        string
		mutate      func(reader *fakeHibernationManifestReader, tamoss *tamossv1alpha1.Tamoss)
		wantPhase   tamossv1alpha1.TamossLifecyclePhase
		wantReason  string
		wantStop    bool
		wantRequeue bool
	}{
		{
			name: "checksum mismatch fails terminally",
			mutate: func(reader *fakeHibernationManifestReader, _ *tamossv1alpha1.Tamoss) {
				reader.checksum = "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
			},
			wantPhase:  tamossv1alpha1.TamossLifecyclePhaseFailed,
			wantReason: operatorstatus.ReasonHibernateManifestChecksumMismatch,
			wantStop:   true,
		},
		{
			name: "transient read error waits",
			mutate: func(reader *fakeHibernationManifestReader, _ *tamossv1alpha1.Tamoss) {
				reader.err = fmt.Errorf("connection refused")
			},
			wantReason:  operatorstatus.ReasonHibernateManifestUnavailable,
			wantStop:    true,
			wantRequeue: true,
		},
		{
			name: "unsupported schema fails terminally",
			mutate: func(reader *fakeHibernationManifestReader, _ *tamossv1alpha1.Tamoss) {
				manifest := reader.manifest
				manifest.Schema.TAMSAPI = "7.0"
				reader.manifest = manifest
			},
			wantPhase:  tamossv1alpha1.TamossLifecyclePhaseFailed,
			wantReason: operatorstatus.ReasonUnsupportedSchemaVersion,
			wantStop:   true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			scheme := hibernateTestScheme(t)
			tamoss := bootstrapTamossFixture()
			destination := hibernateDestinationFixture()
			reader := &fakeHibernationManifestReader{manifest: bootstrapManifestFixture(), checksum: bootstrapTestChecksum}
			test.mutate(reader, tamoss)

			reconciler := &TamossReconciler{
				Client: fake.NewClientBuilder().WithInterceptorFuncs(fakeApplyInterceptor()).
					WithScheme(scheme).
					WithStatusSubresource(&tamossv1alpha1.Tamoss{}).
					WithObjects(tamoss, destination).
					Build(),
				Scheme:         scheme,
				ManifestReader: reader,
			}

			control, err := reconciler.reconcileResumeBootstrap(ctx, tamoss)
			if err != nil {
				t.Fatalf("expected outcome handling without error, got %v", err)
			}
			if control.Stop != test.wantStop {
				t.Fatalf("expected stop=%v, got %#v", test.wantStop, control)
			}
			if test.wantRequeue && control.Result.RequeueAfter <= 0 {
				t.Fatalf("expected wait requeue, got %#v", control.Result)
			}

			updated := &tamossv1alpha1.Tamoss{}
			if err := reconciler.Client.Get(ctx, types.NamespacedName{Name: tamoss.Name, Namespace: tamoss.Namespace}, updated); err != nil {
				t.Fatalf("get updated Tamoss: %v", err)
			}
			if test.wantPhase != "" && updated.Status.Lifecycle.Phase != string(test.wantPhase) {
				t.Fatalf("expected lifecycle phase %s, got %#v", test.wantPhase, updated.Status.Lifecycle)
			}
			if updated.Status.Lifecycle.Reason != test.wantReason {
				t.Fatalf("expected reason %s, got %#v", test.wantReason, updated.Status.Lifecycle)
			}
			if updated.Status.Lifecycle.ResolvedRestore != nil {
				t.Fatalf("expected no resolved restore, got %#v", updated.Status.Lifecycle.ResolvedRestore)
			}
		})
	}
}

func TestResumeBootstrapIgnoredWhenClusterExists(t *testing.T) {
	ctx := context.Background()
	scheme := hibernateTestScheme(t)
	tamoss := bootstrapTamossFixture()
	destination := hibernateDestinationFixture()
	cluster := hibernateClusterFixture(tamoss)
	reader := &fakeHibernationManifestReader{manifest: bootstrapManifestFixture(), checksum: bootstrapTestChecksum}

	reconciler := &TamossReconciler{
		Client: fake.NewClientBuilder().WithInterceptorFuncs(fakeApplyInterceptor()).
			WithScheme(scheme).
			WithStatusSubresource(&tamossv1alpha1.Tamoss{}).
			WithObjects(tamoss, destination, cluster).
			Build(),
		Scheme:         scheme,
		ManifestReader: reader,
	}

	control, err := reconciler.reconcileResumeBootstrap(ctx, tamoss)
	if err != nil || control.Stop {
		t.Fatalf("expected bootstrap to be ignored for an existing cluster, got control %#v err %v", control, err)
	}
	if reader.calls != 0 {
		t.Fatalf("expected no manifest reads for an existing cluster, got %d", reader.calls)
	}
	if tamoss.Status.Lifecycle.ResolvedRestore != nil {
		t.Fatal("expected no resolved restore for an existing cluster")
	}
}

func TestResumeBootstrapWakesFromLastHibernation(t *testing.T) {
	for _, test := range []struct {
		name     string
		previous bool
		declared bool
		invalid  bool
		reused   bool
	}{
		{name: "first cycle"},
		{name: "later cycle", previous: true},
		{name: "later cycle after declared bootstrap", previous: true, declared: true},
		{name: "later cycle reuses a pending archive", previous: true, reused: true},
		{name: "invalid later manifest remains blocked", previous: true, invalid: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			scheme := hibernateTestScheme(t)
			tamoss := hibernateTamossFixture()
			destination := hibernateDestinationFixture()
			hibernate := hibernateFixture()
			hibernate.Status.Phase = string(tamossv1alpha1.TamossOperationPhaseCompleted)
			hibernate.Status.Artifact = tamossv1alpha1.HibernationArtifactStatus{
				Driver:      string(tamossv1alpha1.HibernationDriverCNPGPhysical),
				ManifestKey: "hibernate/example/snap-1/manifest.json",
				Checksum:    bootstrapTestChecksum,
			}
			tamoss.Status.Lifecycle = tamossv1alpha1.TamossLifecycleStatus{
				Phase:            string(tamossv1alpha1.TamossLifecyclePhaseHibernated),
				Reason:           operatorstatus.ReasonTamossHibernated,
				HibernationCycle: 1,
				LastHibernateRef: operationObjectReference(hibernate, "TamossHibernate"),
			}
			reader := &fakeHibernationManifestReader{manifest: bootstrapManifestFixture(), checksum: bootstrapTestChecksum}
			if test.previous {
				resumedAt := metav1.Now()
				tamoss.Status.Lifecycle.HibernationCycle = 2
				tamoss.Status.Lifecycle.ResolvedRestore = &tamossv1alpha1.TamossResolvedRestore{
					HibernationArtifactRetention: tamossv1alpha1.HibernationArtifactRetention{
						ManifestKey:        "previous/manifest.json",
						StorageBackendName: "archive",
						ResumedAt:          &resumedAt,
					},
				}
			}
			if test.declared {
				tamoss.Spec.Hibernation.ResumeFrom = &tamossv1alpha1.TamossResumeSource{
					HibernationRef: &tamossv1alpha1.LocalObjectReference{Name: "previous"},
				}
			}
			if test.invalid {
				reader.manifest.CNPG.Phase = string(cnpgv1.BackupPhaseFailed)
			}
			if test.reused {
				tamoss.Status.Lifecycle.PendingArtifactCleanups = []tamossv1alpha1.HibernationArtifactRetention{{
					StorageBackendName: "archive",
					ManifestKey:        hibernate.Status.Artifact.ManifestKey,
				}}
			}

			reconciler := &TamossReconciler{
				Client: fake.NewClientBuilder().WithInterceptorFuncs(fakeApplyInterceptor()).
					WithScheme(scheme).
					WithStatusSubresource(&tamossv1alpha1.Tamoss{}).
					WithObjects(tamoss, destination, hibernate).
					Build(),
				Scheme:         scheme,
				ManifestReader: reader,
			}

			control, err := reconciler.reconcileResumeBootstrap(ctx, tamoss)
			if test.invalid {
				for attempt := 0; attempt < 2; attempt++ {
					if err != nil || !control.Stop {
						t.Fatalf("expected invalid manifest to block reconcile, got control %#v err %v", control, err)
					}
					if err := reconciler.Client.Get(ctx, types.NamespacedName{Name: tamoss.Name, Namespace: tamoss.Namespace}, tamoss); err != nil {
						t.Fatal(err)
					}
					if tamoss.Status.Lifecycle.Phase != string(tamossv1alpha1.TamossLifecyclePhaseFailed) {
						t.Fatalf("expected failed restore, got %#v", tamoss.Status.Lifecycle)
					}
					if attempt == 0 {
						control, err = reconciler.reconcileResumeBootstrap(ctx, tamoss)
					}
				}
				return
			}
			if err != nil || control.Stop {
				t.Fatalf("expected wake resolution to continue reconcile, got control %#v err %v", control, err)
			}

			updated := &tamossv1alpha1.Tamoss{}
			if err := reconciler.Client.Get(ctx, types.NamespacedName{Name: tamoss.Name, Namespace: tamoss.Namespace}, updated); err != nil {
				t.Fatalf("get updated Tamoss: %v", err)
			}
			if updated.Status.Lifecycle.Phase != string(tamossv1alpha1.TamossLifecyclePhaseResuming) {
				t.Fatalf("expected wake to transition to Resuming, got %#v", updated.Status.Lifecycle)
			}
			if updated.Status.Lifecycle.ResolvedRestore == nil ||
				updated.Status.Lifecycle.ResolvedRestore.ManifestKey != "hibernate/example/snap-1/manifest.json" {
				t.Fatalf("expected restore resolved from the last hibernation, got %#v", updated.Status.Lifecycle.ResolvedRestore)
			}
			if tamossLifecycleBlocksReconcile(updated) {
				t.Fatal("expected Resuming not to gate reconciliation")
			}
			if test.previous && (len(updated.Status.Lifecycle.PendingArtifactCleanups) != 1 ||
				updated.Status.Lifecycle.PendingArtifactCleanups[0].ManifestKey != "previous/manifest.json") {
				t.Fatalf("expected previous archive cleanup to remain recorded, got %#v", updated.Status.Lifecycle.PendingArtifactCleanups)
			}
		})
	}
}

func TestResumeBootstrapValidatesManifest(t *testing.T) {
	manifest := bootstrapManifestFixture()
	if err := validateResumeManifest(manifest, manifest.Artifact.ManifestKey); err != nil {
		t.Fatalf("expected fixture manifest to validate, got %v", err)
	}
	broken := bootstrapManifestFixture()
	broken.CNPG.Phase = "started"
	err := validateResumeManifest(broken, broken.Artifact.ManifestKey)
	if err == nil || !strings.Contains(err.Error(), "not completed") {
		t.Fatalf("expected incomplete backup rejection, got %v", err)
	}
}
