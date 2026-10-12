#!/usr/bin/env bash
# Installs the pinned helmfile into ./bin and the pinned helm-diff plugin into
# helm. Called by `make tools`.
#
# helmfile is a release binary checked against the sha256 below, not a Go tool:
# its module only builds with its own replace directives, and v1.8 needs a newer
# Go than this repo pins. Bump HELMFILE_VERSION and the four sums together, from
# https://github.com/helmfile/helmfile/releases/download/v<version>/helmfile_<version>_checksums.txt
set -euo pipefail

HELMFILE_VERSION="1.8.1"
HELM_DIFF_VERSION="3.15.15"
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BIN="${BIN:-$ROOT/bin}"

sha256_for() {
	case $1 in
	darwin_amd64) echo b7a44505f1cf09e8e42db4e5914d46bd6f3abae95d226d16ea182d56954eb3bd ;;
	darwin_arm64) echo 62a6b086bdd7a934ff4a36d4974446d7271566b88db200b3956053a9b388fed8 ;;
	linux_amd64) echo ccabbe75dddbc25008d055fe92a44f95a184342211e7244d32b870409239c7a3 ;;
	linux_arm64) echo 2217788de9fb42cd0b3d84f1ee99c63617e19dea7af4995a59e45e4952c86ec4 ;;
	*) echo "install-helmfile: no pinned sum for $1" >&2; exit 2 ;;
	esac
}

platform() {
	local os arch
	os=$(uname -s | tr '[:upper:]' '[:lower:]')
	case $(uname -m) in
	x86_64 | amd64) arch=amd64 ;;
	arm64 | aarch64) arch=arm64 ;;
	*) echo "install-helmfile: unsupported machine $(uname -m)" >&2; exit 2 ;;
	esac
	echo "${os}_${arch}"
}

install_helmfile() {
	local plat tarball url tmp want got
	if [ -x "$BIN/helmfile" ] && "$BIN/helmfile" --version 2>/dev/null | grep -q "$HELMFILE_VERSION"; then
		echo "helmfile $HELMFILE_VERSION already in $BIN"
		return
	fi
	plat=$(platform)
	tarball="helmfile_${HELMFILE_VERSION}_${plat}.tar.gz"
	url="https://github.com/helmfile/helmfile/releases/download/v${HELMFILE_VERSION}/${tarball}"
	tmp=$(mktemp -d)
	trap 'rm -rf "$tmp"' RETURN
	curl -fsSL "$url" -o "$tmp/$tarball"
	want=$(sha256_for "$plat")
	got=$(shasum -a 256 "$tmp/$tarball" | cut -d' ' -f1)
	[ "$got" = "$want" ] || { echo "install-helmfile: sha256 mismatch for $tarball" >&2; exit 1; }
	tar -xzf "$tmp/$tarball" -C "$tmp" helmfile
	mkdir -p "$BIN"
	mv "$tmp/helmfile" "$BIN/helmfile"
	echo "installed helmfile $HELMFILE_VERSION into $BIN"
}

install_helm_diff() {
	if helm plugin list 2>/dev/null | awk '$1 == "diff" { print $2 }' | grep -qx "$HELM_DIFF_VERSION"; then
		echo "helm-diff $HELM_DIFF_VERSION already installed"
		return
	fi
	helm plugin uninstall diff >/dev/null 2>&1 || true
	# Helm 4 verifies plugin signatures by default; helm-diff publishes none, so
	# the pinned tag is what we trust. Helm 3 (CI runners) has no --verify flag.
	local verify=""
	if [ "$(helm_major)" -ge 4 ]; then
		verify="--verify=false"
	fi
	# shellcheck disable=SC2086 # $verify is empty or a single flag
	helm plugin install https://github.com/databus23/helm-diff --version "v$HELM_DIFF_VERSION" $verify
}

helm_major() {
	helm version --template '{{.Version}}' | sed -E 's/^v([0-9]+).*/\1/'
}

install_helmfile
install_helm_diff
