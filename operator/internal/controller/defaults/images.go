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
	DefaultTAMSinImage         = "ghcr.io/livewyer-ops/tamsin:1.0.0-rc.1@sha256:a3a8a42b87fc8d8643174ed5e6737adfc4234fd1c3efd5a95c85d2a841380d41"
)
