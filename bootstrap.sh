#!/usr/bin/env bash
#
# gh-setup-go bootstrap.
#
# The user specifies an exact Go version (e.g. 1.27.0). This script:
#   1. Reuses a Go already on PATH if it is exactly that version.
#   2. Otherwise curls the official go.dev distribution into the runner tool
#      cache (no GitHub API, no manifest, no semver).
#   3. Adds the installed Go's bin to PATH (GITHUB_PATH + this process).
#   4. Builds the thin Go action with that Go and exposes `gh-setup-go` on PATH.
#   5. Executes it.
#
set -euo pipefail

ACTION_DIR="${GITHUB_ACTION_PATH:-$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)}"
GO_VERSION="$(echo "${SETUP_GO_GO_VERSION:-}" | tr -d '[:space:]')"

if [ -z "$GO_VERSION" ]; then
  echo "::error::go-version is required (full version like 1.27.0)" >&2
  exit 1
fi

# --- platform / arch --------------------------------------------------------
case "$(uname -s)" in
  Linux*)               GOOS=linux ;;
  Darwin*)              GOOS=darwin ;;
  MINGW*|MSYS*|CYGWIN*) GOOS=windows ;;
  *)                    GOOS=linux ;;
esac
case "$(uname -m)" in
  x86_64|amd64)  GOARCH=amd64 ;;
  aarch64|arm64) GOARCH=arm64 ;;
  i386|i686)     GOARCH=386 ;;
  armv7l|armv6l) GOARCH=armv6l ;;
  *)             GOARCH=amd64 ;;
esac

TOOL_CACHE="${RUNNER_TOOL_CACHE:-$(mktemp -d)}"
GO_INSTALL_DIR="${TOOL_CACHE}/go/${GO_VERSION}/${GOARCH}"

# --- 1/2. reuse an exact-match Go, or download ------------------------------
# The runner's default `go` can report a newer version (GOTOOLCHAIN auto) than
# the one it actually runs with GOTOOLCHAIN=local, so reuse is only trusted
# after re-verifying with the exact env shipped to later steps.
if command -v go >/dev/null 2>&1; then
  CURRENT="$(go version 2>/dev/null | awk '{print $3}' | sed 's/^go//')"
  if [ "$CURRENT" = "$GO_VERSION" ]; then
    GO_INSTALL_DIR="$(dirname "$(dirname "$(command -v go)")")"
    echo "Found go ${GO_VERSION} already on PATH"
  fi
fi

export PATH="${GO_INSTALL_DIR}/bin:${PATH}"

# Never let the go command auto-download a different toolchain.
export GOTOOLCHAIN=local

# Official Go binaries may resolve GOROOT to a pre-existing /usr/local/go
# (the path they were built with); pin it to the toolchain we are using.
export GOROOT="${GO_INSTALL_DIR}"

# Verify the go we will actually run is the requested version under
# GOTOOLCHAIN=local; if not, install the real distribution.
if [ ! -x "${GO_INSTALL_DIR}/bin/go" ] || ! go version 2>/dev/null | grep -q "go${GO_VERSION} "; then
  GO_INSTALL_DIR="${TOOL_CACHE}/go/${GO_VERSION}/${GOARCH}"
  echo "::group::Downloading Go ${GO_VERSION} for ${GOOS}-${GOARCH}"
  EXT=tar.gz
  [ "$GOOS" = "windows" ] && EXT=zip
  BASE_URL="${SETUP_GO_GO_DOWNLOAD_BASE_URL:-https://go.dev/dl}"
  URL="${BASE_URL%/}/go${GO_VERSION}.${GOOS}-${GOARCH}.${EXT}"
  TMP="$(mktemp -d)"

  echo "Downloading ${URL}"

  curl -fsSL --retry 3 -o "${TMP}/go.${EXT}" "$URL"
  rm -rf "${GO_INSTALL_DIR}"

  mkdir -p "$(dirname "${GO_INSTALL_DIR}")" "${GO_INSTALL_DIR}"

  if [ "$EXT" = "zip" ]; then
    unzip -q "${TMP}/go.${EXT}" -d "${TMP}/unzip"
    cp -R "${TMP}/unzip/go/." "${GO_INSTALL_DIR}/"
  else
    tar -xzf "${TMP}/go.${EXT}" -C "${GO_INSTALL_DIR}" --strip-components=1
  fi
  touch "$(dirname "${GO_INSTALL_DIR}")/${GO_VERSION}.complete"
  rm -rf "${TMP}"
  echo "::endgroup::"

  export PATH="${GO_INSTALL_DIR}/bin:${PATH}"
  export GOROOT="${GO_INSTALL_DIR}"
fi
if [ -n "${GITHUB_PATH:-}" ]; then
  echo "${GO_INSTALL_DIR}/bin" >> "$GITHUB_PATH"
fi

# Resolve the Go module/cache dirs and export env for later steps.
GO_DIRS="$(go env GOMODCACHE GOCACHE)"
GOMODCACHE="$(echo "$GO_DIRS" | sed -n '1p')"
GOCACHE="$(echo "$GO_DIRS" | sed -n '2p')"
export GOMODCACHE GOCACHE
if [ -n "${GITHUB_ENV:-}" ]; then
  echo "GOTOOLCHAIN=local" >> "$GITHUB_ENV"
  echo "GOMODCACHE=$GOMODCACHE" >> "$GITHUB_ENV"
  echo "GOCACHE=$GOCACHE" >> "$GITHUB_ENV"
fi

# Problem matcher. Tell the runner to turn `go build` errors into clickable annotations.
echo "::add-matcher::${ACTION_DIR}/matchers.json"

# --- 3. build & run the thin Go action --------------------------------------
cd "${ACTION_DIR}"

# Pass action inputs to the Go binary as flags. bootstrap reads the action's
# SETUP_GO_* env (wired from inputs in action.yml); the binary itself takes
# flags and hands them to runSetup as func args.
ARGS=(-version "$GO_VERSION")
case "${SETUP_GO_CACHE:-true}" in
  true|1|yes|on) ARGS+=(-cache) ;;
  *)             ARGS+=(-cache=false) ;;
esac

if [ -n "${SETUP_GO_CACHE_DEPENDENCY_PATH:-}" ]; then
  ARGS+=(-cache-dependency-path "$SETUP_GO_CACHE_DEPENDENCY_PATH")
fi

# Build the thin action binary into a stable location and put it on PATH so
# later workflow steps can save the cache with `gh-setup-go -save-cache`.
TOOL_BIN="${TOOL_CACHE}/gh-setup-go/bin"
mkdir -p "${TOOL_BIN}"
go build -trimpath -ldflags="-s -w" -o "${TOOL_BIN}/gh-setup-go" .
if [ -n "${GITHUB_PATH:-}" ]; then
  echo "${TOOL_BIN}" >> "$GITHUB_PATH"
fi
export PATH="${TOOL_BIN}:${PATH}"

exec gh-setup-go "${ARGS[@]}"
