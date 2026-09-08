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
	DefaultTAMSinImage         = "ghcr.io/livewyer-ops/tamsin:8.2.0-in2@sha256:3b573d94fabec8ec7d07ae44e406f8ee72d8cc04e793ab628e6981b09c8b71e3"
)
