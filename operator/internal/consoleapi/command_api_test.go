package consoleapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	tamossv1alpha1 "github.com/livewyer-ops/tamoss/operator/api/v1alpha1"
)

type staticAuthenticator struct {
	identity Identity
	err      error
}

func (a staticAuthenticator) Authenticate(*http.Request) (Identity, error) {
	return a.identity, a.err
}

type recordingCommandAuditor struct {
	mu      sync.Mutex
	records []CommandAuditRecord
}

func (a *recordingCommandAuditor) Record(_ context.Context, record CommandAuditRecord) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.records = append(a.records, record)
}

func (a *recordingCommandAuditor) snapshot() []CommandAuditRecord {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]CommandAuditRecord(nil), a.records...)
}

func TestCommandAPISessionReportsAuthorizationAndExecutionGates(t *testing.T) {
	api, _ := newTestCommandAPI(t, nil, operatorIdentity())
	response := serveCommandRequest(api, http.MethodGet, SessionPath, nil, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("session status = %d, body %s", response.Code, response.Body.String())
	}
	var session SessionResponse
	if err := json.Unmarshal(response.Body.Bytes(), &session); err != nil {
		t.Fatal(err)
	}
	capabilities := session.Capabilities.IngestRuns
	if session.SchemaVersion != "1.0" || !capabilities.Read.Available || !capabilities.Read.Allowed {
		t.Fatalf("unexpected read session contract: %#v", session)
	}
	if capabilities.Cancel.Available || !capabilities.Cancel.Allowed {
		t.Fatalf("cancel capability must distinguish unavailable from unauthorized: %#v", capabilities.Cancel)
	}
	if capabilities.Create.Available || capabilities.Retry.Available || capabilities.Create.Reason == "" || capabilities.Retry.Reason == "" {
		t.Fatalf("create/retry gates were not explicit: %#v", capabilities)
	}
}

func TestCommandAPICancelsActiveRunAndAuditsOnce(t *testing.T) {
	run := ingestRunFixture("run", "media", tamossv1alpha1.IngestRunPhaseRunning)
	kube := fakeCommandClient(t, &run)
	api, auditor := newTestCommandAPI(t, kube, operatorIdentity())
	body := cancelBody(t, run.UID, run.ResourceVersion)
	response := serveCommandRequest(api, http.MethodPost, APIBasePath+"/ingest-runs/run/cancel", body, validCommandHeaders())
	if response.Code != http.StatusOK {
		t.Fatalf("cancel status = %d, body %s", response.Code, response.Body.String())
	}
	if response.Header().Get("X-Request-ID") == "" {
		t.Fatal("cancel response omitted its request ID")
	}
	stored := &tamossv1alpha1.IngestRun{}
	if err := kube.Get(context.Background(), client.ObjectKey{Namespace: "tams", Name: "run"}, stored); err != nil {
		t.Fatal(err)
	}
	if stored.Spec.DesiredState != tamossv1alpha1.IngestRunDesiredStateCancelled {
		t.Fatalf("stored desired state = %q, want Cancelled", stored.Spec.DesiredState)
	}
	var result CancelIngestRunResponse
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Replayed || result.Run.DesiredState != string(tamossv1alpha1.IngestRunDesiredStateCancelled) {
		t.Fatalf("unexpected cancellation response: %#v", result)
	}
	records := auditor.snapshot()
	if len(records) != 1 {
		t.Fatalf("audit records = %d, want 1", len(records))
	}
	record := records[0]
	if record.Outcome != "succeeded" || record.Reason != "cancel_requested" || record.HTTPStatus != http.StatusOK ||
		record.Subject != "subject-1" || record.AuthMethod != "forward-auth" || record.TargetUID != string(run.UID) || record.Replay {
		t.Fatalf("unexpected audit record: %#v", record)
	}
}

func TestCommandAPICancellationReplayIgnoresStaleRevision(t *testing.T) {
	run := ingestRunFixture("run", "media", tamossv1alpha1.IngestRunPhaseRunning)
	run.Spec.DesiredState = tamossv1alpha1.IngestRunDesiredStateCancelled
	api, auditor := newTestCommandAPI(t, fakeCommandClient(t, &run), operatorIdentity())
	response := serveCommandRequest(
		api,
		http.MethodPost,
		APIBasePath+"/ingest-runs/run/cancel",
		cancelBody(t, run.UID, "stale-revision"),
		validCommandHeaders(),
	)
	if response.Code != http.StatusOK {
		t.Fatalf("replay status = %d, body %s", response.Code, response.Body.String())
	}
	var result CancelIngestRunResponse
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if !result.Replayed || len(auditor.snapshot()) != 1 || !auditor.snapshot()[0].Replay {
		t.Fatalf("replay was not reported consistently: response=%#v audit=%#v", result, auditor.snapshot())
	}
}

func TestCommandAPIRejectsInvalidCancellationBoundaries(t *testing.T) {
	base := ingestRunFixture("run", "media", tamossv1alpha1.IngestRunPhaseRunning)
	tests := []struct {
		name       string
		identity   Identity
		mutateRun  func(*tamossv1alpha1.IngestRun)
		body       func(t *testing.T, run tamossv1alpha1.IngestRun) []byte
		headers    map[string]string
		wantStatus int
		wantCode   string
	}{
		{name: "viewer", identity: viewerIdentity(), body: validCancelBody, headers: validCommandHeaders(), wantStatus: http.StatusForbidden, wantCode: "forbidden"},
		{name: "missing origin", identity: operatorIdentity(), body: validCancelBody, headers: map[string]string{"Content-Type": "application/json"}, wantStatus: http.StatusForbidden, wantCode: "invalid_origin"},
		{name: "wrong origin", identity: operatorIdentity(), body: validCancelBody, headers: map[string]string{"Content-Type": "application/json", "Origin": "https://attacker.example", "X-Forwarded-Proto": "https"}, wantStatus: http.StatusForbidden, wantCode: "invalid_origin"},
		{name: "duplicate forwarded proto", identity: operatorIdentity(), body: validCancelBody, headers: map[string]string{"Content-Type": "application/json", "Origin": "https://app.example:30443", "X-Forwarded-Proto": "https, http"}, wantStatus: http.StatusForbidden, wantCode: "invalid_origin"},
		{name: "wrong media type", identity: operatorIdentity(), body: validCancelBody, headers: map[string]string{"Content-Type": "text/plain", "Origin": "https://app.example:30443", "X-Forwarded-Proto": "https"}, wantStatus: http.StatusUnsupportedMediaType, wantCode: "unsupported_media_type"},
		{name: "unknown body field", identity: operatorIdentity(), body: func(_ *testing.T, run tamossv1alpha1.IngestRun) []byte {
			return []byte(fmt.Sprintf(`{"uid":%q,"revision":%q,"secret":"canary"}`, run.UID, run.ResourceVersion))
		}, headers: validCommandHeaders(), wantStatus: http.StatusBadRequest, wantCode: "invalid_request"},
		{name: "wrong instance", identity: operatorIdentity(), mutateRun: func(run *tamossv1alpha1.IngestRun) { run.Spec.TamossRef.Name = "other" }, body: validCancelBody, headers: validCommandHeaders(), wantStatus: http.StatusNotFound, wantCode: "run_not_found"},
		{name: "replacement uid", identity: operatorIdentity(), body: func(t *testing.T, run tamossv1alpha1.IngestRun) []byte {
			return cancelBody(t, "replacement", run.ResourceVersion)
		}, headers: validCommandHeaders(), wantStatus: http.StatusConflict, wantCode: "object_replaced"},
		{name: "stale revision", identity: operatorIdentity(), body: func(t *testing.T, run tamossv1alpha1.IngestRun) []byte { return cancelBody(t, run.UID, "old") }, headers: validCommandHeaders(), wantStatus: http.StatusConflict, wantCode: "stale_revision"},
		{name: "terminal", identity: operatorIdentity(), mutateRun: func(run *tamossv1alpha1.IngestRun) { run.Status.Phase = tamossv1alpha1.IngestRunPhaseSucceeded }, body: validCancelBody, headers: validCommandHeaders(), wantStatus: http.StatusConflict, wantCode: "run_not_active"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			run := *base.DeepCopy()
			if test.mutateRun != nil {
				test.mutateRun(&run)
			}
			api, auditor := newTestCommandAPI(t, fakeCommandClient(t, &run), test.identity)
			response := serveCommandRequest(api, http.MethodPost, APIBasePath+"/ingest-runs/run/cancel", test.body(t, run), test.headers)
			if response.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d; body %s", response.Code, test.wantStatus, response.Body.String())
			}
			var failure map[string]string
			if err := json.Unmarshal(response.Body.Bytes(), &failure); err != nil {
				t.Fatal(err)
			}
			if failure["code"] != test.wantCode {
				t.Fatalf("code = %q, want %q", failure["code"], test.wantCode)
			}
			if len(auditor.snapshot()) != 1 || auditor.snapshot()[0].HTTPStatus != test.wantStatus {
				t.Fatalf("audit records = %#v", auditor.snapshot())
			}
		})
	}
}

func TestCommandAPIAuthenticatesBeforeClassifyingTheTarget(t *testing.T) {
	run := ingestRunFixture("run", "media", tamossv1alpha1.IngestRunPhaseRunning)
	const invalidTargetPath = APIBasePath + "/ingest-runs/Invalid_Run/cancel"

	tests := []struct {
		name          string
		authenticator Authenticator
		wantStatus    int
		wantCode      string
		wantReason    string
		wantTarget    string
	}{
		{
			// The reordered handler must not tell an anonymous caller whether the
			// run name it guessed was even well formed.
			name:          "unauthenticated",
			authenticator: staticAuthenticator{err: ErrUnauthenticated},
			wantStatus:    http.StatusUnauthorized,
			wantCode:      "unauthenticated",
			wantReason:    "unauthenticated",
		},
		{
			name:          "forbidden identity",
			authenticator: staticAuthenticator{err: ErrForbidden},
			wantStatus:    http.StatusForbidden,
			wantCode:      "forbidden",
			wantReason:    "forbidden",
		},
		{
			name:          "operator",
			authenticator: staticAuthenticator{identity: operatorIdentity()},
			wantStatus:    http.StatusBadRequest,
			wantCode:      "invalid_target",
			wantReason:    "invalid_target",
		},
		{
			// Authenticated callers keep the response they had before the reorder,
			// whatever their role.
			name:          "viewer",
			authenticator: staticAuthenticator{identity: viewerIdentity()},
			wantStatus:    http.StatusBadRequest,
			wantCode:      "invalid_target",
			wantReason:    "invalid_target",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			api, auditor := newTestCommandAPIWithAuthenticator(t, fakeCommandClient(t, &run), test.authenticator)
			response := serveCommandRequest(api, http.MethodPost, invalidTargetPath, nil, validCommandHeaders())
			if response.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d; body %s", response.Code, test.wantStatus, response.Body.String())
			}
			var failure map[string]string
			if err := json.Unmarshal(response.Body.Bytes(), &failure); err != nil {
				t.Fatal(err)
			}
			if failure["code"] != test.wantCode {
				t.Fatalf("code = %q, want %q", failure["code"], test.wantCode)
			}
			records := auditor.snapshot()
			if len(records) != 1 || records[0].Reason != test.wantReason {
				t.Fatalf("audit records = %#v, want reason %q", records, test.wantReason)
			}
			if records[0].TargetName != test.wantTarget {
				t.Fatalf("audit target name = %q, want %q", records[0].TargetName, test.wantTarget)
			}
		})
	}
}

func TestCommandAPIRateLimitsWithoutQueueing(t *testing.T) {
	run := ingestRunFixture("run", "media", tamossv1alpha1.IngestRunPhaseRunning)
	api, auditor := newTestCommandAPI(t, fakeCommandClient(t, &run), operatorIdentity())
	api.commandSlots <- struct{}{}
	t.Cleanup(func() { <-api.commandSlots })
	response := serveCommandRequest(api, http.MethodPost, APIBasePath+"/ingest-runs/run/cancel", validCancelBody(t, run), validCommandHeaders())
	if response.Code != http.StatusTooManyRequests || response.Header().Get("Retry-After") != "1" {
		t.Fatalf("busy response = %d headers=%v body=%s", response.Code, response.Header(), response.Body.String())
	}
	if len(auditor.snapshot()) != 1 || auditor.snapshot()[0].Reason != "command_busy" {
		t.Fatalf("busy audit = %#v", auditor.snapshot())
	}
}

func TestCommandAPIMapsPatchConflictWithoutLeakingKubernetesError(t *testing.T) {
	run := ingestRunFixture("run", "media", tamossv1alpha1.IngestRunPhaseRunning)
	client := &conflictingCommandClient{run: run}
	api, auditor := newTestCommandAPI(t, client, operatorIdentity())
	response := serveCommandRequest(api, http.MethodPost, APIBasePath+"/ingest-runs/run/cancel", validCancelBody(t, run), validCommandHeaders())
	if response.Code != http.StatusConflict || strings.Contains(response.Body.String(), "kubernetes-secret-canary") {
		t.Fatalf("conflict response = %d body=%s", response.Code, response.Body.String())
	}
	if len(auditor.snapshot()) != 1 || auditor.snapshot()[0].Reason != "stale_revision" {
		t.Fatalf("conflict audit = %#v", auditor.snapshot())
	}
}

type conflictingCommandClient struct {
	run tamossv1alpha1.IngestRun
}

func (c *conflictingCommandClient) Get(_ context.Context, _ client.ObjectKey, object client.Object, _ ...client.GetOption) error {
	c.run.DeepCopyInto(object.(*tamossv1alpha1.IngestRun))
	return nil
}

func (c *conflictingCommandClient) Patch(context.Context, client.Object, client.Patch, ...client.PatchOption) error {
	return apierrors.NewConflict(schema.GroupResource{Group: tamossv1alpha1.GroupVersion.Group, Resource: "ingestruns"}, c.run.Name, errors.New("kubernetes-secret-canary"))
}

func newTestCommandAPI(t *testing.T, kube IngestRunCommandClient, identity Identity) (*CommandAPI, *recordingCommandAuditor) {
	t.Helper()
	return newTestCommandAPIWithAuthenticator(t, kube, staticAuthenticator{identity: identity})
}

func newTestCommandAPIWithAuthenticator(
	t *testing.T,
	kube IngestRunCommandClient,
	authenticator Authenticator,
) (*CommandAPI, *recordingCommandAuditor) {
	t.Helper()
	auditor := &recordingCommandAuditor{}
	api, err := NewCommandAPI(CommandAPIConfig{
		Client: kube, Authenticator: authenticator, Auditor: auditor,
		Namespace: "tams", Instance: "media", MaxConcurrent: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	return api, auditor
}

func fakeCommandClient(t *testing.T, objects ...client.Object) client.Client {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := tamossv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	return fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build()
}

func serveCommandRequest(api *CommandAPI, method, path string, body []byte, headers map[string]string) *httptest.ResponseRecorder {
	mux := http.NewServeMux()
	api.RegisterRoutes(mux)
	request := httptest.NewRequest(method, "https://app.example:30443"+path, bytes.NewReader(body))
	for name, value := range headers {
		request.Header.Set(name, value)
	}
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, request)
	return response
}

func validCommandHeaders() map[string]string {
	return map[string]string{
		"Content-Type":      "application/json; charset=utf-8",
		"Origin":            "https://app.example:30443",
		"X-Forwarded-Proto": "https",
		"X-Request-ID":      "request-123",
	}
}

func cancelBody(t *testing.T, uid types.UID, revision string) []byte {
	t.Helper()
	encoded, err := json.Marshal(CancelIngestRunRequest{UID: string(uid), Revision: revision})
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}

func validCancelBody(t *testing.T, run tamossv1alpha1.IngestRun) []byte {
	t.Helper()
	return cancelBody(t, run.UID, run.ResourceVersion)
}

func operatorIdentity() Identity {
	return Identity{Subject: "subject-1", Username: "operator", Method: "forward-auth", Roles: []Role{RoleViewer, RoleOperator}}
}

func viewerIdentity() Identity {
	return Identity{Subject: "subject-2", Username: "viewer", Method: "forward-auth", Roles: []Role{RoleViewer}}
}
