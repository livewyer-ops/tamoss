package consoleapi

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/util/validation"
	"sigs.k8s.io/controller-runtime/pkg/client"

	tamossv1alpha1 "github.com/livewyer-ops/tamoss/operator/api/v1alpha1"
)

const (
	defaultCommandTimeout     = 4 * time.Second
	defaultCommandConcurrency = 8
	maxCancelRequestBytes     = 4096
	maxCommandValueLength     = 512
	maxAuditRequestIDLength   = 128
)

type IngestRunCommandClient interface {
	Get(context.Context, client.ObjectKey, client.Object, ...client.GetOption) error
	Patch(context.Context, client.Object, client.Patch, ...client.PatchOption) error
}

type CommandAPIConfig struct {
	Client        IngestRunCommandClient
	Authenticator Authenticator
	Auditor       CommandAuditor
	Namespace     string
	Instance      string
	MaxConcurrent int
}

type CommandAPI struct {
	client          IngestRunCommandClient
	authenticator   Authenticator
	auditor         CommandAuditor
	namespace       string
	instance        string
	commandTimeout  time.Duration
	commandSlots    chan struct{}
	cancelAvailable bool
}

func NewCommandAPI(config CommandAPIConfig) (*CommandAPI, error) {
	namespace := strings.TrimSpace(config.Namespace)
	if namespace == "" {
		return nil, fmt.Errorf("namespace is required")
	}
	instance := strings.TrimSpace(config.Instance)
	if instance == "" {
		return nil, fmt.Errorf("instance is required")
	}
	if config.Authenticator == nil {
		return nil, fmt.Errorf("authenticator is required")
	}
	if config.Auditor == nil {
		return nil, fmt.Errorf("command auditor is required")
	}
	concurrency := config.MaxConcurrent
	if concurrency == 0 {
		concurrency = defaultCommandConcurrency
	}
	if concurrency < 1 || concurrency > 64 {
		return nil, fmt.Errorf("command concurrency must be between 1 and 64")
	}
	return &CommandAPI{
		client:          config.Client,
		authenticator:   config.Authenticator,
		auditor:         config.Auditor,
		namespace:       namespace,
		instance:        instance,
		commandTimeout:  defaultCommandTimeout,
		commandSlots:    make(chan struct{}, concurrency),
		cancelAvailable: config.Client != nil,
	}, nil
}

func (a *CommandAPI) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET "+SessionPath, a.session)
	mux.HandleFunc(IngestRunCancelPathPattern, a.cancelIngestRun)
}

func (a *CommandAPI) session(w http.ResponseWriter, request *http.Request) {
	identity, ok := a.requireIdentity(w, request, RoleViewer)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, sessionResponse(identity, a.cancelAvailable))
}

func (a *CommandAPI) cancelIngestRun(w http.ResponseWriter, request *http.Request) {
	response := &statusCapturingResponseWriter{ResponseWriter: w, status: http.StatusInternalServerError}
	w = response
	requestID := boundedAuditRequestID(request.Header.Get("X-Request-ID"))
	if requestID == "" {
		requestID = newAuditRequestID()
	}
	w.Header().Set("X-Request-ID", requestID)
	audit := CommandAuditRecord{
		RequestID:  requestID,
		Namespace:  a.namespace,
		Instance:   a.instance,
		Command:    "ingest_run.cancel",
		TargetKind: "IngestRun",
		Outcome:    "rejected",
		Reason:     "invalid_request",
	}
	defer func() {
		audit.HTTPStatus = response.status
		a.auditor.Record(context.WithoutCancel(request.Context()), audit)
	}()

	identity, err := a.authenticator.Authenticate(request)
	if err != nil {
		audit.Outcome = "denied"
		if errors.Is(err, ErrForbidden) {
			audit.Reason = "forbidden"
			writeCommandError(w, http.StatusForbidden, "forbidden", "access denied")
			return
		}
		audit.Reason = "unauthenticated"
		writeCommandError(w, http.StatusUnauthorized, "unauthenticated", "authentication required")
		return
	}
	audit.Subject = identity.Subject
	audit.Roles = append([]Role(nil), identity.Roles...)
	audit.AuthMethod = identity.Method

	// Every other endpoint authenticates before it inspects the request, so the
	// target is classified only once the caller is known. Role-independent
	// request-shape errors stay identical for every authenticated caller.
	name := request.PathValue("name")
	if len(validation.IsDNS1123Subdomain(name)) != 0 {
		writeCommandError(w, http.StatusBadRequest, "invalid_target", "the ingest run target is invalid")
		audit.Reason = "invalid_target"
		return
	}
	audit.TargetName = name

	if !identity.HasRole(RoleOperator) {
		audit.Outcome = "denied"
		audit.Reason = "forbidden"
		writeCommandError(w, http.StatusForbidden, "forbidden", "access denied")
		return
	}
	if !sameOriginRequest(request) {
		audit.Reason = "invalid_origin"
		writeCommandError(w, http.StatusForbidden, "invalid_origin", "request origin is not allowed")
		return
	}
	if !requestHasJSONContentType(request) {
		audit.Reason = "unsupported_media_type"
		writeCommandError(w, http.StatusUnsupportedMediaType, "unsupported_media_type", "content type must be application/json")
		return
	}
	if !a.cancelAvailable {
		audit.Outcome = "failed"
		audit.Reason = "command_unavailable"
		writeCommandError(w, http.StatusServiceUnavailable, "command_unavailable", "ingest cancellation is unavailable")
		return
	}

	payload, code, decodeErr := decodeCancelRequest(w, request)
	if decodeErr != nil {
		audit.Reason = code
		switch code {
		case "request_too_large":
			writeCommandError(w, http.StatusRequestEntityTooLarge, code, "request body is too large")
		default:
			writeCommandError(w, http.StatusBadRequest, code, "request body is invalid")
		}
		return
	}
	if !validOpaqueCommandValue(payload.UID) || !validOpaqueCommandValue(payload.Revision) {
		audit.Reason = "invalid_request"
		writeCommandError(w, http.StatusBadRequest, "invalid_request", "uid and revision are required")
		return
	}

	ctx, cancel := context.WithTimeout(request.Context(), a.commandTimeout)
	defer cancel()
	select {
	case a.commandSlots <- struct{}{}:
		defer func() { <-a.commandSlots }()
	default:
		audit.Outcome = "failed"
		audit.Reason = "command_busy"
		w.Header().Set("Retry-After", "1")
		writeCommandError(w, http.StatusTooManyRequests, "command_busy", "ingest cancellation is temporarily busy")
		return
	}

	run := &tamossv1alpha1.IngestRun{}
	key := client.ObjectKey{Namespace: a.namespace, Name: name}
	if err := a.client.Get(ctx, key, run); err != nil {
		switch {
		case apierrors.IsNotFound(err):
			audit.Reason = "run_not_found"
			writeCommandError(w, http.StatusNotFound, "run_not_found", "ingest run was not found")
		case errors.Is(err, context.DeadlineExceeded), errors.Is(ctx.Err(), context.DeadlineExceeded):
			audit.Outcome = "failed"
			audit.Reason = "command_timeout"
			writeCommandError(w, http.StatusGatewayTimeout, "command_timeout", "ingest cancellation timed out")
		default:
			audit.Outcome = "failed"
			audit.Reason = "command_unavailable"
			writeCommandError(w, http.StatusServiceUnavailable, "command_unavailable", "ingest cancellation is unavailable")
		}
		return
	}
	if run.Spec.TamossRef.Name != a.instance {
		audit.Reason = "run_not_found"
		writeCommandError(w, http.StatusNotFound, "run_not_found", "ingest run was not found")
		return
	}
	audit.TargetUID = string(run.UID)
	if string(run.UID) != payload.UID {
		audit.Reason = "object_replaced"
		writeCommandError(w, http.StatusConflict, "object_replaced", "the ingest run identity has changed")
		return
	}
	if run.Spec.DesiredState == tamossv1alpha1.IngestRunDesiredStateCancelled {
		audit.Outcome = "succeeded"
		audit.Reason = "cancel_requested"
		audit.Replay = true
		writeJSON(w, http.StatusOK, CancelIngestRunResponse{Run: ProjectIngestRunDetail(run), Replayed: true})
		return
	}
	if run.ResourceVersion != payload.Revision {
		audit.Reason = "stale_revision"
		writeCommandError(w, http.StatusConflict, "stale_revision", "the ingest run revision has changed")
		return
	}
	if !IsIngestRunCancelable(run) {
		audit.Reason = "run_not_active"
		writeCommandError(w, http.StatusConflict, "run_not_active", "the ingest run is not active")
		return
	}

	original := run.DeepCopy()
	run.Spec.DesiredState = tamossv1alpha1.IngestRunDesiredStateCancelled
	patch := client.MergeFromWithOptions(original, client.MergeFromWithOptimisticLock{})
	if err := a.client.Patch(ctx, run, patch); err != nil {
		switch {
		case apierrors.IsConflict(err):
			audit.Reason = "stale_revision"
			writeCommandError(w, http.StatusConflict, "stale_revision", "the ingest run revision has changed")
		case errors.Is(err, context.DeadlineExceeded), errors.Is(ctx.Err(), context.DeadlineExceeded):
			audit.Outcome = "failed"
			audit.Reason = "command_timeout"
			writeCommandError(w, http.StatusGatewayTimeout, "command_timeout", "ingest cancellation timed out")
		default:
			audit.Outcome = "failed"
			audit.Reason = "command_unavailable"
			writeCommandError(w, http.StatusServiceUnavailable, "command_unavailable", "ingest cancellation is unavailable")
		}
		return
	}
	audit.Outcome = "succeeded"
	audit.Reason = "cancel_requested"
	writeJSON(w, http.StatusOK, CancelIngestRunResponse{Run: ProjectIngestRunDetail(run)})
}

func (a *CommandAPI) requireIdentity(w http.ResponseWriter, request *http.Request, role Role) (Identity, bool) {
	identity, err := a.authenticator.Authenticate(request)
	if err != nil {
		if errors.Is(err, ErrForbidden) {
			writeCommandError(w, http.StatusForbidden, "forbidden", "access denied")
		} else {
			writeCommandError(w, http.StatusUnauthorized, "unauthenticated", "authentication required")
		}
		return Identity{}, false
	}
	if !identity.HasRole(role) {
		writeCommandError(w, http.StatusForbidden, "forbidden", "access denied")
		return Identity{}, false
	}
	return identity, true
}

func decodeCancelRequest(w http.ResponseWriter, request *http.Request) (CancelIngestRunRequest, string, error) {
	if request.ContentLength > maxCancelRequestBytes {
		return CancelIngestRunRequest{}, "request_too_large", errors.New("request body is too large")
	}
	request.Body = http.MaxBytesReader(w, request.Body, maxCancelRequestBytes)
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	var payload CancelIngestRunRequest
	if err := decoder.Decode(&payload); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			return CancelIngestRunRequest{}, "request_too_large", err
		}
		return CancelIngestRunRequest{}, "invalid_request", err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			return CancelIngestRunRequest{}, "request_too_large", err
		}
		return CancelIngestRunRequest{}, "invalid_request", errors.New("multiple JSON values are not allowed")
	}
	return payload, "", nil
}

func requestHasJSONContentType(request *http.Request) bool {
	values := request.Header.Values("Content-Type")
	if len(values) != 1 {
		return false
	}
	mediaType, parameters, err := mime.ParseMediaType(values[0])
	if err != nil || mediaType != "application/json" {
		return false
	}
	for name, value := range parameters {
		if !strings.EqualFold(name, "charset") || !strings.EqualFold(value, "utf-8") {
			return false
		}
	}
	return true
}

func sameOriginRequest(request *http.Request) bool {
	values := request.Header.Values("Origin")
	if len(values) != 1 {
		return false
	}
	origin, err := parseOrigin(values[0])
	if err != nil {
		return false
	}
	scheme, ok := requestScheme(request)
	if !ok {
		return false
	}
	target, err := parseOrigin(scheme + "://" + request.Host)
	if err != nil {
		return false
	}
	return origin.scheme == target.scheme &&
		strings.EqualFold(origin.hostname, target.hostname) &&
		origin.port == target.port
}

type normalizedOrigin struct {
	scheme   string
	hostname string
	port     string
}

func parseOrigin(value string) (normalizedOrigin, error) {
	if value == "" || strings.TrimSpace(value) != value || value == "null" {
		return normalizedOrigin{}, errors.New("invalid origin")
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Opaque != "" || parsed.User != nil || parsed.Host == "" ||
		parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return normalizedOrigin{}, errors.New("invalid origin")
	}
	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		return normalizedOrigin{}, errors.New("invalid origin scheme")
	}
	hostname := parsed.Hostname()
	if hostname == "" {
		return normalizedOrigin{}, errors.New("invalid origin host")
	}
	port := parsed.Port()
	if port == "" {
		if scheme == "https" {
			port = "443"
		} else {
			port = "80"
		}
	}
	return normalizedOrigin{scheme: scheme, hostname: hostname, port: port}, nil
}

func requestScheme(request *http.Request) (string, bool) {
	values := request.Header.Values("X-Forwarded-Proto")
	if len(values) > 0 {
		if len(values) != 1 || (values[0] != "http" && values[0] != "https") {
			return "", false
		}
		return values[0], true
	}
	if request.TLS != nil {
		return "https", true
	}
	return "http", true
}

func validOpaqueCommandValue(value string) bool {
	if value == "" || len(value) > maxCommandValueLength || strings.TrimSpace(value) != value {
		return false
	}
	for _, character := range value {
		if !unicode.IsPrint(character) {
			return false
		}
	}
	return true
}

func boundedAuditRequestID(value string) string {
	if value == "" || len(value) > maxAuditRequestIDLength || strings.TrimSpace(value) != value {
		return ""
	}
	for _, character := range value {
		isAlphaNumeric := character >= 'a' && character <= 'z' ||
			character >= 'A' && character <= 'Z' ||
			character >= '0' && character <= '9'
		if !isAlphaNumeric &&
			character != '.' && character != '_' && character != ':' && character != '-' {
			return ""
		}
	}
	return value
}

func newAuditRequestID() string {
	random := make([]byte, 16)
	if _, err := rand.Read(random); err != nil {
		return "request-id-unavailable"
	}
	return base64.RawURLEncoding.EncodeToString(random)
}

func writeCommandError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]string{jsonCodeKey: code, jsonErrorKey: message})
}

type statusCapturingResponseWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusCapturingResponseWriter) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}
