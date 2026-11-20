package defaults

import "github.com/livewyer-ops/tamoss/operator/internal/releases"

// DefaultOperandTag identifies locally built development images.
var DefaultOperandTag = "dev"

const (
	DefaultAPIRepository       = "livewyer/tamoss-api"
	DefaultUIRepository        = "livewyer/tamoss-ui"
	DefaultConsoleRepository   = "livewyer/tamoss-console-api"
	DefaultPostgresClientImage = "postgres:18.6-alpine"
	DefaultCNPGPostgresVersion = "18.6"
	DefaultRustFSImage         = "rustfs/rustfs:1.0.0"
	DefaultTAMSinImage         = "ghcr.io/livewyer-ops/tamsin:8.2.0-in3@sha256:e28e43ce069fc1c241d102228139822bc6423c6d5cca58fafdef88346c51e2e0"
)

var DevelopmentImages = releases.Images{
	API:                 DefaultAPIRepository + ":" + DefaultOperandTag,
	UI:                  DefaultUIRepository + ":" + DefaultOperandTag,
	Console:             DefaultConsoleRepository + ":" + DefaultOperandTag,
	PostgresClient:      DefaultPostgresClientImage,
	CNPGPostgresVersion: DefaultCNPGPostgresVersion,
	RustFS:              DefaultRustFSImage,
	TAMSin:              DefaultTAMSinImage,
}
