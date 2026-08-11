# VERSION defines the project version for the bundle.
# Update this value when you upgrade the version of your project.
# To re-generate a bundle for another specific version without changing the standard setup, you can:
# - use the VERSION as arg of the bundle target (e.g make bundle VERSION=0.0.2)
# - use environment variables to overwrite this value (e.g export VERSION=0.0.2)
VERSION ?= 0.0.1

# Image URL to use all building/pushing image targets
IMG ?= livewyer/tamoss-operator:v$(VERSION)
# ENVTEST_K8S_VERSION refers to the version of kubebuilder assets to be downloaded by envtest binary.
# Keep this on the same Kubernetes minor as the Kind node image used by the
# chainsaw suite (kind v0.32.0 defaults to kindest/node v1.36.1).
ENVTEST_K8S_VERSION = 1.36.x
SCHEMA_VERSION ?= $(VERSION)
PREVIOUS_SCHEMA_VERSION ?=
TAMS_API_VERSION ?= 8.2
# OPERAND_VERSION sets the operand image tag the operator defaults to when a
# Tamoss spec omits one. Release builds pass the release version; development
# builds keep the "dev" fallback.
OPERAND_VERSION ?= dev
MAX_CRD_BYTES ?= 1350000
GO_LDFLAGS ?= -X github.com/livewyer-ops/tamoss/operator/internal/schema.SchemaVersion=$(SCHEMA_VERSION) -X github.com/livewyer-ops/tamoss/operator/internal/schema.PreviousSupportedSchemaVersion=$(PREVIOUS_SCHEMA_VERSION) -X github.com/livewyer-ops/tamoss/operator/internal/schema.SupportedTAMSAPIVersion=$(TAMS_API_VERSION) -X github.com/livewyer-ops/tamoss/operator/internal/controller/defaults.DefaultOperandTag=$(OPERAND_VERSION)

# Get the currently used golang install path (in GOPATH/bin, unless GOBIN is set)
ifeq (,$(shell go env GOBIN))
GOBIN=$(shell go env GOPATH)/bin
else
GOBIN=$(shell go env GOBIN)
endif

# Container tool used by image build targets.
CONTAINER_TOOL ?= docker

# Setting SHELL to bash allows bash commands to be executed by recipes.
# Options are set to exit when a recipe line exits non-zero or a piped command fails.
SHELL = /usr/bin/env bash -o pipefail
.SHELLFLAGS = -ec

.PHONY: all
all: build

##@ General

# The help target prints out all targets with their descriptions organized
# beneath their categories. The categories are represented by '##@' and the
# target descriptions by '##'. The awk command is responsible for reading the
# entire set of makefiles included in this invocation, looking for lines of the
# file as xyz: ## something, and then pretty-format the target and help. Then,
# if there's a line with ##@ something, that gets pretty-printed as a category.
# More info on the usage of ANSI control characters for terminal formatting:
# https://en.wikipedia.org/wiki/ANSI_escape_code#SGR_parameters
# More info on the awk command:
# http://linuxcommand.org/lc3_adv_awk.php

.PHONY: help
help: ## Display this help.
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

##@ Development

.PHONY: manifests
manifests: controller-gen ## Generate WebhookConfiguration, ClusterRole and CustomResourceDefinition objects.
	$(CONTROLLER_GEN) rbac:roleName=manager-role crd webhook paths="./..." output:crd:artifacts:config=config/crd/bases

.PHONY: crd-size-budget
crd-size-budget: manifests ## Fail before generated CRDs approach the API object-size ceiling.
	@for file in config/crd/bases/*.yaml; do \
		bytes="$$(wc -c < "$$file")"; \
		if (( bytes > $(MAX_CRD_BYTES) )); then \
			echo "$$file is $$bytes bytes; limit is $(MAX_CRD_BYTES)" >&2; \
			exit 1; \
		fi; \
	done

.PHONY: generate
generate: controller-gen ## Generate code containing DeepCopy, DeepCopyInto, and DeepCopyObject method implementations.
	$(CONTROLLER_GEN) object:headerFile="hack/boilerplate.go.txt" paths="./..."

.PHONY: fmt
fmt: ## Run go fmt against code.
	go fmt ./...

.PHONY: vet
vet: ## Run go vet against code.
	go vet ./...

# Resolve envtest assets and run the Go test suite; shared by test and test-fast.
define run-envtest-suite
assets="$$($(ENVTEST) use $(ENVTEST_K8S_VERSION) --bin-dir $(LOCALBIN) -p path)"; \
case "$$assets" in \
	/*) kubebuilder_assets="$$assets" ;; \
	*) kubebuilder_assets="$$(pwd)/$$assets" ;; \
esac; \
KUBEBUILDER_ASSETS="$$kubebuilder_assets" go test $$(go list ./... | grep -v /e2e) -coverprofile cover.out
endef

.PHONY: test
test: crd-size-budget generate fmt vet envtest ## Run tests (full gate: codegen, fmt, vet, envtest suite).
	$(run-envtest-suite)

.PHONY: test-fast
test-fast: envtest ## Run the envtest suite only (no codegen/fmt/vet); inner loop helper, run `make test` before pushing.
	$(run-envtest-suite)

.PHONY: lint
lint: golangci-lint deadcode ## Run Go linters and dead-code checks.
	$(GOLANGCI_LINT) run

.PHONY: lint-fix
lint-fix: golangci-lint ## Run golangci-lint linter and perform fixes
	$(GOLANGCI_LINT) run --fix

##@ Build

.PHONY: build
build: manifests generate fmt vet ## Build manager binary.
	go build -ldflags "$(GO_LDFLAGS)" -o bin/manager ./cmd

.PHONY: run
run: manifests generate fmt vet ## Run a controller from your host.
	go run -ldflags "$(GO_LDFLAGS)" ./cmd

.PHONY: install-manifest
install-manifest: manifests kustomize ## Render the public operator install manifest to ../deploy/operator/install.yaml.
	mkdir -p ../deploy/operator
	$(KUSTOMIZE) build config/default > ../deploy/operator/install.yaml

.PHONY: docker-build
docker-build: ## Build docker image with the manager.
	$(CONTAINER_TOOL) build \
		--build-arg VERSION=$(VERSION) \
		--build-arg SCHEMA_VERSION=$(SCHEMA_VERSION) \
		--build-arg PREVIOUS_SCHEMA_VERSION=$(PREVIOUS_SCHEMA_VERSION) \
		--build-arg TAMS_API_VERSION=$(TAMS_API_VERSION) \
		--build-arg OPERAND_VERSION=$(OPERAND_VERSION) \
		-t ${IMG} -f Dockerfile ..

##@ Dependencies

## Location to install dependencies to
LOCALBIN ?= $(shell pwd)/bin
$(LOCALBIN):
	mkdir -p $(LOCALBIN)

## Tool Binaries
KUBECTL ?= kubectl
KUSTOMIZE ?= $(LOCALBIN)/kustomize-$(KUSTOMIZE_VERSION)
CONTROLLER_GEN ?= $(LOCALBIN)/controller-gen-$(CONTROLLER_TOOLS_VERSION)
ENVTEST ?= $(LOCALBIN)/setup-envtest-$(ENVTEST_VERSION)
GOLANGCI_LINT = $(LOCALBIN)/golangci-lint-$(GOLANGCI_LINT_VERSION)
DEADCODE ?= $(LOCALBIN)/deadcode-$(GO_TOOLS_VERSION)

## Tool Versions
KUSTOMIZE_VERSION ?= v5.8.1
CONTROLLER_TOOLS_VERSION ?= v0.21.0
ENVTEST_VERSION ?= release-0.24
GOLANGCI_LINT_VERSION ?= v2.12.2
GO_TOOLS_VERSION ?= v0.44.0

.PHONY: kustomize
kustomize: $(KUSTOMIZE) ## Download kustomize locally if necessary.
$(KUSTOMIZE): $(LOCALBIN)
	$(call go-install-tool,$(KUSTOMIZE),sigs.k8s.io/kustomize/kustomize/v5,$(KUSTOMIZE_VERSION))

.PHONY: controller-gen
controller-gen: $(CONTROLLER_GEN) ## Download controller-gen locally if necessary.
$(CONTROLLER_GEN): $(LOCALBIN)
	$(call go-install-tool,$(CONTROLLER_GEN),sigs.k8s.io/controller-tools/cmd/controller-gen,$(CONTROLLER_TOOLS_VERSION))

.PHONY: envtest
envtest: $(ENVTEST) ## Download setup-envtest locally if necessary.
$(ENVTEST): $(LOCALBIN)
	$(call go-install-tool,$(ENVTEST),sigs.k8s.io/controller-runtime/tools/setup-envtest,$(ENVTEST_VERSION))

.PHONY: golangci-lint
golangci-lint: $(GOLANGCI_LINT) ## Download golangci-lint locally if necessary.
# Build golangci-lint with the module toolchain so it can analyse the Go
# language version targeted by go.mod (its own go.mod pins an older toolchain).
$(GOLANGCI_LINT): export GOTOOLCHAIN = $(shell awk '/^toolchain /{print $$2}' go.mod)
$(GOLANGCI_LINT): $(LOCALBIN)
	$(call go-install-tool,$(GOLANGCI_LINT),github.com/golangci/golangci-lint/v2/cmd/golangci-lint,${GOLANGCI_LINT_VERSION})

.PHONY: deadcode
deadcode: $(DEADCODE) ## Run Go dead-code analysis.
	$(DEADCODE) ./...

$(DEADCODE): export GOTOOLCHAIN = $(shell awk '/^toolchain /{print $$2}' go.mod)
$(DEADCODE): $(LOCALBIN)
	$(call go-install-tool,$(DEADCODE),golang.org/x/tools/cmd/deadcode,$(GO_TOOLS_VERSION))

# go-install-tool will 'go install' any package with custom target and name of binary, if it doesn't exist
# $1 - target path with name of binary (ideally with version)
# $2 - package url which can be installed
# $3 - specific version of package
define go-install-tool
@[ -f $(1) ] || { \
set -e; \
package=$(2)@$(3) ;\
echo "Downloading $${package}" ;\
GOBIN=$(LOCALBIN) go install $${package} ;\
mv "$$(echo "$(1)" | sed "s/-$(3)$$//")" $(1) ;\
}
endef
