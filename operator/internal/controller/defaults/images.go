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
)
