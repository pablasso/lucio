# Builds and releases

## Source builds

Go **1.26.8 or a newer patched release** is required. `go.mod` enforces the
patched baseline; the Go command may download a compatible toolchain. Use
`GOTOOLCHAIN=local` if you instead manage it explicitly.

```sh
make build
./bin/lucio version --json
```

Linux does not require cgo. macOS requires a native macOS SDK/C compiler for USB
enumeration. `lucio usb` remains physically unvalidated even when a build passes.
Python 3.9+ is needed for test/release scripts and the optional USB helper, not
for normal Lucio rendering or serving. Poppler is only needed for Paperboy.

## Local archives (no publication)

From a source checkout with Python 3.9+:

```sh
make test smoke fuzz
sh scripts/release.sh snapshot                    # native target
sh scripts/release.sh snapshot linux arm64        # cross-build, cgo off
python3 scripts/check-release.py dist/*.tar.gz
```

The target matrix is Linux `amd64`, `arm64`, `armv7` and macOS `amd64`, `arm64`.
Both macOS targets can be built with the native SDK on a macOS host; Linux-only
cross builders cannot produce the supported macOS USB implementation. There are
no Windows release targets or universal macOS archives.

Each `dist/lucio_VERSION_OS_ARCH.tar.gz` contains both binaries, README/docs,
original examples, schemas, service units, USB helper, build information, and
all required notices. It contains neither downloaded PDFs nor device logs.
The script copies only **tracked allowlisted release material**, rejects
symlinks/special files, excludes untracked runtime files, and strips host
ownership/extended filesystem metadata. Code is built from
the checkout; `snapshot`/`dev` allow local modifications and mark that fact in
build metadata. Stage newly added release documentation before a snapshot test.
Other version labels require a clean tracked tree.

A companion `.sha256` file verifies the archive:

```sh
cd dist
sha256sum --check lucio_VERSION_linux_arm64.sha256  # Linux
# or: shasum -a 256 -c lucio_VERSION_darwin_arm64.sha256
```

Replace the placeholders with the actual filenames. Extract on the matching
platform, then run `./lucio version` inside the archive directory. Review service
installation instructions before copying binaries into system locations.
Archives contain release material, not a buildable source checkout; obtain the
source repository for `make`/Go development commands.

Go builds use `-trimpath`, omit automatic VCS build metadata, and inject the
Lucio version. `BUILDINFO.txt` records linked dependency/toolchain versions and
the source commit without local filesystem paths. Archives/checksums are not
currently signed or claimed to be reproducible byte-for-byte across hosts.

## GitHub automation

CI runs race tests, vet, formatting/doc checks, offline and simulated-screen
smoke tests on Linux/macOS, optional Poppler coverage, bounded parser fuzzing,
archive checks, and secret/vulnerability scans. Actions are pinned by commit;
audit tool versions are pinned too. Weekly scans and dependency-update PRs help
surface drift, but maintainers must update the patched Go baseline when needed.

After publication of the repository, pushing a `v*` tag runs the release workflow.
It tests on both OSes, builds five target archives, verifies them/checksums, and
creates an **unpublished draft** with SHA256SUMS. It does not install software,
reconfigure screens, or publish the draft automatically. Failed jobs block the
draft. Re-running a completed workflow does not silently overwrite a release.

Before publishing a draft:

1. Review tests, vulnerability/secret scans, dependency/font licenses, and the
   repository's complete history. Inspect the archives and their build info.
2. Run the [physical acceptance checklist](operations.md#acceptance-and-rollback)
   on the declared panel/firmware. Software tests do not establish new hardware
   support or validate the Go USB implementation.
3. State the tested platforms/firmware and any known limitations in release
   notes. Keep firmware-egress and factory-provisioning claims conservative.
4. Publish deliberately. Retain a previous known-good binary/configuration for
   rollback; a tag or archive build alone is not a live cutover.
