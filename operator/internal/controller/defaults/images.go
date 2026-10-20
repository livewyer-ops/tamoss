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
	DefaultTAMSinImage         = "ghcr.io/livewyer-ops/tamsin:8.2.0-in2@sha256:3b573d94fabec8ec7d07ae44e406f8ee72d8cc04e793ab628e6981b09c8b71e3"
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
