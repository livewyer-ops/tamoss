package consoleapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"
)

const (
	APIBasePath       = "/ui-api/v1"
	HealthPath        = APIBasePath + "/healthz"
	ReadinessPath     = APIBasePath + "/readyz"
	RuntimePath       = APIBasePath + "/runtime"
	RuntimeEventsPath = RuntimePath + "/events"
)

type Server struct {
	monitor           *Monitor
	heartbeatInterval time.Duration
}

func NewServer(monitor *Monitor) *Server {
	return &Server{
		monitor:           monitor,
		heartbeatInterval: 15 * time.Second,
	}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET "+HealthPath, s.health)
	mux.HandleFunc("GET "+ReadinessPath, s.readiness)
	mux.HandleFunc("GET "+RuntimePath, s.runtime)
	mux.HandleFunc("GET "+RuntimeEventsPath, s.runtimeEvents)
	return securityHeaders(mux)
}

func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) readiness(w http.ResponseWriter, _ *http.Request) {
	_, ready, found := s.monitor.Current()
	if !ready || !found {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "notReady"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}

func (s *Server) runtime(w http.ResponseWriter, _ *http.Request) {
	snapshot, _, found := s.monitor.Current()
	if !found {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "runtime state is not available"})
		return
	}
	writeJSON(w, http.StatusOK, snapshot)
}

func (s *Server) runtimeEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "streaming is unavailable"})
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	_, _ = fmt.Fprint(w, "retry: 5000\n\n")
	flusher.Flush()

	updates, cancel := s.monitor.Subscribe()
	defer cancel()
	heartbeat := time.NewTicker(s.heartbeatInterval)
	defer heartbeat.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case update := <-updates:
			if err := writeRuntimeEvent(w, update); err != nil {
				return
			}
			flusher.Flush()
		case <-heartbeat.C:
			if _, err := fmt.Fprint(w, ": keepalive\n\n"); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

func writeRuntimeEvent(w http.ResponseWriter, update RuntimeUpdate) error {
	data, err := json.Marshal(update.Snapshot)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "id: %s\nevent: runtime\ndata: %s\n\n", strconv.FormatUint(update.ID, 10), data)
	return err
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}
