#!/usr/bin/env bash
set -euo pipefail

tool="${1:?usage: install-aqua-tool.sh <tool> <version>}"
version="${2:?usage: install-aqua-tool.sh <tool> <version>}"
tmp="$(mktemp -d)"

cleanup() {
  rm -rf "$tmp"
}
trap cleanup EXIT

case "$tool" in
  chainsaw)
    curl -fsSL -o "$tmp/chainsaw.tar.gz" \
      "https://github.com/kyverno/chainsaw/releases/download/v${version}/chainsaw_linux_amd64.tar.gz"
    tar -xzf "$tmp/chainsaw.tar.gz" -C "$tmp"
    sudo install "$tmp/chainsaw" /usr/local/bin/chainsaw
    chainsaw version
    ;;
  helm)
    curl -fsSL -o "$tmp/helm.tar.gz" \
      "https://get.helm.sh/helm-v${version}-linux-amd64.tar.gz"
    tar -xzf "$tmp/helm.tar.gz" -C "$tmp"
    sudo install "$tmp/linux-amd64/helm" /usr/local/bin/helm
    helm version --short
    ;;
  helmfile)
    curl -fsSL -o "$tmp/helmfile.tar.gz" \
      "https://github.com/helmfile/helmfile/releases/download/v${version}/helmfile_${version}_linux_amd64.tar.gz"
    tar -xzf "$tmp/helmfile.tar.gz" -C "$tmp"
    sudo install "$tmp/helmfile" /usr/local/bin/helmfile
    helmfile version
    ;;
  kube-linter)
    curl -fsSL -o "$tmp/kube-linter.tar.gz" \
      "https://github.com/stackrox/kube-linter/releases/download/v${version}/kube-linter-linux.tar.gz"
    tar -xzf "$tmp/kube-linter.tar.gz" -C "$tmp"
    sudo install "$tmp/kube-linter" /usr/local/bin/kube-linter
    kube-linter version
    ;;
  ripgrep)
    curl -fsSL -o "$tmp/ripgrep.tar.gz" \
      "https://github.com/BurntSushi/ripgrep/releases/download/${version}/ripgrep-${version}-x86_64-unknown-linux-musl.tar.gz"
    tar -xzf "$tmp/ripgrep.tar.gz" -C "$tmp"
    sudo install "$tmp/ripgrep-${version}-x86_64-unknown-linux-musl/rg" /usr/local/bin/rg
    rg --version | head -n 1
    ;;
  yq)
    curl -fsSL -o "$tmp/yq" \
      "https://github.com/mikefarah/yq/releases/download/v${version}/yq_linux_amd64"
    sudo install "$tmp/yq" /usr/local/bin/yq
    yq --version
    ;;
  *)
    echo "unsupported tool: $tool" >&2
    exit 2
    ;;
esac
