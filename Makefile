# Local developer Makefile
# Builds the provider binary into ./bin instead of $GOPATH/bin.

GO ?= go
BIN_DIR ?= bin
BINARY_NAME ?= terraform-provider-datahub
MAIN ?= ./main.go
DEV_TFRC ?= $(PWD)/dev.tfrc
COVERAGE_FILE ?= coverage.out
COVERAGE_HTML ?= coverage.html
COVER_PKG ?= ./internal/...

# Best-effort version string (used for main.version via -ldflags)
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS ?= -X main.version=$(VERSION)

.PHONY: all help build install clean fmt lint generate test testacc testacc-local testacc-cloud coverage coverage-html dev-override

all: install

help:
	@echo "Targets:"
	@echo "  build         Build $(BIN_DIR)/$(BINARY_NAME) from $(MAIN)"
	@echo "  install       Alias for build (installs into $(BIN_DIR))"
	@echo "  dev-override  Generate dev.tfrc for local provider development"
	@echo "  clean         Remove built binary"
	@echo "  fmt           Format Go sources"
	@echo "  lint          Run golangci-lint"
	@echo "  generate      Run go generate in tools/"
	@echo "  test          Run unit tests"
	@echo "  testacc       Run acceptance tests against the in-memory mock (TF_ACC=1)"
	@echo "  testacc-local Run acceptance tests against a local DataHub Quickstart (env-gated)"
	@echo "  testacc-cloud Run acceptance tests against a DataHub cloud tenant (env-gated)"
	@echo "  coverage      Run all tests with merged coverage; prints total"
	@echo "  coverage-html Run coverage, then write $(COVERAGE_HTML)"

build:
	@mkdir -p "$(BIN_DIR)"
	$(GO) build -trimpath -ldflags "$(LDFLAGS)" -o "$(BIN_DIR)/$(BINARY_NAME)" "$(MAIN)"

install: build

dev-override:
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
	@rm -f "$(BIN_DIR)/$(BINARY_NAME)"

fmt:
	gofmt -s -w -e .

lint:
	golangci-lint run

generate:
	cd tools; $(GO) generate ./...

test:
	$(GO) test -v -cover -timeout=120s -parallel=10 ./...

testacc:
	TF_ACC=1 $(GO) test -v -cover -timeout 120m ./...

testacc-local:
	@if [ -z "$$DATAHUB_GMS_URL" ] || [ -z "$$DATAHUB_GMS_TOKEN" ]; then \
		echo "DATAHUB_GMS_URL and DATAHUB_GMS_TOKEN must be set; see BUILDING.md."; \
		exit 1; \
	fi
	TF_ACC=1 DATAHUB_TEST_TARGET=local $(GO) test -v -timeout 30m ./...

testacc-cloud:
	@if [ -z "$$DATAHUB_GMS_URL" ] || [ -z "$$DATAHUB_GMS_TOKEN" ]; then \
		echo "DATAHUB_GMS_URL and DATAHUB_GMS_TOKEN must be set; see BUILDING.md."; \
		exit 1; \
	fi
	@if [ "$$DATAHUB_TEST_ALLOW_CLOUD" != "1" ]; then \
		echo "DATAHUB_TEST_ALLOW_CLOUD=1 must be set to run against a cloud instance; see BUILDING.md."; \
		exit 1; \
	fi
	TF_ACC=1 DATAHUB_TEST_TARGET=cloud $(GO) test -v -timeout 30m ./...

coverage:
	TF_ACC=1 $(GO) test -coverprofile=$(COVERAGE_FILE) -coverpkg=$(COVER_PKG) -timeout 120m ./...
	@echo ""
	@$(GO) tool cover -func=$(COVERAGE_FILE) | tail -1

coverage-html: coverage
	$(GO) tool cover -html=$(COVERAGE_FILE) -o $(COVERAGE_HTML)
	@echo "Wrote $(COVERAGE_HTML). Open with: open $(COVERAGE_HTML)"
