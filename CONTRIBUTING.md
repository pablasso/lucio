# Contributing

Keep Lucio small: one screen per daemon, offline rendering, private local IPC,
and explicit updates. Read [architecture](docs/architecture.md) before moving
responsibilities across those boundaries. Scene JSON is the authoring contract;
internal Go packages and HTTP routes are not a public SDK/API.

## Build and test

Use Go 1.26.8 or a newer patched release. macOS also needs Xcode command-line
tools for cgo USB enumeration. From a clean checkout:

```sh
make build
make test                 # race tests, vet, formatting/diff checks, Python helper tests
make smoke                # offline CLI examples; optional PDF preview when Poppler exists
make fuzz                 # short bounded runs of the core parsers
```

The Go suite needs no screen, captures, external services, credentials, or local
font installation. Required fixtures are synthetic/package-local or original
examples. Missing required files fail tests. The **optional** Poppler integration
test is skipped only when `pdftocairo` is absent; CI installs Poppler. USB tests
use fake ports and do not claim hardware validation.

Protocol tests check independently written expected bytes and asymmetric pixel
mapping. Renderer goldens are the regression baseline; do not regenerate them
merely to make a change pass. Explain intentional visual changes. Daemon tests
use loopback fake screens to exercise busy submissions, ACK ownership, timeouts,
connection replacement, partial fallback, shutdown, and invalid input.

## Before a pull request

- Add a regression test for a bug, and preserve protocol/visual behavior unless
  the change is intentional and supported by evidence.
- Run `make test`, `make smoke`, and `make fuzz`. For release/security changes,
  run `make audit` with `govulncheck` and `gitleaks` installed (versions are pinned
  in CI), plus the archive checks in [releases](docs/releases.md).
- Update examples/schema/CLI reference together when changing scenes. Resource
  checks and layout validation supplement JSON Schema; an editor accepting a
  document does not imply it will render successfully.
- Keep hardware validation separate from software tests. Include panel/firmware
  information without publishing unique device identifiers or raw captures.
- Do not add cloud fetching to the daemon, automatic startup delivery, silent
  queues, capture-dependent test skips, or unbounded decoding/allocation paths.

New fixtures must be original/synthetic or explicitly licensed and sanitized.
Never submit credentials, Wi-Fi configuration, private PDFs/images, runtime logs,
LAN topology, or personal filesystem paths. Preserve dependency/font notices.
Code contributions use the project's MIT license; bundled fonts retain OFL.
