#!/usr/bin/env bash
# Run the go command at the version go.mod asks for, on a network where only
# GitHub is reachable (go.dev, proxy.golang.org, golang.org, gopkg.in and
# go.yaml.in do not resolve).
#
#   scripts/go-restricted.sh test ./...
#   make test GO=scripts/go-restricted.sh
#
# What it does on every run:
#   - downloads Go from the actions/go-versions releases on GitHub into
#     .tools/go-<version>/ (once) and puts its bin directory first on PATH;
#   - copies go.mod and go.sum to .tools/go.restricted.{mod,sum}, adding a
#     replace for every module hosted outside github.com that points at its
#     GitHub mirror. go.mod and go.sum themselves are never touched;
#   - exports GOTOOLCHAIN, GOPROXY, GOSUMDB, GOFLAGS, GOMODCACHE, GOCACHE and
#     GOPATH so child processes (tests that run go) use the same setup;
#   - execs go in the caller's current directory.
#
# Limits: go run pkg@version does not work with the -modfile flag, so make
# lint, vuln and snapshot are not supported. The mirror modules have no hashes
# in go.sum and GOSUMDB is off, so nothing checks the downloaded source: use
# this for local builds and tests only.
#
# All state is under .tools/ at the repo root (git-ignored). The module cache
# is made of read-only files: run `chmod -R u+w .tools` before `rm -rf .tools`,
# or clean it with `scripts/go-restricted.sh clean -modcache`.
set -euo pipefail

script_dir=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
root=$(cd "$script_dir/.." && pwd)
tools="$root/.tools"
manifest_url=https://raw.githubusercontent.com/actions/go-versions/main/versions-manifest.json

# GitHub mirror for a module path hosted elsewhere. Add a case for a new one.
mirror_for() {
  case "$1" in
    golang.org/x/*) echo "github.com/golang/${1#golang.org/x/}" ;;
    go.yaml.in/yaml/v3) echo "github.com/yaml/go-yaml/v3" ;;
    gopkg.in/check.v1) echo "github.com/go-check/check" ;;
    *) return 1 ;;
  esac
}

mkdir -p "$tools"

# --- Go version -------------------------------------------------------------
toolchain=$(sed -n 's/^toolchain go\([0-9][0-9.]*\)[[:space:]]*$/\1/p' "$root/go.mod" | head -1)
if [ -n "$toolchain" ]; then
  version=$toolchain
  manifest=
else
  want=$(sed -n 's/^go \([0-9][0-9.]*\)[[:space:]]*$/\1/p' "$root/go.mod" | head -1)
  [ -n "$want" ] || { echo "go-restricted: no go or toolchain line in go.mod" >&2; exit 1; }
  minor=$(echo "$want" | sed -n 's/^\([0-9][0-9]*\.[0-9][0-9]*\).*/\1/p')
  min_patch=$(echo "$want" | sed -n 's/^[0-9][0-9]*\.[0-9][0-9]*\.\([0-9][0-9]*\)$/\1/p')
  version=
  manifest=$(curl -fsSL "$manifest_url") || {
    echo "go-restricted: cannot download $manifest_url" >&2
    exit 1
  }
  # Newest first; the first stable X.Y.N entry is the highest patch.
  version=$(printf '%s\n' "$manifest" |
    grep -A1 -E "\"version\": \"${minor//./\\.}\.[0-9]+\"" |
    grep -B1 '"stable": true' |
    sed -n 's/.*"version": "\([^"]*\)".*/\1/p' | head -1)
  [ -n "$version" ] || { echo "go-restricted: no stable Go $minor release in the manifest" >&2; exit 1; }
  if [ -n "$min_patch" ] && [ "${version##*.}" -lt "$min_patch" ]; then
    echo "go-restricted: go.mod needs Go $want but the newest stable $minor release is $version" >&2
    exit 1
  fi
fi

goroot="$tools/go-$version"
if [ ! -x "$goroot/bin/go" ]; then
  os=$(uname -s)
  case "$os/$(uname -m)" in
    Linux/x86_64) platform=linux arch=x64 ;;
    Linux/aarch64 | Linux/arm64) platform=linux arch=arm64 ;;
    *) echo "go-restricted: unsupported OS/arch $os/$(uname -m); only Linux x86_64 and arm64 are supported" >&2; exit 1 ;;
  esac
  [ -n "$manifest" ] || manifest=$(curl -fsSL "$manifest_url") || {
    echo "go-restricted: cannot download $manifest_url" >&2
    exit 1
  }
  url=$(printf '%s\n' "$manifest" |
    grep -A3 "\"filename\": \"go-$version-$platform-$arch.tar.gz\"" |
    sed -n 's/.*"download_url": "\([^"]*\)".*/\1/p' | head -1)
  [ -n "$url" ] || { echo "go-restricted: Go $version for $platform/$arch is not in the manifest" >&2; exit 1; }
  echo "go-restricted: downloading Go $version" >&2
  tmp="$tools/go-$version.partial"
  rm -rf "$tmp"
  mkdir -p "$tmp"
  curl -fsSL "$url" | tar -xz -C "$tmp"
  [ -x "$tmp/bin/go" ] || { echo "go-restricted: unexpected archive layout in $url" >&2; rm -rf "$tmp"; exit 1; }
  mv "$tmp" "$goroot"
fi

# --- module copy with GitHub mirrors ----------------------------------------
modfile="$tools/go.restricted.mod"
cp "$root/go.mod" "$modfile"
cp "$root/go.sum" "$tools/go.restricted.sum"

modules=$({ sed -n 's/^[[:space:]]*\(require[[:space:]]*\)\{0,1\}\([^[:space:]/]*\.[^[:space:]]*\)[[:space:]][[:space:]]*v[0-9].*/\2/p' "$root/go.mod"
  sed -n 's/^\([^[:space:]]*\)[[:space:]].*/\1/p' "$root/go.sum"; } |
  grep -v '^github\.com/' | sort -u)
for mod in $modules; do
  case "$mod" in *.*) ;; *) continue ;; esac
  mirror=$(mirror_for "$mod") || {
    echo "no GitHub mirror known for $mod; add one to scripts/go-restricted.sh" >&2
    exit 1
  }
  ver=$(sed -n "s|^$mod \(v[^ /]*\)[ /].*|\1|p" "$root/go.sum" | sort -V | tail -1)
  [ -n "$ver" ] || ver=$(sed -n "s|^[[:space:]]*\(require \)\{0,1\}$mod \(v[^ ]*\).*|\2|p" "$root/go.mod" | head -1)
  printf '\nreplace %s => %s %s\n' "$mod" "$mirror" "$ver" >>"$modfile"
done

# --- environment ------------------------------------------------------------
export PATH="$goroot/bin:$PATH"
export GOTOOLCHAIN=local
export GOPROXY=direct
export GOSUMDB=off
export GOFLAGS="-modfile=$modfile -mod=mod${GOFLAGS:+ $GOFLAGS}"
export GOMODCACHE="$tools/gomod"
export GOCACHE="$tools/gocache"
export GOPATH="$tools/gopath"

exec "$goroot/bin/go" "$@"
