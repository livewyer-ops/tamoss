package defaults

// DefaultOperandTag is the image tag applied to operand images (API, UI,
// Console, and the schema migration runtime) when the spec does not pin one. Release
// builds set it through ldflags so operands default to the operator release
// tag; development builds fall back to "dev".
var DefaultOperandTag = "dev"

const (
	DefaultAPIRepository       = "livewyer/tamoss-api"
	DefaultUIRepository        = "livewyer/tamoss-ui"
	DefaultConsoleRepository   = "livewyer/tamoss-console-api"
	DefaultPostgresClientImage = "postgres:18-alpine"
	DefaultCNPGPostgresVersion = "18"
	DefaultRustFSImage         = "rustfs/rustfs:1.0.0-beta.3"
	DefaultTAMSinImage         = "ghcr.io/livewyer-ops/tamsin:8.2.0-in1@sha256:f69ad71d29c2618f6d9e9f7c366c5f72b0cb4034fbbce45fe8a1c87325a92f72"
)
