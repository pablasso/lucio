GO ?= go
VERSION ?= dev
FUZZTIME ?= 5s
LDFLAGS = -X github.com/pablasso/lucio/internal/cli.Version=$(VERSION)

.PHONY: build test check smoke fuzz audit release
build:
	$(GO) build -trimpath -ldflags '$(LDFLAGS)' -o bin/lucio ./cmd/lucio
	$(GO) build -trimpath -o bin/paperboy ./cmd/paperboy

check:
	@test -z "$$(find cmd internal schemas -name '*.go' -type f -print0 | xargs -0 gofmt -l)" || (echo 'Run gofmt on Go sources' >&2; exit 1)
	git diff --check HEAD
	$(GO) vet ./...
	python3 scripts/check-docs.py

test:
	$(GO) test -race ./...
	python3 -m unittest discover -s tools
	python3 -m unittest discover -s scripts
	$(MAKE) check

smoke: build
	sh scripts/smoke.sh

fuzz:
	$(GO) test ./internal/pv3 -run '^$$' -fuzz '^FuzzFrame$$' -fuzztime $(FUZZTIME) -parallel 2
	$(GO) test ./internal/pv3 -run '^$$' -fuzz '^FuzzCompression$$' -fuzztime $(FUZZTIME) -parallel 2
	$(GO) test ./internal/renderer -run '^$$' -fuzz '^FuzzScene$$' -fuzztime $(FUZZTIME) -parallel 2

audit:
	govulncheck ./...
	gitleaks git --redact --no-banner .
	gitleaks dir --redact --no-banner .

release:
	sh scripts/release.sh '$(VERSION)'
