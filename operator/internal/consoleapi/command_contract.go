package consoleapi

const (
	SessionPath                = APIBasePath + "/session"
	IngestRunCancelPathPattern = "POST " + APIBasePath + "/ingest-runs/{name}/cancel"
)

type Capability struct {
	Available bool   `json:"available"`
	Allowed   bool   `json:"allowed"`
	Reason    string `json:"reason,omitempty"`
}

type SessionIdentity struct {
	Subject  string `json:"subject"`
	Username string `json:"username,omitempty"`
	Method   string `json:"method"`
	Roles    []Role `json:"roles"`
}

type IngestRunCapabilities struct {
	Read   Capability `json:"read"`
	Create Capability `json:"create"`
	Cancel Capability `json:"cancel"`
	Retry  Capability `json:"retry"`
}

type SessionCapabilities struct {
	IngestRuns IngestRunCapabilities `json:"ingestRuns"`
}

type SessionResponse struct {
	SchemaVersion string              `json:"schemaVersion"`
	Identity      SessionIdentity     `json:"identity"`
	Capabilities  SessionCapabilities `json:"capabilities"`
}

type CancelIngestRunRequest struct {
	UID      string `json:"uid"`
	Revision string `json:"revision"`
}

type CancelIngestRunResponse struct {
	Run      IngestRunDetail `json:"run"`
	Replayed bool            `json:"replayed"`
}

func sessionResponse(identity Identity, cancellationAvailable bool) SessionResponse {
	return SessionResponse{
		SchemaVersion: "1.0",
		Identity: SessionIdentity{
			Subject:  identity.Subject,
			Username: identity.Username,
			Method:   identity.Method,
			Roles:    append([]Role(nil), identity.Roles...),
		},
		Capabilities: SessionCapabilities{IngestRuns: IngestRunCapabilities{
			Read: Capability{
				Available: true,
				Allowed:   identity.CanView(),
			},
			Create: Capability{
				Available: false,
				Allowed:   identity.HasRole(RoleIngestRunner),
				Reason:    "ingest_creation_unavailable",
			},
			Cancel: Capability{
				Available: cancellationAvailable,
				Allowed:   identity.HasRole(RoleOperator),
			},
			Retry: Capability{
				Available: false,
				Allowed:   identity.HasRole(RoleOperator),
				Reason:    "ingest_retry_unavailable",
			},
		}},
	}
}
