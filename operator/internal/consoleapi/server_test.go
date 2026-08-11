package consoleapi

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
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
	server := NewServer(monitor).Handler()

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

func TestRuntimeEventStreamStartsWithLatestSnapshot(t *testing.T) {
	t.Parallel()
	monitor := NewMonitor(&queuedSource{results: []sourceResult{{
		snapshot: RuntimeSnapshot{SchemaVersion: RuntimeSchemaVersion, Instance: Instance{Phase: "Ready"}},
	}}})
	if err := monitor.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(NewServer(monitor).Handler())
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

func assertStatus(t *testing.T, handler http.Handler, path string, want int) {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, path, nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != want {
		t.Fatalf("GET %s status = %d, want %d", path, response.Code, want)
	}
}
