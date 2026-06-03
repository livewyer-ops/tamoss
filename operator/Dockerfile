# Build the manager binary. This Dockerfile is intended to be built from the
# repository root with `docker build -f operator/Dockerfile .`.
FROM --platform=$BUILDPLATFORM golang:1.23 AS builder
ARG TARGETOS
ARG TARGETARCH
ARG VERSION=0.0.1
ARG SCHEMA_VERSION
ARG PREVIOUS_SCHEMA_VERSION
ARG TAMS_API_VERSION=8.1

WORKDIR /workspace/operator

# Copy module manifests first so dependency downloads stay cacheable.
COPY operator/go.mod operator/go.sum ./
RUN go mod download

# Copy source.
COPY operator/*.go ./
COPY operator/cmd/ cmd/
COPY operator/api/ api/
COPY operator/internal/ internal/

# Build. TARGETOS and TARGETARCH are set by buildx for multi-arch builds and may
# be empty for a plain local build, in which case Go uses the builder image
# architecture.
RUN schema_version="${SCHEMA_VERSION:-$VERSION}" && \
    previous_schema_version="${PREVIOUS_SCHEMA_VERSION:-}" && \
    CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} go build -trimpath \
    -ldflags "-X github.com/livewyer-ops/tamoss/operator/internal/schema.SchemaVersion=${schema_version} -X github.com/livewyer-ops/tamoss/operator/internal/schema.PreviousSupportedSchemaVersion=${previous_schema_version} -X github.com/livewyer-ops/tamoss/operator/internal/schema.SupportedTAMSAPIVersion=${TAMS_API_VERSION}" \
    -o /workspace/manager ./cmd

# Use distroless as minimal base image to package the manager binary
# Refer to https://github.com/GoogleContainerTools/distroless for more details
FROM gcr.io/distroless/static:nonroot
WORKDIR /
COPY --from=builder /workspace/manager .
USER 65532:65532

ENTRYPOINT ["/manager"]
