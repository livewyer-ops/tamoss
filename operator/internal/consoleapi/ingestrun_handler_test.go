package consoleapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	tamossv1alpha1 "github.com/livewyer-ops/tamoss/operator/api/v1alpha1"
)

type stubIngestRunReadAPI struct {
	page      IngestRunListPage
	detail    IngestRunDetail
	err       error
	lastQuery IngestRunListQuery
	lastName  string
}

func (s *stubIngestRunReadAPI) List(_ context.Context, query IngestRunListQuery) (IngestRunListPage, error) {
	s.lastQuery = query
	return s.page, s.err
}

func (s *stubIngestRunReadAPI) Get(_ context.Context, name string) (IngestRunDetail, error) {
	s.lastName = name
	return s.detail, s.err
}

func TestIngestRunHandlersParseBoundedQueriesAndRequireViewer(t *testing.T) {
	readAPI := &stubIngestRunReadAPI{page: IngestRunListPage{
		SchemaVersion: "1.0", Items: []IngestRunSummary{}, Page: IngestRunPageInformation{Limit: 50},
	}}
	viewer := staticAuthenticator{identity: viewerIdentity()}
	handler := NewServer(nil, viewer, WithIngestRunReadAPI(readAPI)).Handler()
	request := httptest.NewRequest(http.MethodGet, IngestRunsPath+"?limit=50&phase=Running&cursor=opaque", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("list status = %d, body %s", response.Code, response.Body.String())
	}
	if readAPI.lastQuery.Limit != 50 || readAPI.lastQuery.Phase != tamossv1alpha1.IngestRunPhaseRunning || readAPI.lastQuery.Cursor != "opaque" {
		t.Fatalf("parsed query = %#v", readAPI.lastQuery)
	}

	request = httptest.NewRequest(http.MethodGet, IngestRunsPath+"?unknown=value", nil)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("unknown query status = %d, want 400", response.Code)
	}

	unauthenticated := NewServer(nil, staticAuthenticator{err: ErrUnauthenticated}, WithIngestRunReadAPI(readAPI)).Handler()
	request = httptest.NewRequest(http.MethodGet, IngestRunsPath, nil)
	response = httptest.NewRecorder()
	unauthenticated.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated list status = %d, want 401", response.Code)
	}
}

func TestIngestRunHandlersHideInvalidAndCrossInstanceDetails(t *testing.T) {
	readAPI := &stubIngestRunReadAPI{detail: IngestRunDetail{IngestRunSummary: IngestRunSummary{Name: "run"}}}
	handler := NewServer(nil, staticAuthenticator{identity: viewerIdentity()}, WithIngestRunReadAPI(readAPI)).Handler()
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, IngestRunsPath+"/valid.run", nil))
	if response.Code != http.StatusOK || readAPI.lastName != "valid.run" {
		t.Fatalf("detail response = %d name=%q body=%s", response.Code, readAPI.lastName, response.Body.String())
	}

	response = httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, IngestRunsPath+"/INVALID", nil))
	if response.Code != http.StatusNotFound {
		t.Fatalf("invalid detail status = %d, want 404", response.Code)
	}
}

func TestIngestRunHandlerMapsProjectedErrors(t *testing.T) {
	tests := []struct {
		err    error
		status int
		code   string
	}{
		{ErrInvalidIngestRunCursor, http.StatusBadRequest, "invalid_cursor"},
		{ErrIngestRunCursorExpired, http.StatusGone, "cursor_expired"},
		{ErrIngestRunNotFound, http.StatusNotFound, "not_found"},
		{ErrIngestRunReadBusy, http.StatusTooManyRequests, "busy"},
		{ErrIngestRunReadTimeout, http.StatusGatewayTimeout, "timeout"},
		{fmt.Errorf("%w: kubernetes-secret-canary", ErrIngestRunReadFailed), http.StatusServiceUnavailable, "unavailable"},
		{errors.New("kubernetes-secret-canary"), http.StatusInternalServerError, "internal_error"},
	}
	for _, test := range tests {
		readAPI := &stubIngestRunReadAPI{err: test.err}
		handler := NewServer(nil, staticAuthenticator{identity: viewerIdentity()}, WithIngestRunReadAPI(readAPI)).Handler()
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, IngestRunsPath, nil))
		if response.Code != test.status || !strings.Contains(response.Body.String(), `"code":"`+test.code+`"`) {
			t.Fatalf("error %v response = %d body=%s", test.err, response.Code, response.Body.String())
		}
		if strings.Contains(response.Body.String(), "kubernetes-secret-canary") {
			t.Fatalf("handler leaked backend error: %s", response.Body.String())
		}
	}
}
