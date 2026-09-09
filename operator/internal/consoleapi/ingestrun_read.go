package consoleapi

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"slices"
	"strings"
	"time"
	"unicode"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	tamossv1alpha1 "github.com/livewyer-ops/tamoss/operator/api/v1alpha1"
)

const (
	IngestRunReadSchemaVersion      = "1.0"
	defaultIngestRunListLimit       = 25
	maxIngestRunListLimit           = 100
	defaultIngestRunRequestTimeout  = 4 * time.Second
	defaultIngestRunReadConcurrency = 4
	defaultIngestRunBackendPages    = 32
	ingestRunCursorVersion          = byte(1)
	maxIngestRunCursorLength        = 16 << 10
	maxProjectedConditions          = 16
	maxProjectedTamsinRunIDLength   = 128
	maxProjectedConditionTypeLength = 128
	maxProjectedConditionReason     = 256
	maxProjectedMediaTypeLength     = 255
	maxProjectedOutputLabelLength   = 256
	maxProjectedOutputDescription   = 4096
	maxProjectedOutputTagName       = 256
	maxProjectedOutputTagValue      = 4096
	maxProjectedOutputTags          = 32
	maxProjectedOutputMemberFlows   = 16
)

var (
	ErrInvalidIngestRunCursor = errors.New("invalid ingest run cursor")
	ErrIngestRunCursorExpired = errors.New("ingest run cursor expired")
	ErrIngestRunNotFound      = errors.New("ingest run not found")
	ErrIngestRunReadBusy      = errors.New("ingest run reader is busy")
	ErrIngestRunReadTimeout   = errors.New("ingest run read timed out")
	ErrIngestRunReadFailed    = errors.New("ingest run read failed")
)

type IngestRunReadAPI interface {
	List(context.Context, IngestRunListQuery) (IngestRunListPage, error)
	Get(context.Context, string) (IngestRunDetail, error)
}

type IngestRunReadConfig struct {
	Reader             client.Reader
	Namespace          string
	Instance           string
	CursorKey          []byte
	RequestTimeout     time.Duration
	MaxConcurrentReads int
	MaxBackendPages    int
}

type IngestRunListQuery struct {
	Limit  int
	Phase  tamossv1alpha1.IngestRunPhase
	Cursor string
}

type IngestRunListPage struct {
	SchemaVersion string                   `json:"schemaVersion"`
	Items         []IngestRunSummary       `json:"items"`
	Page          IngestRunPageInformation `json:"page"`
}

type IngestRunPageInformation struct {
	Limit      int    `json:"limit"`
	NextCursor string `json:"nextCursor,omitempty"`
}

type IngestRunSummary struct {
	Name         string            `json:"name"`
	UID          string            `json:"uid"`
	Revision     string            `json:"revision"`
	Phase        string            `json:"phase"`
	Profile      string            `json:"profile"`
	SizeClass    string            `json:"sizeClass"`
	DesiredState string            `json:"desiredState"`
	Attempt      int32             `json:"attempt"`
	CreatedAt    string            `json:"createdAt"`
	StartedAt    string            `json:"startedAt,omitempty"`
	CompletedAt  string            `json:"completedAt,omitempty"`
	Progress     IngestRunProgress `json:"progress"`
	Cancellable  bool              `json:"cancellable"`
}

type IngestRunProgress struct {
	InputsTotal     int32 `json:"inputsTotal"`
	InputsCompleted int32 `json:"inputsCompleted"`
	InputsSucceeded int32 `json:"inputsSucceeded"`
	InputsFailed    int32 `json:"inputsFailed"`
	BytesUploaded   int64 `json:"bytesUploaded"`
}

type IngestRunDetail struct {
	IngestRunSummary
	Generation         int64                     `json:"generation"`
	ObservedGeneration int64                     `json:"observedGeneration"`
	InputKind          string                    `json:"inputKind"`
	Options            IngestRunOptions          `json:"options"`
	OutputIntent       *IngestRunOutputIntent    `json:"outputIntent,omitempty"`
	Job                *IngestRunObjectReference `json:"job,omitempty"`
	TamsinRunID        string                    `json:"tamsinRunId,omitempty"`
	RetryOf            *IngestRunObjectReference `json:"retryOf,omitempty"`
	Result             *IngestRunResult          `json:"result,omitempty"`
	Output             *IngestRunOutput          `json:"output,omitempty"`
	Conditions         []IngestRunCondition      `json:"conditions"`
}

type IngestRunOptions struct {
	StorageBackend   string                     `json:"storageBackend,omitempty"`
	Verify           bool                       `json:"verify"`
	DryRun           bool                       `json:"dryRun"`
	MaxInputs        int32                      `json:"maxInputs"`
	Concurrency      int32                      `json:"concurrency"`
	TAMSFlowProfiles []IngestRunTAMSFlowProfile `json:"tamsFlowProfiles,omitempty"`
}

type IngestRunTAMSFlowProfile struct {
	Format            string `json:"format"`
	Index             int32  `json:"index"`
	ProfileID         string `json:"profileID,omitempty"`
	ProfileRef        string `json:"profileRef,omitempty"`
	ResolvedProfileID string `json:"resolvedProfileID,omitempty"`
}

type IngestRunOutputIntent struct {
	FlowMetadata IngestRunFlowMetadata `json:"flowMetadata"`
}

type IngestRunFlowMetadata struct {
	Label       string         `json:"label,omitempty"`
	Description string         `json:"description,omitempty"`
	Tags        map[string]any `json:"tags,omitempty"`
}

type IngestRunObjectReference struct {
	Name string `json:"name"`
	UID  string `json:"uid"`
}

type IngestRunResult struct {
	Present   bool   `json:"present"`
	Size      int64  `json:"size,omitempty"`
	MediaType string `json:"mediaType,omitempty"`
	Verified  bool   `json:"verified"`
}

type IngestRunOutputFlow struct {
	ID     string `json:"id"`
	Format string `json:"format,omitempty"`
	Role   string `json:"role,omitempty"`
}

type IngestRunOutput struct {
	RootFlowID           string                `json:"rootFlowID"`
	SourceID             string                `json:"sourceID"`
	MemberFlows          []IngestRunOutputFlow `json:"memberFlows,omitempty"`
	MemberFlowsTruncated bool                  `json:"memberFlowsTruncated,omitempty"`
}

type IngestRunCondition struct {
	Type               string `json:"type"`
	Status             string `json:"status"`
	Reason             string `json:"reason,omitempty"`
	LastTransitionTime string `json:"lastTransitionTime,omitempty"`
}

type IngestRunReadStore struct {
	reader          client.Reader
	namespace       string
	instance        string
	cursor          ingestRunCursorCodec
	requestTimeout  time.Duration
	readSlots       chan struct{}
	maxBackendPages int
}

type ingestRunCursorCodec struct {
	aead cipher.AEAD
}

type ingestRunCursorPayload struct {
	Continue  string `json:"continue"`
	QueryHash string `json:"queryHash"`
}

func NewIngestRunReadStore(config IngestRunReadConfig) (*IngestRunReadStore, error) {
	if config.Reader == nil {
		return nil, fmt.Errorf("ingest run reader is required")
	}
	config.Namespace = strings.TrimSpace(config.Namespace)
	config.Instance = strings.TrimSpace(config.Instance)
	if config.Namespace == "" || config.Instance == "" {
		return nil, fmt.Errorf("ingest run namespace and instance are required")
	}
	key := append([]byte(nil), config.CursorKey...)
	if len(key) == 0 {
		key = make([]byte, 32)
		if _, err := io.ReadFull(rand.Reader, key); err != nil {
			return nil, fmt.Errorf("generate IngestRun cursor key: %w", err)
		}
	}
	if len(key) != 32 {
		return nil, fmt.Errorf("ingest run cursor key must be 32 bytes")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("create IngestRun cursor cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create IngestRun cursor codec: %w", err)
	}
	requestTimeout := config.RequestTimeout
	if requestTimeout == 0 {
		requestTimeout = defaultIngestRunRequestTimeout
	}
	if requestTimeout < 100*time.Millisecond || requestTimeout > 30*time.Second {
		return nil, fmt.Errorf("ingest run request timeout must be between 100ms and 30s")
	}
	maxConcurrentReads := config.MaxConcurrentReads
	if maxConcurrentReads == 0 {
		maxConcurrentReads = defaultIngestRunReadConcurrency
	}
	if maxConcurrentReads < 1 || maxConcurrentReads > 64 {
		return nil, fmt.Errorf("ingest run read concurrency must be between 1 and 64")
	}
	maxBackendPages := config.MaxBackendPages
	if maxBackendPages == 0 {
		maxBackendPages = defaultIngestRunBackendPages
	}
	if maxBackendPages < 1 || maxBackendPages > 128 {
		return nil, fmt.Errorf("ingest run backend page limit must be between 1 and 128")
	}
	return &IngestRunReadStore{
		reader:          config.Reader,
		namespace:       config.Namespace,
		instance:        config.Instance,
		cursor:          ingestRunCursorCodec{aead: aead},
		requestTimeout:  requestTimeout,
		readSlots:       make(chan struct{}, maxConcurrentReads),
		maxBackendPages: maxBackendPages,
	}, nil
}

// DeriveIngestRunCursorKey creates a purpose-specific key from the Console's
// existing proof material. It lets replicas exchange cursors without adding a
// second deployment Secret.
func DeriveIngestRunCursorKey(proof []byte) []byte {
	mac := hmac.New(sha256.New, proof)
	_, _ = mac.Write([]byte("tamoss-console-api/ingest-run-cursor/v1"))
	return mac.Sum(nil)
}

func (s *IngestRunReadStore) List(ctx context.Context, query IngestRunListQuery) (IngestRunListPage, error) {
	if query.Limit == 0 {
		query.Limit = defaultIngestRunListLimit
	}
	if query.Limit < 1 || query.Limit > maxIngestRunListLimit || !validIngestRunPhase(query.Phase) {
		return IngestRunListPage{}, fmt.Errorf("%w: invalid list query", ErrIngestRunReadFailed)
	}
	ctx, release, err := s.acquire(ctx)
	if err != nil {
		return IngestRunListPage{}, err
	}
	defer release()

	continueToken := ""
	if query.Cursor != "" {
		continueToken, err = s.cursor.decode(query.Cursor, s.queryHash(query))
		if err != nil {
			return IngestRunListPage{}, err
		}
	}
	page := IngestRunListPage{
		SchemaVersion: IngestRunReadSchemaVersion,
		Items:         make([]IngestRunSummary, 0, query.Limit),
		Page:          IngestRunPageInformation{Limit: query.Limit},
	}
	for backendPage := 0; backendPage < s.maxBackendPages; backendPage++ {
		remaining := query.Limit - len(page.Items)
		if remaining == 0 {
			break
		}
		runs := &tamossv1alpha1.IngestRunList{}
		if err := s.reader.List(ctx, runs, &client.ListOptions{
			Namespace: s.namespace,
			Limit:     int64(remaining),
			Continue:  continueToken,
		}); err != nil {
			return IngestRunListPage{}, classifyIngestRunReadError(err, query.Cursor != "")
		}
		for index := range runs.Items {
			run := &runs.Items[index]
			if run.Namespace != s.namespace || run.Spec.TamossRef.Name != s.instance {
				continue
			}
			if query.Phase != "" && effectiveIngestRunPhase(run) != query.Phase {
				continue
			}
			page.Items = append(page.Items, ProjectIngestRunSummary(run))
		}
		next := runs.Continue
		if next == "" {
			continueToken = ""
			break
		}
		if next == continueToken {
			return IngestRunListPage{}, fmt.Errorf("%w: Kubernetes returned a non-advancing continuation", ErrIngestRunReadFailed)
		}
		continueToken = next
		if len(page.Items) == query.Limit {
			break
		}
	}
	if continueToken != "" {
		page.Page.NextCursor, err = s.cursor.encode(continueToken, s.queryHash(query))
		if err != nil {
			return IngestRunListPage{}, fmt.Errorf("%w: encode continuation", ErrIngestRunReadFailed)
		}
	}
	return page, nil
}

func (s *IngestRunReadStore) Get(ctx context.Context, name string) (IngestRunDetail, error) {
	ctx, release, err := s.acquire(ctx)
	if err != nil {
		return IngestRunDetail{}, err
	}
	defer release()
	run := &tamossv1alpha1.IngestRun{}
	if err := s.reader.Get(ctx, types.NamespacedName{Namespace: s.namespace, Name: name}, run); err != nil {
		if apierrors.IsNotFound(err) {
			return IngestRunDetail{}, ErrIngestRunNotFound
		}
		return IngestRunDetail{}, classifyIngestRunReadError(err, false)
	}
	if run.Namespace != s.namespace || run.Spec.TamossRef.Name != s.instance {
		return IngestRunDetail{}, ErrIngestRunNotFound
	}
	return ProjectIngestRunDetail(run), nil
}

func (s *IngestRunReadStore) acquire(parent context.Context) (context.Context, func(), error) {
	if err := parent.Err(); err != nil {
		return nil, nil, ErrIngestRunReadTimeout
	}
	select {
	case s.readSlots <- struct{}{}:
		ctx, cancel := context.WithTimeout(parent, s.requestTimeout)
		return ctx, func() {
			<-s.readSlots
			cancel()
		}, nil
	default:
		return nil, nil, ErrIngestRunReadBusy
	}
}

func (s *IngestRunReadStore) queryHash(query IngestRunListQuery) string {
	digest := sha256.Sum256([]byte(fmt.Sprintf("v1\x00%s\x00%s\x00%d\x00%s", s.namespace, s.instance, query.Limit, query.Phase)))
	return base64.RawURLEncoding.EncodeToString(digest[:])
}

func (c ingestRunCursorCodec) encode(continueToken, queryHash string) (string, error) {
	if continueToken == "" {
		return "", ErrInvalidIngestRunCursor
	}
	payload, err := json.Marshal(ingestRunCursorPayload{Continue: continueToken, QueryHash: queryHash})
	if err != nil {
		return "", err
	}
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	sealed := c.aead.Seal(nil, nonce, payload, []byte{ingestRunCursorVersion})
	wire := make([]byte, 1, 1+len(nonce)+len(sealed))
	wire[0] = ingestRunCursorVersion
	wire = append(wire, nonce...)
	wire = append(wire, sealed...)
	return base64.RawURLEncoding.EncodeToString(wire), nil
}

func (c ingestRunCursorCodec) decode(encoded, queryHash string) (string, error) {
	if encoded == "" || len(encoded) > maxIngestRunCursorLength {
		return "", ErrInvalidIngestRunCursor
	}
	wire, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil || len(wire) < 1+c.aead.NonceSize()+c.aead.Overhead() || wire[0] != ingestRunCursorVersion {
		return "", ErrInvalidIngestRunCursor
	}
	nonceEnd := 1 + c.aead.NonceSize()
	payload, err := c.aead.Open(nil, wire[1:nonceEnd], wire[nonceEnd:], wire[:1])
	if err != nil {
		return "", ErrInvalidIngestRunCursor
	}
	var cursor ingestRunCursorPayload
	if err := json.Unmarshal(payload, &cursor); err != nil || cursor.Continue == "" || cursor.QueryHash == "" {
		return "", ErrInvalidIngestRunCursor
	}
	if !hmac.Equal([]byte(cursor.QueryHash), []byte(queryHash)) {
		return "", ErrInvalidIngestRunCursor
	}
	return cursor.Continue, nil
}

func classifyIngestRunReadError(err error, usedCursor bool) error {
	if usedCursor && (apierrors.IsResourceExpired(err) || apierrors.IsGone(err)) {
		return ErrIngestRunCursorExpired
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return ErrIngestRunReadTimeout
	}
	return fmt.Errorf("%w: %v", ErrIngestRunReadFailed, err)
}

func validIngestRunPhase(phase tamossv1alpha1.IngestRunPhase) bool {
	switch phase {
	case "",
		tamossv1alpha1.IngestRunPhasePending,
		tamossv1alpha1.IngestRunPhaseQueued,
		tamossv1alpha1.IngestRunPhaseRunning,
		tamossv1alpha1.IngestRunPhaseSucceeded,
		tamossv1alpha1.IngestRunPhasePartiallySucceeded,
		tamossv1alpha1.IngestRunPhaseFailed,
		tamossv1alpha1.IngestRunPhaseCancelled:
		return true
	default:
		return false
	}
}

func ProjectIngestRunSummary(run *tamossv1alpha1.IngestRun) IngestRunSummary {
	return IngestRunSummary{
		Name:         run.Name,
		UID:          string(run.UID),
		Revision:     run.ResourceVersion,
		Phase:        string(effectiveIngestRunPhase(run)),
		Profile:      string(effectiveIngestRunProfile(run)),
		SizeClass:    string(effectiveIngestRunSizeClass(run)),
		DesiredState: string(effectiveIngestRunDesiredState(run)),
		Attempt:      run.Status.Attempt,
		CreatedAt:    projectTimestamp(&run.CreationTimestamp),
		StartedAt:    projectTimestamp(run.Status.StartedAt),
		CompletedAt:  projectTimestamp(run.Status.CompletedAt),
		Progress: IngestRunProgress{
			InputsTotal:     run.Status.Progress.InputsTotal,
			InputsCompleted: run.Status.Progress.InputsCompleted,
			InputsSucceeded: run.Status.Progress.InputsSucceeded,
			InputsFailed:    run.Status.Progress.InputsFailed,
			BytesUploaded:   run.Status.Progress.BytesUploaded,
		},
		Cancellable: IsIngestRunCancelable(run),
	}
}

func ProjectIngestRunDetail(run *tamossv1alpha1.IngestRun) IngestRunDetail {
	verify := true
	if run.Spec.Options.Verify != nil {
		verify = *run.Spec.Options.Verify
	}
	maxInputs := run.Spec.Options.MaxInputs
	if maxInputs == 0 {
		maxInputs = 1000
	}
	// The storage backend reference is optional; an unset reference projects as
	// an empty string rather than dereferencing a nil pointer.
	storageBackend := ""
	if run.Spec.Options.StorageBackendRef != nil {
		storageBackend = run.Spec.Options.StorageBackendRef.Name
	}
	detail := IngestRunDetail{
		IngestRunSummary:   ProjectIngestRunSummary(run),
		Generation:         run.Generation,
		ObservedGeneration: run.Status.ObservedGeneration,
		InputKind:          string(run.Spec.Input.Kind),
		Options: IngestRunOptions{
			StorageBackend:   storageBackend,
			Verify:           verify,
			DryRun:           run.Spec.Options.DryRun,
			MaxInputs:        maxInputs,
			Concurrency:      run.Spec.Options.Concurrency,
			TAMSFlowProfiles: projectTAMSFlowProfiles(run.Spec.Options.TAMSFlowProfiles, run.Status.ResolvedTAMSFlowProfiles),
		},
		TamsinRunID: boundedProjectedString(run.Status.TamsinRunID, maxProjectedTamsinRunIDLength),
		Conditions:  make([]IngestRunCondition, 0, min(len(run.Status.Conditions), maxProjectedConditions)),
	}
	if run.Spec.Output != nil {
		detail.OutputIntent = &IngestRunOutputIntent{FlowMetadata: IngestRunFlowMetadata{
			Label:       boundedProjectedString(run.Spec.Output.FlowMetadata.Label, maxProjectedOutputLabelLength),
			Description: boundedProjectedString(run.Spec.Output.FlowMetadata.Description, maxProjectedOutputDescription),
			Tags:        projectIngestOutputTags(run.Spec.Output.FlowMetadata.Tags),
		}}
	}
	if run.Status.JobRef.Name != "" {
		detail.Job = &IngestRunObjectReference{Name: run.Status.JobRef.Name, UID: string(run.Status.JobRef.UID)}
	}
	if run.Spec.RetryOf != nil {
		detail.RetryOf = &IngestRunObjectReference{Name: run.Spec.RetryOf.Name, UID: run.Spec.RetryOf.UID}
	}
	result := run.Status.ResultRef
	if result.Key != "" || result.SHA256 != "" || result.Size != 0 || result.MediaType != "" || result.Verified {
		detail.Result = &IngestRunResult{
			Present:   true,
			Size:      result.Size,
			MediaType: boundedProjectedString(result.MediaType, maxProjectedMediaTypeLength),
			Verified:  result.Verified,
		}
	}
	if run.Status.Output != nil && run.Status.Output.RootFlowID != "" && run.Status.Output.SourceID != "" {
		detail.Output = &IngestRunOutput{
			RootFlowID:           run.Status.Output.RootFlowID,
			SourceID:             run.Status.Output.SourceID,
			MemberFlowsTruncated: run.Status.Output.MemberFlowsTruncated,
			MemberFlows:          make([]IngestRunOutputFlow, 0, min(len(run.Status.Output.MemberFlows), maxProjectedOutputMemberFlows)),
		}
		for index := range min(len(run.Status.Output.MemberFlows), maxProjectedOutputMemberFlows) {
			member := run.Status.Output.MemberFlows[index]
			detail.Output.MemberFlows = append(detail.Output.MemberFlows, IngestRunOutputFlow{
				ID:     member.ID,
				Format: boundedProjectedString(member.Format, 128),
				Role:   boundedProjectedString(member.Role, 64),
			})
		}
		if len(run.Status.Output.MemberFlows) > maxProjectedOutputMemberFlows {
			detail.Output.MemberFlowsTruncated = true
		}
	}
	for index := range min(len(run.Status.Conditions), maxProjectedConditions) {
		condition := run.Status.Conditions[index]
		detail.Conditions = append(detail.Conditions, IngestRunCondition{
			Type:               boundedProjectedString(condition.Type, maxProjectedConditionTypeLength),
			Status:             string(condition.Status),
			Reason:             boundedProjectedString(condition.Reason, maxProjectedConditionReason),
			LastTransitionTime: projectTimestamp(&condition.LastTransitionTime),
		})
	}
	return detail
}

func IsIngestRunCancelable(run *tamossv1alpha1.IngestRun) bool {
	if effectiveIngestRunDesiredState(run) == tamossv1alpha1.IngestRunDesiredStateCancelled {
		return false
	}
	switch effectiveIngestRunPhase(run) {
	case tamossv1alpha1.IngestRunPhasePending,
		tamossv1alpha1.IngestRunPhaseQueued,
		tamossv1alpha1.IngestRunPhaseRunning:
		return true
	default:
		return false
	}
}

func effectiveIngestRunPhase(run *tamossv1alpha1.IngestRun) tamossv1alpha1.IngestRunPhase {
	if run.Status.Phase == "" {
		return tamossv1alpha1.IngestRunPhasePending
	}
	return run.Status.Phase
}

func effectiveIngestRunProfile(run *tamossv1alpha1.IngestRun) tamossv1alpha1.IngestRunProfile {
	if run.Spec.Profile == "" {
		return tamossv1alpha1.IngestRunProfileEssenceSegments
	}
	return run.Spec.Profile
}

func projectTAMSFlowProfiles(assignments []tamossv1alpha1.IngestRunTAMSFlowProfile, resolved []tamossv1alpha1.IngestRunResolvedFlowProfileStatus) []IngestRunTAMSFlowProfile {
	resolvedByStream := make(map[string]tamossv1alpha1.IngestRunResolvedFlowProfileStatus, len(resolved))
	for _, item := range resolved {
		resolvedByStream[fmt.Sprintf("%s:%d", item.Format, item.Index)] = item
	}
	result := make([]IngestRunTAMSFlowProfile, 0, len(assignments))
	for _, assignment := range assignments {
		item := IngestRunTAMSFlowProfile{Format: assignment.Format, Index: assignment.Index, ProfileID: assignment.ProfileID}
		if assignment.ProfileRef != nil {
			item.ProfileRef = boundedProjectedString(assignment.ProfileRef.Name, 253)
		}
		if resolvedItem, ok := resolvedByStream[fmt.Sprintf("%s:%d", assignment.Format, assignment.Index)]; ok {
			item.ResolvedProfileID = resolvedItem.ProfileID
		} else if assignment.ProfileID != "" {
			item.ResolvedProfileID = assignment.ProfileID
		}
		result = append(result, item)
	}
	return result
}

func projectIngestOutputTags(tags map[string]apiextensionsv1.JSON) map[string]any {
	keys := slices.Sorted(maps.Keys(tags))
	result := make(map[string]any, min(len(keys), maxProjectedOutputTags))
	for _, key := range keys[:min(len(keys), maxProjectedOutputTags)] {
		var value any
		if err := json.Unmarshal(tags[key].Raw, &value); err != nil {
			continue
		}
		name := boundedProjectedString(key, maxProjectedOutputTagName)
		switch typed := value.(type) {
		case string:
			result[name] = boundedProjectedString(typed, maxProjectedOutputTagValue)
		case []any:
			values := make([]string, 0, len(typed))
			for _, item := range typed {
				text, ok := item.(string)
				if !ok {
					values = nil
					break
				}
				values = append(values, boundedProjectedString(text, maxProjectedOutputTagValue))
			}
			if values != nil {
				result[name] = values
			}
		}
	}
	return result
}

func effectiveIngestRunSizeClass(run *tamossv1alpha1.IngestRun) tamossv1alpha1.IngestRunSizeClass {
	if run.Spec.SizeClass == "" {
		return tamossv1alpha1.IngestRunSizeClassStandard
	}
	return run.Spec.SizeClass
}

func effectiveIngestRunDesiredState(run *tamossv1alpha1.IngestRun) tamossv1alpha1.IngestRunDesiredState {
	if run.Spec.DesiredState == "" {
		return tamossv1alpha1.IngestRunDesiredStateRunning
	}
	return run.Spec.DesiredState
}

func projectTimestamp(value *metav1.Time) string {
	if value == nil || value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339Nano)
}

func boundedProjectedString(value string, maxRunes int) string {
	var result strings.Builder
	count := 0
	for _, character := range value {
		if unicode.IsControl(character) {
			continue
		}
		if count == maxRunes {
			break
		}
		result.WriteRune(character)
		count++
	}
	return result.String()
}
