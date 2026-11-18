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
	IngestRunsPath    = APIBasePath + "/ingest-runs"
	jsonCodeKey       = "code"
	jsonErrorKey      = "error"
)

type Server struct {
	monitor       *Monitor
	authenticator Authenticator
	ingestRuns    IngestRunReadAPI
	commands      *CommandAPI
	// shutdownContext carries the process lifetime into long-lived responses.
	// It is never used as a request-scoped context.
	shutdownContext   context.Context
	heartbeatInterval time.Duration
	maxStreamDuration time.Duration
}

type ServerOption func(*Server)

func WithIngestRunReadAPI(api IngestRunReadAPI) ServerOption {
	return func(server *Server) {
		server.ingestRuns = api
	}
}

func WithCommandAPI(api *CommandAPI) ServerOption {
	return func(server *Server) {
		server.commands = api
	}
}

// WithShutdownContext binds every event stream to the process lifetime.
// net/http does not cancel in-flight request contexts during Shutdown, and an
// SSE heartbeat keeps its connection out of the idle set, so without this
// signal each open stream holds graceful shutdown open until either the stream
// deadline or the shutdown grace period expires. Cancelling the supplied
// context ends the streams at once while ordinary bounded requests still drain.
func WithShutdownContext(ctx context.Context) ServerOption {
	return func(server *Server) {
		if ctx != nil {
			server.shutdownContext = ctx
		}
	}
}

func NewServer(monitor *Monitor, authenticator Authenticator, options ...ServerOption) *Server {
	if authenticator == nil {
		authenticator = rejectAuthenticator{}
	}
	server := &Server{
		monitor:           monitor,
		authenticator:     authenticator,
		shutdownContext:   context.Background(),
		heartbeatInterval: 15 * time.Second,
		maxStreamDuration: 5 * time.Minute,
	}
	for _, option := range options {
		if option != nil {
			option(server)
		}
	}
	return server
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET "+HealthPath, s.health)
	mux.HandleFunc("GET "+ReadinessPath, s.readiness)
	mux.Handle("GET "+RuntimePath, s.requireViewer(http.HandlerFunc(s.runtime)))
	mux.Handle("GET "+RuntimeEventsPath, s.requireViewer(http.HandlerFunc(s.runtimeEvents)))
	mux.Handle("GET "+IngestRunsPath, s.requireViewer(http.HandlerFunc(s.listIngestRuns)))
	mux.Handle("GET "+IngestRunsPath+"/{name}", s.requireViewer(http.HandlerFunc(s.getIngestRun)))
	if s.commands != nil {
		s.commands.RegisterRoutes(mux)
	}
	return securityHeaders(mux)
}

func (s *Server) requireViewer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		identity, err := s.authenticator.Authenticate(r)
		if err != nil {
			switch {
			case errors.Is(err, ErrForbidden):
				writeJSON(w, http.StatusForbidden, map[string]string{
					jsonCodeKey:  "forbidden",
					jsonErrorKey: "access denied",
				})
			default:
				writeJSON(w, http.StatusUnauthorized, map[string]string{
					jsonCodeKey:  "unauthenticated",
					jsonErrorKey: "authentication required",
				})
			}
			return
		}
		if !identity.CanView() {
			writeJSON(w, http.StatusForbidden, map[string]string{
				jsonCodeKey:  "forbidden",
				jsonErrorKey: "access denied",
			})
			return
		}
		next.ServeHTTP(w, r)
	})
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
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{jsonErrorKey: "runtime state is not available"})
		return
	}
	writeJSON(w, http.StatusOK, snapshot)
}

func (s *Server) runtimeEvents(w http.ResponseWriter, r *http.Request) {
	controller := http.NewResponseController(w)
	streamDeadline := time.Now().Add(s.maxStreamDuration)
	if err := controller.SetWriteDeadline(streamDeadline); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{jsonErrorKey: "streaming is unavailable"})
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
		case <-s.shutdownContext.Done():
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
