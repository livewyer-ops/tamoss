package consoleapi

import (
	"errors"
	"net/http"
	"net/url"
	"strconv"

	"k8s.io/apimachinery/pkg/util/validation"

	tamossv1alpha1 "github.com/livewyer-ops/tamoss/operator/api/v1alpha1"
)

var errInvalidIngestRunQuery = errors.New("invalid ingest run query")

func (s *Server) listIngestRuns(w http.ResponseWriter, r *http.Request) {
	if s.ingestRuns == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{
			jsonCodeKey:  "unavailable",
			jsonErrorKey: "ingest history is unavailable",
		})
		return
	}
	query, err := parseIngestRunListQuery(r.URL.RawQuery)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			jsonCodeKey:  "invalid_query",
			jsonErrorKey: "invalid ingest run query",
		})
		return
	}
	page, err := s.ingestRuns.List(r.Context(), query)
	if err != nil {
		writeIngestRunReadError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, page)
}

func (s *Server) getIngestRun(w http.ResponseWriter, r *http.Request) {
	if s.ingestRuns == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{
			jsonCodeKey:  "unavailable",
			jsonErrorKey: "ingest history is unavailable",
		})
		return
	}
	name := r.PathValue("name")
	if problems := validation.IsDNS1123Subdomain(name); len(problems) != 0 {
		writeJSON(w, http.StatusNotFound, map[string]string{
			jsonCodeKey:  "not_found",
			jsonErrorKey: "ingest run was not found",
		})
		return
	}
	run, err := s.ingestRuns.Get(r.Context(), name)
	if err != nil {
		writeIngestRunReadError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, run)
}

func parseIngestRunListQuery(rawQuery string) (IngestRunListQuery, error) {
	values, err := url.ParseQuery(rawQuery)
	if err != nil {
		return IngestRunListQuery{}, errInvalidIngestRunQuery
	}
	for key, entries := range values {
		switch key {
		case "limit", "phase", "cursor":
		default:
			return IngestRunListQuery{}, errInvalidIngestRunQuery
		}
		if len(entries) != 1 || entries[0] == "" {
			return IngestRunListQuery{}, errInvalidIngestRunQuery
		}
	}
	query := IngestRunListQuery{Limit: defaultIngestRunListLimit}
	if encoded := values.Get("limit"); encoded != "" {
		limit, err := strconv.Atoi(encoded)
		if err != nil || limit < 1 || limit > maxIngestRunListLimit {
			return IngestRunListQuery{}, errInvalidIngestRunQuery
		}
		query.Limit = limit
	}
	if phase := values.Get("phase"); phase != "" {
		query.Phase = tamossv1alpha1.IngestRunPhase(phase)
		if !validIngestRunPhase(query.Phase) {
			return IngestRunListQuery{}, errInvalidIngestRunQuery
		}
	}
	query.Cursor = values.Get("cursor")
	if len(query.Cursor) > maxIngestRunCursorLength {
		return IngestRunListQuery{}, errInvalidIngestRunQuery
	}
	return query, nil
}

func writeIngestRunReadError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrInvalidIngestRunCursor):
		writeJSON(w, http.StatusBadRequest, map[string]string{
			jsonCodeKey:  "invalid_cursor",
			jsonErrorKey: "ingest run cursor is invalid",
		})
	case errors.Is(err, ErrIngestRunCursorExpired):
		writeJSON(w, http.StatusGone, map[string]string{
			jsonCodeKey:  "cursor_expired",
			jsonErrorKey: "ingest run cursor expired; restart pagination",
		})
	case errors.Is(err, ErrIngestRunNotFound):
		writeJSON(w, http.StatusNotFound, map[string]string{
			jsonCodeKey:  "not_found",
			jsonErrorKey: "ingest run was not found",
		})
	case errors.Is(err, ErrIngestRunReadBusy):
		w.Header().Set("Retry-After", "1")
		writeJSON(w, http.StatusTooManyRequests, map[string]string{
			jsonCodeKey:  "busy",
			jsonErrorKey: "ingest history is temporarily busy",
		})
	case errors.Is(err, ErrIngestRunReadTimeout):
		writeJSON(w, http.StatusGatewayTimeout, map[string]string{
			jsonCodeKey:  "timeout",
			jsonErrorKey: "ingest history did not respond in time",
		})
	case errors.Is(err, ErrIngestRunReadFailed):
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{
			jsonCodeKey:  "unavailable",
			jsonErrorKey: "ingest history is unavailable",
		})
	default:
		writeJSON(w, http.StatusInternalServerError, map[string]string{
			jsonCodeKey:  "internal_error",
			jsonErrorKey: "ingest history request failed",
		})
	}
}
