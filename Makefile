# Local developer Makefile
# Builds the provider binary into ./bin instead of $GOPATH/bin.

GO ?= go
BIN_DIR ?= bin
BINARY_NAME ?= terraform-provider-datahub
TOOL_NAME ?= datahub-tf-extract
MAIN ?= ./main.go
TOOL_MAIN ?= ./cmd/datahub-tf-extract
DEV_TFRC ?= $(PWD)/dev.tfrc
COVERAGE_FILE ?= coverage.out
COVERAGE_HTML ?= coverage.html
COVER_PKG ?= ./internal/...
DATAHUB_GMS_URL ?= http://localhost:8080
QUICKSTART_GMS_URL := http://localhost:8080
TOKEN_ACTOR ?= urn:li:corpuser:datahub
QUICKSTART_VERSION ?= v1.5.0.6
QUICKSTART_HEALTH_TIMEOUT ?= 600
QUICKSTART_HEALTH_INTERVAL ?= 5

# Best-effort version string (used for main.version via -ldflags)
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS ?= -X main.version=$(VERSION)

.PHONY: all help build install clean fmt lint generate bump-examples test testacc testacc-local testacc-remote testacc-quickstart quickstart-up quickstart-down quickstart-token coverage coverage-html dev-override dev-deps

all: install

help:
	@echo "Targets:"
	@echo "  build         Build both $(BIN_DIR)/$(BINARY_NAME) and $(BIN_DIR)/$(TOOL_NAME)"
	@echo "  install       Alias for build"
	@echo "  dev-override  Generate dev.tfrc for local provider development"
	@echo "  clean         Remove built binaries"
	@echo "  fmt           Format Go sources"
	@echo "  lint          Run golangci-lint"
	@echo "  generate      Run go generate in tools/"
	@echo "  bump-examples Bump the datahub provider version pin across all examples; VERSION=x.y.z required"
	@echo "  test          Run unit tests"
	@echo "  testacc            Run acceptance tests against the in-memory mock (no live DataHub needed; env vars cleared)"
	@echo "  testacc-local      Run acceptance tests against a DataHub instance already running at localhost:8080 (BYO);"
	@echo "                     Cloud vs OSS auto-detected via GET /config; DATAHUB_CLOUD=1 or =0 to override"
	@echo "  testacc-quickstart Boot a fresh OSS DataHub Quickstart at localhost, run acceptance tests, then nuke;"
	@echo "                     KEEP_QUICKSTART=1 to skip nuke; auto-detected as OSS"
	@echo "  testacc-remote     Run acceptance tests against a remote DataHub instance"
	@echo "                     (DATAHUB_GMS_URL + DATAHUB_GMS_TOKEN required; loopback URLs refused);"
	@echo "                     Cloud vs OSS auto-detected via GET /config; DATAHUB_CLOUD=1 or =0 to override"
	@echo "  coverage           Run all tests with merged coverage; prints total"
	@echo "  coverage-html      Run coverage, then write $(COVERAGE_HTML)"
	@echo "  dev-deps           Install Python dev dependencies (datahub CLI) into .venv"
	@echo "  quickstart-up      Start (or reuse) a local DataHub Quickstart; FRESH=1 nukes first; QUICKSTART_VERSION=vX.Y.Z overrides image"
	@echo "  quickstart-down    Tear down the Quickstart (datahub docker nuke)"
	@echo "  quickstart-token   Mint a DataHub PAT against the running Quickstart"

build:
	@mkdir -p "$(BIN_DIR)"
	$(GO) build -trimpath -ldflags "$(LDFLAGS)" -o "$(BIN_DIR)/$(BINARY_NAME)" "$(MAIN)"
	$(GO) build -trimpath -ldflags "-X main.version=$(VERSION)" -o "$(BIN_DIR)/$(TOOL_NAME)" "$(TOOL_MAIN)"

install: build

dev-deps:
	uv venv --allow-existing .venv
	UV_INDEX= uv pip install -r requirements-dev.txt

quickstart-up:
	@if [ "$$FRESH" = "1" ]; then \
		echo "FRESH=1: nuking existing Quickstart"; \
		datahub docker nuke >/dev/null 2>&1 || true; \
	fi
	@if datahub docker check >/dev/null 2>&1; then \
		echo "Quickstart already healthy; reusing"; \
	else \
		echo "Starting Quickstart (first pull can take 5-10 min)"; \
		datahub docker quickstart --version $(QUICKSTART_VERSION); \
	fi
	@echo "Polling GMS until ready..."
	@end=$$(( $$(date +%s) + $(QUICKSTART_HEALTH_TIMEOUT) )); \
	while ! datahub docker check >/dev/null 2>&1; do \
		if [ $$(date +%s) -ge $$end ]; then \
			echo "Quickstart did not become healthy within $(QUICKSTART_HEALTH_TIMEOUT)s"; \
			exit 1; \
		fi; \
		sleep $(QUICKSTART_HEALTH_INTERVAL); \
	done
	@echo "Quickstart healthy at $(QUICKSTART_GMS_URL)"

quickstart-down:
	datahub docker nuke

quickstart-token:
	@DATAHUB_GMS_URL=$(QUICKSTART_GMS_URL) TOKEN_ACTOR=$(TOKEN_ACTOR) scripts/quickstart-token.sh

testacc-quickstart:
	@if [ "$$KEEP_QUICKSTART" != "1" ]; then \
		trap 'echo "Tearing down Quickstart"; datahub docker nuke >/dev/null 2>&1 || true' EXIT; \
	fi; \
	set -e; \
	$(MAKE) quickstart-up; \
	$(MAKE) testacc-local

dev-override: dev-deps
	@{ \
		echo 'provider_installation {'; \
		echo '  dev_overrides {'; \
		echo '    "registry.terraform.io/datahub-project/datahub" = "$(PWD)/$(BIN_DIR)"'; \
		echo '  }'; \
		echo '  direct {}'; \
		echo '}'; \
	} > $(DEV_TFRC)
	@echo "TF_CLI_CONFIG_FILE=$(DEV_TFRC)" > .mise.env
	@echo "Written $(DEV_TFRC) and .mise.env"
	@echo "Run 'cd .' to activate TF_CLI_CONFIG_FILE in your current shell."

clean:
	@rm -f "$(BIN_DIR)/$(BINARY_NAME)" "$(BIN_DIR)/$(TOOL_NAME)"

fmt:
	gofmt -s -w -e .

lint:
	golangci-lint run

generate:
	cd tools; $(GO) generate ./...

bump-examples:
ifndef VERSION
	$(error VERSION is required, e.g. make bump-examples VERSION=0.3.0)
endif
	cd tools && $(GO) run github.com/minamijoyo/tfupdate provider \
	  --version $(VERSION) \
	  -r \
	  datahub \
	  ../examples

test:
	$(GO) test -v -cover -timeout=120s -parallel=10 ./...

testacc:
	TF_ACC=1 DATAHUB_GMS_URL= DATAHUB_GMS_TOKEN= $(GO) test -v -cover -timeout 120m ./...

testacc-local: install
	@TOKEN=$$(DATAHUB_GMS_URL=$(QUICKSTART_GMS_URL) TOKEN_ACTOR=$(TOKEN_ACTOR) scripts/quickstart-token.sh) || { echo "Failed to mint PAT against $(QUICKSTART_GMS_URL)"; exit 1; }; \
	TF_ACC=1 DATAHUB_GMS_URL=$(QUICKSTART_GMS_URL) DATAHUB_GMS_TOKEN="$$TOKEN" $(GO) test -v -timeout 30m ./...

testacc-remote:
	@if [ -z "$$DATAHUB_GMS_URL" ] || [ -z "$$DATAHUB_GMS_TOKEN" ]; then \
		echo "DATAHUB_GMS_URL and DATAHUB_GMS_TOKEN must be set; see BUILDING.md."; \
		exit 1; \
	fi
	@case "$$DATAHUB_GMS_URL" in \
		*://localhost[:/]*|*://localhost|*://127.0.0.1[:/]*|*://127.0.0.1|*://\[::1\][:/]*|*://\[::1\]) \
			echo "testacc-remote refuses loopback URL $$DATAHUB_GMS_URL; use testacc-local or testacc-quickstart instead."; \
			exit 1 ;; \
	esac
	@echo ""
	@echo "==> testacc-remote will run against: $$DATAHUB_GMS_URL"
	@if [ -n "$$DATAHUB_CLOUD" ]; then \
		if [ "$$DATAHUB_CLOUD" = "1" ]; then \
			echo "==> Mode: Cloud (DATAHUB_CLOUD=1 forced) -- Cloud-only tests will run"; \
		else \
			echo "==> Mode: OSS (DATAHUB_CLOUD=0 forced) -- Cloud-only tests skipped"; \
		fi; \
	else \
		GMS=$$(printf '%s' "$$DATAHUB_GMS_URL" | sed 's|/$$||'); \
		RESP=$$(curl -sS -H "Authorization: Bearer $$DATAHUB_GMS_TOKEN" -w "\n%{http_code}" "$$GMS/config" 2>&1); \
		CODE=$$(printf '%s' "$$RESP" | tail -1); \
		BODY=$$(printf '%s' "$$RESP" | sed '$$d'); \
		ENV=$$(printf '%s' "$$BODY" | jq -r '.datahub.serverEnv // empty' 2>/dev/null); \
		if [ -z "$$ENV" ]; then \
			ENV=$$(curl -sS -H "Authorization: Bearer $$DATAHUB_GMS_TOKEN" "$$GMS/api/gms/config" 2>/dev/null | jq -r '.datahub.serverEnv // empty' 2>/dev/null); \
		fi; \
		if [ "$$ENV" = "cloud" ]; then \
			echo "==> Mode: DataHub Cloud auto-detected (serverEnv=cloud) -- Cloud-only tests will run"; \
		elif [ -n "$$ENV" ]; then \
			echo "==> Mode: OSS DataHub auto-detected (serverEnv=$$ENV) -- Cloud-only tests skipped"; \
		else \
			echo "==> Mode: probe failed (HTTP $$CODE); treating as OSS. Set DATAHUB_CLOUD=1 to force Cloud."; \
		fi; \
	fi
	@echo "==> (Set DATAHUB_CLOUD=1 or DATAHUB_CLOUD=0 to override detection)"
	@echo "==> Starting in 3s. Ctrl-C to abort."
	@echo ""
	@sleep 3
	TF_ACC=1 $(GO) test -v -timeout 30m ./...

coverage:
	TF_ACC=1 $(GO) test -coverprofile=$(COVERAGE_FILE) -coverpkg=$(COVER_PKG) -timeout 120m ./...
	@echo ""
	@$(GO) tool cover -func=$(COVERAGE_FILE) | tail -1

coverage-html: coverage
	$(GO) tool cover -html=$(COVERAGE_FILE) -o $(COVERAGE_HTML)
	@echo "Wrote $(COVERAGE_HTML). Open with: open $(COVERAGE_HTML)"
