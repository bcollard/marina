BINARY     := marina
MODULE     := github.com/bcollard/marina
VERSION    ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS    := -ldflags "-s -w -X main.version=$(VERSION)"
BUILD_DIR  := ./dist

# CGO_ENABLED=1 is required: Lima's instance/store packages link against macOS frameworks.
export CGO_ENABLED=1

.PHONY: build sign test lint install dev-install clean tidy snapshot release-check

# Sign with the Developer ID certificate when the keychain has one, ad-hoc
# otherwise. A Developer ID build keeps one identity across rebuilds, which is
# what an MDM/EDR allowlist keys on. Offline-friendly: no secure timestamp
# locally (release CI adds it, and notarizes).
# Override with: make build MARINA_SIGN_IDENTITY="Developer ID Application: ..."
MARINA_SIGN_IDENTITY ?= $(shell security find-identity -v -p codesigning 2>/dev/null \
	| sed -n 's/.*"\(Developer ID Application: [^"]*\)".*/\1/p' | head -1)

build:
	go build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY) ./cmd/marina/...
	MARINA_SIGN_IDENTITY="$(MARINA_SIGN_IDENTITY)" MARINA_SIGN_TIMESTAMP=none ./scripts/sign-darwin.sh $(BUILD_DIR)/$(BINARY)

test:
	go test ./...

lint:
	golangci-lint run ./...

install:
	go install $(LDFLAGS) ./cmd/marina/...

dev-install: build
	@if pgrep -qf '$(BINARY) hostagent'; then \
		echo "ERROR: marina hostagent is running. Run 'marina down' first, or:"; \
		echo "  kill \$$(pgrep -f '$(BINARY) hostagent')"; \
		exit 1; \
	fi
	sudo cp $(BUILD_DIR)/$(BINARY) /usr/local/bin/$(BINARY)

clean:
	rm -rf $(BUILD_DIR)

tidy:
	go mod tidy

# Local dry-run release (no publish). Requires goreleaser.
snapshot:
	goreleaser release --snapshot --clean

# Validate .goreleaser.yaml
release-check:
	goreleaser check
