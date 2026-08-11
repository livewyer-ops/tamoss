package consoleapi

import (
	"context"
	"encoding/json"
	"errors"
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
	authenticator     Authenticator
	heartbeatInterval time.Duration
	maxStreamDuration time.Duration
}

func NewServer(monitor *Monitor, authenticator Authenticator) *Server {
	if authenticator == nil {
		authenticator = rejectAuthenticator{}
	}
	return &Server{
		monitor:           monitor,
		authenticator:     authenticator,
		heartbeatInterval: 15 * time.Second,
		maxStreamDuration: 5 * time.Minute,
	}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET "+HealthPath, s.health)
	mux.HandleFunc("GET "+ReadinessPath, s.readiness)
	mux.Handle("GET "+RuntimePath, s.requireViewer(http.HandlerFunc(s.runtime)))
	mux.Handle("GET "+RuntimeEventsPath, s.requireViewer(http.HandlerFunc(s.runtimeEvents)))
	return securityHeaders(mux)
}

func (s *Server) requireViewer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		identity, err := s.authenticator.Authenticate(r)
		if err != nil {
			switch {
			case errors.Is(err, ErrForbidden):
				writeJSON(w, http.StatusForbidden, map[string]string{
					"code":  "forbidden",
					"error": "access denied",
				})
			default:
				writeJSON(w, http.StatusUnauthorized, map[string]string{
					"code":  "unauthenticated",
					"error": "authentication required",
				})
			}
			return
		}
		if !identity.CanView() {
			writeJSON(w, http.StatusForbidden, map[string]string{
				"code":  "forbidden",
				"error": "access denied",
			})
			return
		}
		next.ServeHTTP(w, r.WithContext(contextWithIdentity(r, identity)))
	})
}

func contextWithIdentity(request *http.Request, identity Identity) context.Context {
	return context.WithValue(request.Context(), identityContextKey{}, identity)
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
	controller := http.NewResponseController(w)
	streamDeadline := time.Now().Add(s.maxStreamDuration)
	if err := controller.SetWriteDeadline(streamDeadline); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "streaming is unavailable"})
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	if _, err := fmt.Fprint(w, "retry: 5000\n\n"); err != nil {
		return
	}
	if err := controller.Flush(); err != nil {
		return
	}

	updates, cancel := s.monitor.Subscribe()
	defer cancel()
	heartbeat := time.NewTicker(s.heartbeatInterval)
	defer heartbeat.Stop()
	streamEnd := streamDeadline
	if s.maxStreamDuration > 0 {
		// Leave net/http time to finish the response while retaining the absolute
		// deadline that unblocks a client which is no longer consuming bytes.
		streamEnd = streamEnd.Add(-min(time.Second, s.maxStreamDuration/2))
	}
	streamLifetime := time.NewTimer(time.Until(streamEnd))
	defer streamLifetime.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-streamLifetime.C:
			return
		case update := <-updates:
			if err := writeRuntimeEvent(w, update); err != nil {
				return
			}
			if err := controller.Flush(); err != nil {
				return
			}
		case <-heartbeat.C:
			if _, err := fmt.Fprint(w, ": keepalive\n\n"); err != nil {
				return
			}
			if err := controller.Flush(); err != nil {
				return
			}
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
