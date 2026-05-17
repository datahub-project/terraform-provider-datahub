# Local developer Makefile
# Builds the provider binary into ./bin instead of $GOPATH/bin.

GO ?= go
BIN_DIR ?= bin
BINARY_NAME ?= terraform-provider-datahub
MAIN ?= ./main.go

# Best-effort version string (used for main.version via -ldflags)
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS ?= -X main.version=$(VERSION)

.PHONY: all help build install clean fmt lint generate test testacc

all: install

help:
	@echo "Targets:"
	@echo "  build    Build $(BIN_DIR)/$(BINARY_NAME) from $(MAIN)"
	@echo "  install  Alias for build (installs into $(BIN_DIR))"
	@echo "  clean    Remove built binary"
	@echo "  fmt      Format Go sources"
	@echo "  lint     Run golangci-lint"
	@echo "  generate Run go generate in tools/"
	@echo "  test     Run unit tests"
	@echo "  testacc  Run acceptance tests (TF_ACC=1)"

build: $(BIN_DIR)/$(BINARY_NAME)

install: build

$(BIN_DIR):
	@mkdir -p "$(BIN_DIR)"

$(BIN_DIR)/$(BINARY_NAME): | $(BIN_DIR)
	$(GO) build -trimpath -ldflags "$(LDFLAGS)" -o "$(BIN_DIR)/$(BINARY_NAME)" "$(MAIN)"

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
