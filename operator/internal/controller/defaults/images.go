package defaults

// DefaultOperandTag is the image tag applied to operand images (API, UI, and
// the schema migration runtime) when the spec does not pin one. Release
// builds set it through ldflags so operands default to the operator release
// tag; development builds fall back to "dev".
var DefaultOperandTag = "dev"

const (
	DefaultAPIRepository       = "livewyer/tamoss-api"
	DefaultUIRepository        = "livewyer/tamoss-ui"
	DefaultPostgresClientImage = "postgres:18-alpine"
	DefaultCNPGPostgresVersion = "18"
	DefaultRustFSImage         = "rustfs/rustfs:1.0.0-beta.3"
	DefaultTAMSinImage         = "ghcr.io/livewyer-ops/tamsin:1.0.0-rc.3@sha256:f764e26601561b8652d80ff34c16ff50598a09fb8735204c65bbf26b62a6e709"
)
