APP_NAME := managed-services-registry
BUILD_DIR := dist
CACHE_DIR := .cache
GO_BUILD_CACHE := $(CACHE_DIR)/go-build
GO_MOD_CACHE := $(CACHE_DIR)/go-mod
BIN := $(BUILD_DIR)/$(APP_NAME)
LINUX_GOOS ?= linux
LINUX_GOARCH ?= amd64
LINUX_BIN := $(BUILD_DIR)/$(APP_NAME)-$(LINUX_GOOS)-$(LINUX_GOARCH)
GO ?= go
GOFLAGS ?= -mod=mod
PKGS := ./...
GOLANGCI_LINT_VERSION ?= v2.4.0
GOLANGCI_LINT := $(GO) run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)

export GOCACHE := $(abspath $(GO_BUILD_CACHE))
export GOMODCACHE := $(abspath $(GO_MOD_CACHE))

.PHONY: all build build-linux test lint fmt clean

all: fmt lint test build

build:
	mkdir -p $(BUILD_DIR) $(GO_BUILD_CACHE) $(GO_MOD_CACHE)
	$(GO) build $(GOFLAGS) -o $(BIN) ./cmd/server

build-linux:
	mkdir -p $(BUILD_DIR) $(GO_BUILD_CACHE) $(GO_MOD_CACHE)
	GOOS=$(LINUX_GOOS) GOARCH=$(LINUX_GOARCH) $(GO) build $(GOFLAGS) -o $(LINUX_BIN) ./cmd/server

test:
	mkdir -p $(GO_BUILD_CACHE) $(GO_MOD_CACHE)
	$(GO) test $(GOFLAGS) $(PKGS)

lint:
	mkdir -p $(GO_BUILD_CACHE) $(GO_MOD_CACHE)
	$(GOLANGCI_LINT) run

fmt:
	mkdir -p $(GO_BUILD_CACHE) $(GO_MOD_CACHE)
	$(GO) fmt $(PKGS)

clean:
	rm -rf $(BUILD_DIR) $(CACHE_DIR)
