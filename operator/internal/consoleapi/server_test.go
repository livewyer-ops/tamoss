package consoleapi

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestRuntimeAndProbeHandlers(t *testing.T) {
	t.Parallel()
	monitor := NewMonitor(&queuedSource{results: []sourceResult{{
		snapshot: RuntimeSnapshot{
			SchemaVersion: RuntimeSchemaVersion,
			ObservedAt:    "now",
			Instance:      Instance{Name: "media"},
			Services: []Service{{
				Name:  "media-api",
				Ports: []ServicePort{{Name: "http", Protocol: "TCP", Port: 8000, TargetPort: "http"}},
			}},
			EndpointSlices: []EndpointSlice{{
				Name: "media-api-abc", ServiceName: "media-api", TotalEndpoints: 1, ReadyEndpoints: 1,
			}},
		},
	}}})
	server := NewServer(monitor, NewDevelopmentAnonymousAuthenticator()).Handler()

	assertStatus(t, server, HealthPath, http.StatusOK)
	assertStatus(t, server, ReadinessPath, http.StatusServiceUnavailable)
	assertStatus(t, server, RuntimePath, http.StatusServiceUnavailable)
	if err := monitor.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	assertStatus(t, server, ReadinessPath, http.StatusOK)

	request := httptest.NewRequest(http.MethodGet, RuntimePath, nil)
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("GET runtime status = %d, want 200", response.Code)
	}
	if got := response.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", got)
	}
	if got := response.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("X-Content-Type-Options = %q, want nosniff", got)
	}
	var snapshot RuntimeSnapshot
	if err := json.NewDecoder(response.Body).Decode(&snapshot); err != nil {
		t.Fatal(err)
	}
	if snapshot.Instance.Name != "media" || len(snapshot.Services) != 1 || snapshot.Services[0].Name != "media-api" ||
		len(snapshot.EndpointSlices) != 1 || snapshot.EndpointSlices[0].ReadyEndpoints != 1 {
		t.Fatalf("unexpected runtime response: %#v", snapshot)
	}

	post := httptest.NewRequest(http.MethodPost, RuntimePath, nil)
	postResponse := httptest.NewRecorder()
	server.ServeHTTP(postResponse, post)
	if postResponse.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST runtime status = %d, want 405", postResponse.Code)
	}
}

func TestRuntimeTruncationJSONIsAdditiveAndOptional(t *testing.T) {
	t.Parallel()
	complete, err := json.Marshal(RuntimeSnapshot{SchemaVersion: RuntimeSchemaVersion})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(complete), "ingestRuntimeTruncated") {
		t.Fatalf("complete runtime response contains truncation marker: %s", complete)
	}

	partial, err := json.Marshal(RuntimeSnapshot{
		SchemaVersion:          RuntimeSchemaVersion,
		IngestRuntimeTruncated: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(partial), `"ingestRuntimeTruncated":true`) {
		t.Fatalf("partial runtime response omits truncation marker: %s", partial)
	}
}

func TestRuntimeSnapshotMarshalsCollectionsAsArrays(t *testing.T) {
	t.Parallel()
	snapshot := RuntimeSnapshot{
		Services:       []Service{{Name: "api"}},
		EndpointSlices: []EndpointSlice{{Name: "api-abc"}},
		Workloads:      []Workload{{Name: "api"}},
		Jobs:           []Job{{Name: "ingest"}},
	}
	encoded, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), ":null") {
		t.Fatalf("runtime response contains a null collection: %s", encoded)
	}
	for _, expected := range []string{
		`"conditions":[]`, `"workloads":[`, `"services":[`, `"ports":[]`,
		`"endpointSlices":[`, `"pods":[]`, `"jobs":[`, `"events":[]`,
	} {
		if !strings.Contains(string(encoded), expected) {
			t.Fatalf("runtime response omits %s: %s", expected, encoded)
		}
	}
}

func TestRuntimeEventStreamStartsWithLatestSnapshot(t *testing.T) {
	t.Parallel()
	monitor := NewMonitor(&queuedSource{results: []sourceResult{{
		snapshot: RuntimeSnapshot{SchemaVersion: RuntimeSchemaVersion, Instance: Instance{Phase: "Ready"}},
	}}})
	if err := monitor.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(NewServer(monitor, NewDevelopmentAnonymousAuthenticator()).Handler())
	t.Cleanup(server.Close)

	response, err := server.Client().Get(server.URL + RuntimeEventsPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := response.Body.Close(); err != nil {
			t.Errorf("close SSE response: %v", err)
		}
	})
	if response.StatusCode != http.StatusOK || response.Header.Get("Content-Type") != "text/event-stream" {
		t.Fatalf("unexpected SSE response: status=%d content-type=%q", response.StatusCode, response.Header.Get("Content-Type"))
	}

	scanner := bufio.NewScanner(response.Body)
	lines := make([]string, 0, 8)
	for scanner.Scan() {
		line := scanner.Text()
		lines = append(lines, line)
		if strings.HasPrefix(line, "data: ") {
			break
		}
	}
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "event: runtime") || !strings.Contains(joined, `"phase":"Ready"`) {
		t.Fatalf("unexpected SSE event:\n%s", joined)
	}
}

func TestRuntimeEventStreamStopsOnTheShutdownSignal(t *testing.T) {
	t.Parallel()
	monitor := NewMonitor(&queuedSource{results: []sourceResult{{
		snapshot: RuntimeSnapshot{SchemaVersion: RuntimeSchemaVersion, Instance: Instance{Phase: "Ready"}},
	}}})
	if err := monitor.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	shutdown, stop := context.WithCancel(context.Background())
	t.Cleanup(stop)
	consoleServer := NewServer(
		monitor,
		NewDevelopmentAnonymousAuthenticator(),
		WithShutdownContext(shutdown),
	)
	// Neither the heartbeat nor the stream deadline may end the stream instead.
	consoleServer.heartbeatInterval = time.Hour
	consoleServer.maxStreamDuration = time.Hour
	server := httptest.NewServer(consoleServer.Handler())
	t.Cleanup(server.Close)

	response, err := server.Client().Get(server.URL + RuntimeEventsPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := response.Body.Close(); err != nil {
			t.Errorf("close SSE response: %v", err)
		}
	})
	readFirstSSEEvent(t, response.Body)

	stop()
	// httptest.Server.Close blocks until every in-flight handler has returned.
	closed := make(chan struct{})
	go func() {
		server.Close()
		close(closed)
	}()
	select {
	case <-closed:
	case <-time.After(5 * time.Second):
		t.Fatal("the event stream ignored the shutdown signal")
	}
}

func readFirstSSEEvent(t *testing.T, body io.Reader) {
	t.Helper()
	scanner := bufio.NewScanner(body)
	for scanner.Scan() {
		if strings.HasPrefix(scanner.Text(), "data: ") {
			return
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("read the event stream: %v", err)
	}
	t.Fatal("the event stream closed before its first event")
}

func TestRuntimeEndpointsRequireAuthenticatedViewerButProbesDoNot(t *testing.T) {
	t.Parallel()
	monitor := NewMonitor(&queuedSource{results: []sourceResult{{
		snapshot: RuntimeSnapshot{SchemaVersion: RuntimeSchemaVersion, Instance: Instance{Name: "media"}},
	}}})
	if err := monitor.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	authenticator := newTestForwardAuthAuthenticator(t)
	handler := NewServer(monitor, authenticator).Handler()

	assertStatus(t, handler, HealthPath, http.StatusOK)
	assertStatus(t, handler, ReadinessPath, http.StatusOK)
	assertAuthResponse(t, handler, nil, http.StatusUnauthorized, "unauthenticated")
	assertAuthResponseAtPath(t, handler, RuntimeEventsPath, nil, http.StatusUnauthorized, "unauthenticated")
	assertAuthResponse(t, handler, map[string]string{
		ForwardAuthSecretHeader:  testForwardAuthSecret,
		ForwardAuthSubjectHeader: "user-123",
		ForwardAuthGroupsHeader:  "not-a-console-group",
	}, http.StatusForbidden, "forbidden")
	assertAuthResponse(t, handler, map[string]string{
		ForwardAuthSecretHeader:  testForwardAuthSecret,
		ForwardAuthSubjectHeader: "user-123",
		ForwardAuthGroupsHeader:  "tamoss-viewers",
	}, http.StatusOK, "")
}

func TestNilAuthenticatorFailsClosed(t *testing.T) {
	t.Parallel()
	monitor := NewMonitor(&queuedSource{})
	assertAuthResponse(t, NewServer(monitor, nil).Handler(), nil, http.StatusUnauthorized, "unauthenticated")
}

func TestRuntimeEventStreamHasBoundedAuthenticationLifetime(t *testing.T) {
	t.Parallel()
	monitor := NewMonitor(&queuedSource{results: []sourceResult{{
		snapshot: RuntimeSnapshot{SchemaVersion: RuntimeSchemaVersion, Instance: Instance{Phase: "Ready"}},
	}}})
	if err := monitor.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	consoleServer := NewServer(monitor, NewDevelopmentAnonymousAuthenticator())
	consoleServer.maxStreamDuration = 25 * time.Millisecond
	consoleServer.heartbeatInterval = time.Hour
	server := httptest.NewServer(consoleServer.Handler())
	t.Cleanup(server.Close)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+RuntimeEventsPath, nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(response.Body)
	if closeErr := response.Body.Close(); closeErr != nil {
		t.Errorf("close SSE response: %v", closeErr)
	}
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "event: runtime") || !strings.Contains(string(body), `"phase":"Ready"`) {
		t.Fatalf("unexpected bounded SSE body: %q", body)
	}
}

func TestRuntimeEventStreamDeadlineUnblocksStalledWriter(t *testing.T) {
	monitor := NewMonitor(&queuedSource{})
	consoleServer := NewServer(monitor, NewDevelopmentAnonymousAuthenticator())
	consoleServer.maxStreamDuration = 25 * time.Millisecond
	consoleServer.heartbeatInterval = time.Hour
	handler := consoleServer.Handler()
	w := newDeadlineBlockingResponseWriter()
	t.Cleanup(w.release)

	done := make(chan struct{})
	go func() {
		handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, RuntimeEventsPath, nil))
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("SSE handler remained blocked after its write deadline")
	}
	if w.deadline.IsZero() || w.deadlineSetAt.IsZero() {
		t.Fatal("SSE handler did not set a write deadline")
	}
	if duration := w.deadline.Sub(w.deadlineSetAt); duration > consoleServer.maxStreamDuration {
		t.Fatalf("SSE write deadline duration = %s, want a bounded stream deadline", duration)
	}
}

func TestRuntimeEventStreamStopsWhenFlushFails(t *testing.T) {
	monitor := NewMonitor(&queuedSource{})
	consoleServer := NewServer(monitor, NewDevelopmentAnonymousAuthenticator())
	consoleServer.maxStreamDuration = time.Hour
	consoleServer.heartbeatInterval = time.Hour
	w := newFlushErrorResponseWriter()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	done := make(chan struct{})
	go func() {
		consoleServer.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodGet, RuntimeEventsPath, nil).WithContext(ctx))
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		cancel()
		<-done
		t.Fatal("SSE handler ignored the flush error")
	}
	if w.flushCalls != 1 {
		t.Fatalf("SSE flush calls = %d, want 1", w.flushCalls)
	}
	if w.deadline.IsZero() {
		t.Fatal("SSE handler did not set a write deadline before flushing")
	}
}

type deadlineBlockingResponseWriter struct {
	header          http.Header
	deadline        time.Time
	deadlineSetAt   time.Time
	deadlineReached chan struct{}
	released        chan struct{}
	deadlineOnce    sync.Once
	releaseOnce     sync.Once
}

func newDeadlineBlockingResponseWriter() *deadlineBlockingResponseWriter {
	return &deadlineBlockingResponseWriter{
		header:          make(http.Header),
		deadlineReached: make(chan struct{}),
		released:        make(chan struct{}),
	}
}

func (w *deadlineBlockingResponseWriter) Header() http.Header {
	return w.header
}

func (w *deadlineBlockingResponseWriter) WriteHeader(_ int) {}

func (w *deadlineBlockingResponseWriter) Write(_ []byte) (int, error) {
	select {
	case <-w.deadlineReached:
		return 0, context.DeadlineExceeded
	case <-w.released:
		return 0, io.ErrClosedPipe
	}
}

func (w *deadlineBlockingResponseWriter) FlushError() error {
	return nil
}

func (w *deadlineBlockingResponseWriter) SetWriteDeadline(deadline time.Time) error {
	w.deadline = deadline
	w.deadlineSetAt = time.Now()
	if !deadline.IsZero() {
		time.AfterFunc(time.Until(deadline), func() {
			w.deadlineOnce.Do(func() { close(w.deadlineReached) })
		})
	}
	return nil
}

func (w *deadlineBlockingResponseWriter) release() {
	w.releaseOnce.Do(func() { close(w.released) })
}

type flushErrorResponseWriter struct {
	header     http.Header
	body       strings.Builder
	deadline   time.Time
	flushCalls int
}

func newFlushErrorResponseWriter() *flushErrorResponseWriter {
	return &flushErrorResponseWriter{header: make(http.Header)}
}

func (w *flushErrorResponseWriter) Header() http.Header {
	return w.header
}

func (w *flushErrorResponseWriter) WriteHeader(_ int) {}

func (w *flushErrorResponseWriter) Write(value []byte) (int, error) {
	return w.body.Write(value)
}

func (w *flushErrorResponseWriter) FlushError() error {
	w.flushCalls++
	return io.ErrClosedPipe
}

func (w *flushErrorResponseWriter) SetWriteDeadline(deadline time.Time) error {
	w.deadline = deadline
	return nil
}

func assertAuthResponse(t *testing.T, handler http.Handler, headers map[string]string, wantStatus int, wantCode string) {
	t.Helper()
	assertAuthResponseAtPath(t, handler, RuntimePath, headers, wantStatus, wantCode)
}

func assertAuthResponseAtPath(t *testing.T, handler http.Handler, path string, headers map[string]string, wantStatus int, wantCode string) {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, path, nil)
	for name, value := range headers {
		request.Header.Set(name, value)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != wantStatus {
		t.Fatalf("GET %s status = %d, want %d: %s", path, response.Code, wantStatus, response.Body.String())
	}
	if wantCode == "" {
		return
	}
	rawBody := response.Body.String()
	var body map[string]string
	if err := json.NewDecoder(strings.NewReader(rawBody)).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body["code"] != wantCode {
		t.Fatalf("error code = %q, want %q", body["code"], wantCode)
	}
	if strings.Contains(rawBody, "tamoss-viewers") || strings.Contains(rawBody, "not-a-console-group") {
		t.Fatalf("authorization response disclosed group names: %s", rawBody)
	}
}

func assertStatus(t *testing.T, handler http.Handler, path string, want int) {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, path, nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != want {
		t.Fatalf("GET %s status = %d, want %d", path, response.Code, want)
	}
}
