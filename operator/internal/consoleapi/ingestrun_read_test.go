package consoleapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	tamossv1alpha1 "github.com/livewyer-ops/tamoss/operator/api/v1alpha1"
)

type memoryIngestRunReader struct {
	mu         sync.Mutex
	items      []tamossv1alpha1.IngestRun
	listErr    error
	getErr     error
	blockGet   <-chan struct{}
	getStarted chan<- struct{}
	listCalls  int
}

func (r *memoryIngestRunReader) Get(ctx context.Context, key client.ObjectKey, object client.Object, _ ...client.GetOption) error {
	if r.getStarted != nil {
		select {
		case r.getStarted <- struct{}{}:
		default:
		}
	}
	if r.blockGet != nil {
		select {
		case <-r.blockGet:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	if r.getErr != nil {
		return r.getErr
	}
	run, ok := object.(*tamossv1alpha1.IngestRun)
	if !ok {
		return fmt.Errorf("unexpected Get object %T", object)
	}
	for i := range r.items {
		if r.items[i].Namespace == key.Namespace && r.items[i].Name == key.Name {
			r.items[i].DeepCopyInto(run)
			return nil
		}
	}
	return apierrors.NewNotFound(schema.GroupResource{Group: tamossv1alpha1.GroupVersion.Group, Resource: "ingestruns"}, key.Name)
}

func (r *memoryIngestRunReader) List(ctx context.Context, object client.ObjectList, options ...client.ListOption) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.listCalls++
	if r.listErr != nil {
		return r.listErr
	}
	runs, ok := object.(*tamossv1alpha1.IngestRunList)
	if !ok {
		return fmt.Errorf("unexpected List object %T", object)
	}
	listOptions := (&client.ListOptions{}).ApplyOptions(options)
	start := 0
	if listOptions.Continue != "" {
		var err error
		start, err = strconv.Atoi(listOptions.Continue)
		if err != nil || start < 0 || start > len(r.items) {
			return apierrors.NewResourceExpired("invalid continuation")
		}
	}
	limit := len(r.items) - start
	if listOptions.Limit > 0 && int64(limit) > listOptions.Limit {
		limit = int(listOptions.Limit)
	}
	end := start + limit
	runs.Items = append([]tamossv1alpha1.IngestRun(nil), r.items[start:end]...)
	if end < len(r.items) {
		runs.Continue = strconv.Itoa(end)
	}
	return nil
}

func TestIngestRunReadStoreTraversesSparseTenThousandRunHistory(t *testing.T) {
	items := make([]tamossv1alpha1.IngestRun, 10_000)
	want := map[string]bool{}
	for i := range items {
		instance := "other"
		phase := tamossv1alpha1.IngestRunPhaseFailed
		if i%997 == 0 {
			instance = "media"
			phase = tamossv1alpha1.IngestRunPhaseRunning
			want[fmt.Sprintf("run-%05d", i)] = true
		}
		items[i] = ingestRunFixture(fmt.Sprintf("run-%05d", i), instance, phase)
	}
	reader := &memoryIngestRunReader{items: items}
	store := newTestIngestRunStore(t, reader, 32)
	query := IngestRunListQuery{Limit: 25, Phase: tamossv1alpha1.IngestRunPhaseRunning}
	seen := map[string]bool{}
	requests := 0
	for {
		before := reader.listCalls
		page, err := store.List(context.Background(), query)
		if err != nil {
			t.Fatal(err)
		}
		requests++
		if calls := reader.listCalls - before; calls < 1 || calls > 32 {
			t.Fatalf("backend list calls = %d, want 1..32", calls)
		}
		if page.SchemaVersion != "1.0" || page.Page.Limit != 25 {
			t.Fatalf("unexpected page contract: %#v", page)
		}
		for _, run := range page.Items {
			if !want[run.Name] || seen[run.Name] {
				t.Fatalf("unexpected or duplicate projected run %q", run.Name)
			}
			seen[run.Name] = true
		}
		if page.Page.NextCursor == "" {
			break
		}
		query.Cursor = page.Page.NextCursor
	}
	if len(seen) != len(want) {
		t.Fatalf("projected %d sparse runs, want %d", len(seen), len(want))
	}
	if requests < 2 || requests > 20 {
		t.Fatalf("product page requests = %d, want a bounded multi-page traversal", requests)
	}
}

func TestIngestRunCursorIsOpaqueTamperEvidentAndQueryBound(t *testing.T) {
	reader := &memoryIngestRunReader{items: []tamossv1alpha1.IngestRun{
		ingestRunFixture("one", "media", tamossv1alpha1.IngestRunPhaseRunning),
		ingestRunFixture("two", "media", tamossv1alpha1.IngestRunPhaseFailed),
	}}
	store := newTestIngestRunStore(t, reader, 1)
	query := IngestRunListQuery{Limit: 1}
	page, err := store.List(context.Background(), query)
	if err != nil {
		t.Fatal(err)
	}
	if page.Page.NextCursor == "" || page.Page.NextCursor == "1" {
		t.Fatalf("cursor is missing or exposes the raw continuation: %q", page.Page.NextCursor)
	}
	query.Cursor = page.Page.NextCursor
	query.Phase = tamossv1alpha1.IngestRunPhaseRunning
	if _, err := store.List(context.Background(), query); !errors.Is(err, ErrInvalidIngestRunCursor) {
		t.Fatalf("query-bound cursor error = %v, want invalid cursor", err)
	}
	query.Phase = ""
	tampered := []byte(query.Cursor)
	tamperAt := len(tampered) / 2
	if tampered[tamperAt] == 'A' {
		tampered[tamperAt] = 'B'
	} else {
		tampered[tamperAt] = 'A'
	}
	query.Cursor = string(tampered)
	if _, err := store.List(context.Background(), query); !errors.Is(err, ErrInvalidIngestRunCursor) {
		t.Fatalf("tampered cursor error = %v, want invalid cursor", err)
	}
}

func TestIngestRunDetailProjectionRedactsSensitiveFieldsAndDefaults(t *testing.T) {
	verify := false
	run := ingestRunFixture("run", "media", "")
	run.Spec.InputRef = tamossv1alpha1.IngestInputReference{Kind: "ApprovedS3", ID: "input-secret-canary"}
	run.Spec.CredentialProfileRef = &tamossv1alpha1.IngestCredentialProfileReference{Name: "credential-secret-canary"}
	run.Spec.Options.Verify = &verify
	run.Spec.Options.StorageBackendRef = &tamossv1alpha1.IngestStorageBackendReference{Name: "archive"}
	run.Status.TamsinRunID = "run\n" + strings.Repeat("r", maxProjectedTamsinRunIDLength+20)
	run.Status.ResultRef = tamossv1alpha1.IngestRunResultStatus{
		Key: "artifact-secret-canary", SHA256: strings.Repeat("a", 64), Size: 42, MediaType: "application/\njson", Verified: true,
	}
	run.Status.Conditions = []metav1.Condition{{
		Type: "Ready\n", Status: metav1.ConditionFalse, Reason: "InputResolver\tUnavailable", Message: "message-secret-canary", LastTransitionTime: metav1.Now(),
	}}
	detail := ProjectIngestRunDetail(&run)
	encoded, err := json.Marshal(detail)
	if err != nil {
		t.Fatal(err)
	}
	for _, canary := range []string{"input-secret-canary", "credential-secret-canary", "artifact-secret-canary", strings.Repeat("a", 64), "message-secret-canary"} {
		if strings.Contains(string(encoded), canary) {
			t.Fatalf("detail projection leaked %q: %s", canary, encoded)
		}
	}
	if detail.Phase != string(tamossv1alpha1.IngestRunPhasePending) ||
		detail.Profile != string(tamossv1alpha1.IngestRunProfileEditorial) ||
		detail.SizeClass != string(tamossv1alpha1.IngestRunSizeClassStandard) ||
		detail.DesiredState != string(tamossv1alpha1.IngestRunDesiredStateRunning) {
		t.Fatalf("defaulted projection = %#v", detail.IngestRunSummary)
	}
	if detail.Options.Verify || detail.Options.MaxInputs != 1000 || detail.Result == nil || !detail.Result.Present || !detail.Result.Verified {
		t.Fatalf("unexpected detail options/result: %#v", detail)
	}
	if len([]rune(detail.TamsinRunID)) != maxProjectedTamsinRunIDLength {
		t.Fatalf("Tamsin run ID was not bounded: %d", len([]rune(detail.TamsinRunID)))
	}
	if strings.ContainsAny(detail.TamsinRunID+detail.Result.MediaType+detail.Conditions[0].Type+detail.Conditions[0].Reason, "\n\t") {
		t.Fatalf("detail projection retained control characters: %#v", detail)
	}
}

func TestIngestRunDetailProjectionToleratesAnUnsetStorageBackend(t *testing.T) {
	t.Parallel()
	run := ingestRunFixture("run", "media", tamossv1alpha1.IngestRunPhaseRunning)
	if run.Spec.Options.StorageBackendRef != nil {
		t.Fatal("the fixture must leave the optional storage backend reference unset")
	}
	detail := ProjectIngestRunDetail(&run)
	if detail.Options.StorageBackend != "" {
		t.Fatalf("storage backend = %q, want empty for an unset reference", detail.Options.StorageBackend)
	}
	encoded, err := json.Marshal(detail)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "storageBackend") {
		t.Fatalf("an unset storage backend must be omitted from the projection: %s", encoded)
	}
}

func TestIngestRunReadStoreRejectsCrossInstanceDetail(t *testing.T) {
	reader := &memoryIngestRunReader{items: []tamossv1alpha1.IngestRun{
		ingestRunFixture("run", "other", tamossv1alpha1.IngestRunPhaseRunning),
	}}
	store := newTestIngestRunStore(t, reader, 1)
	if _, err := store.Get(context.Background(), "run"); !errors.Is(err, ErrIngestRunNotFound) {
		t.Fatalf("cross-instance Get error = %v, want not found", err)
	}
}

func TestIngestRunReadStoreBoundsConcurrencyAndDeadline(t *testing.T) {
	release := make(chan struct{})
	started := make(chan struct{}, 1)
	reader := &memoryIngestRunReader{
		items:    []tamossv1alpha1.IngestRun{ingestRunFixture("run", "media", tamossv1alpha1.IngestRunPhaseRunning)},
		blockGet: release, getStarted: started,
	}
	store, err := NewIngestRunReadStore(IngestRunReadConfig{
		Reader: reader, Namespace: "tams", Instance: "media", CursorKey: make([]byte, 32),
		RequestTimeout: 100 * time.Millisecond, MaxConcurrentReads: 1, MaxBackendPages: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	first := make(chan error, 1)
	go func() {
		_, err := store.Get(context.Background(), "run")
		first <- err
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("first read did not start")
	}
	if _, err := store.Get(context.Background(), "run"); !errors.Is(err, ErrIngestRunReadBusy) {
		t.Fatalf("concurrent Get error = %v, want busy", err)
	}
	select {
	case err := <-first:
		if !errors.Is(err, ErrIngestRunReadTimeout) {
			t.Fatalf("blocked Get error = %v, want timeout", err)
		}
	case <-time.After(time.Second):
		t.Fatal("blocked Get ignored its deadline")
	}
	close(release)
}

func newTestIngestRunStore(t *testing.T, reader client.Reader, maxBackendPages int) *IngestRunReadStore {
	t.Helper()
	store, err := NewIngestRunReadStore(IngestRunReadConfig{
		Reader: reader, Namespace: "tams", Instance: "media", CursorKey: make([]byte, 32), MaxBackendPages: maxBackendPages,
	})
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func ingestRunFixture(name, instance string, phase tamossv1alpha1.IngestRunPhase) tamossv1alpha1.IngestRun {
	return tamossv1alpha1.IngestRun{
		ObjectMeta: metav1.ObjectMeta{
			Name: name, Namespace: "tams", UID: types.UID("uid-" + name), ResourceVersion: "7", Generation: 2, CreationTimestamp: metav1.NewTime(time.Unix(1_700_000_000, 0)),
		},
		Spec: tamossv1alpha1.IngestRunSpec{
			TamossRef: tamossv1alpha1.TamossReferenceSpec{Name: instance},
			InputRef:  tamossv1alpha1.IngestInputReference{Kind: "StagedObject", ID: "opaque"},
		},
		Status: tamossv1alpha1.IngestRunStatus{Phase: phase, ObservedGeneration: 2},
	}
}
