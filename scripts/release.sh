#!/bin/sh
# Build a local archive only. Never installs, pushes tags, or publishes a release.
set -eu
cd "$(dirname "$0")/.."
version=${1:-snapshot}
os=${2:-$(go env GOHOSTOS)}
arch=${3:-$(go env GOHOSTARCH)}
case "$version" in ''|*[!A-Za-z0-9._+-]*) echo 'Invalid release version' >&2; exit 1;; esac
case "$version" in dev|snapshot) ;; *)
  git diff --quiet HEAD || { echo 'Versioned archives require a clean tracked tree; use snapshot for local checks' >&2; exit 1; }
  ;;
esac
export GOOS="$os"
export GOARCH="$arch"
export CGO_ENABLED=0
case "$os/$arch" in
  linux/amd64|linux/arm64) ;;
  linux/armv7) export GOARCH=arm GOARM=7 ;;
  darwin/amd64|darwin/arm64)
    if [ "$(go env GOHOSTOS)" != darwin ]; then
      echo 'macOS archives require a macOS host/SDK for cgo USB enumeration' >&2
      exit 1
    fi
    export CGO_ENABLED=1
    ;;
  *) echo "Unsupported release target: $os/$arch" >&2; exit 1;;
esac
work=$(mktemp -d)
trap 'rm -rf "$work"' 0
trap 'exit 1' HUP INT TERM
name="lucio_${version}_${os}_${arch}"
pkg="$work/$name"
mkdir -p "$pkg" dist
flags="-s -w -X github.com/pablasso/lucio/internal/cli.Version=$version"
go build -trimpath -buildvcs=false -ldflags "$flags" -o "$pkg/lucio" ./cmd/lucio
go build -trimpath -buildvcs=false -ldflags '-s -w' -o "$pkg/paperboy" ./cmd/paperboy
python3 scripts/package.py "$pkg"
# Relative arguments avoid publishing local build filesystem paths.
(cd "$pkg" && go version -m ./lucio ./paperboy > BUILDINFO.txt)
printf '\nsource_commit=%s\n' "$(git rev-parse HEAD)" >> "$pkg/BUILDINFO.txt"
if ! git diff --quiet HEAD; then printf 'source_state=modified\n' >> "$pkg/BUILDINFO.txt"; fi
python3 scripts/archive.py "$pkg" "dist/$name.tar.gz"
printf 'Built dist/%s.tar.gz and checksum\n' "$name"
