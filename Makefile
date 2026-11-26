BIN := livery
VERSION ?= $(or $(shell git describe --tags --always --dirty 2>/dev/null | sed 's/^v//'),0.0.0-dev)
LDFLAGS := -s -w -X github.com/Pnakoryn/livery/internal/cli.Version=$(VERSION)
PLATFORMS := linux/amd64 linux/arm64 darwin/amd64 darwin/arm64

.PHONY: build test lint lint-ci e2e check dist clean

build:
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o $(BIN) ./cmd/livery

test:
	go test ./...

lint:
	golangci-lint run ./...

# The workflow files are passed explicitly, so the check does not depend on zizmor's discovery.
lint-ci:
	zizmor --offline .github/workflows/*.yml

# User journeys: the built binary in a throwaway home (needs git, nothing else).
e2e: build
	go test -tags e2e ./e2e/ -count=1

# Everything a change must pass: build, tests, vet of the macOS build, lint, journeys.
check: build test
	GOOS=darwin GOARCH=arm64 go vet ./...
	$(MAKE) lint
	$(MAKE) e2e

# Static binaries for Linux and macOS in dist/, with SHA256SUMS.
dist:
	rm -rf dist
	mkdir -p dist
	@set -e; for p in $(PLATFORMS); do \
		os=$${p%/*}; arch=$${p#*/}; \
		echo "dist/$(BIN)-$(VERSION)-$$os-$$arch"; \
		CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch go build -trimpath -ldflags "$(LDFLAGS)" \
			-o dist/$(BIN)-$(VERSION)-$$os-$$arch ./cmd/livery; \
	done
	cd dist && if command -v sha256sum >/dev/null 2>&1; then sha256sum $(BIN)-*; else shasum -a 256 $(BIN)-*; fi > SHA256SUMS

clean:
	rm -rf $(BIN) dist
