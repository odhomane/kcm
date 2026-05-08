BIN       := kcm
MODULE    := github.com/odhomane/kcm
VERSION   ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS   := -s -w -X main.Version=$(VERSION)
GOFLAGS   := -trimpath

.PHONY: all build test lint install release clean tidy

all: build

## build: compile for the host platform
build:
	go build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o bin/$(BIN) ./cmd/kcm

## install: install to GOPATH/bin (or $GOBIN)
install:
	go install $(GOFLAGS) -ldflags "$(LDFLAGS)" ./cmd/kcm

## test: run all tests with race detector
test:
	go test -race -count=1 ./...

## test-cover: generate coverage report
test-cover:
	go test -race -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report: coverage.html"

## lint: run golangci-lint
lint:
	golangci-lint run ./...

## tidy: tidy and verify go.mod
tidy:
	go mod tidy
	go mod verify

## release: build cross-platform binaries via goreleaser
release:
	goreleaser release --clean

## release-snapshot: local snapshot release (no publish)
release-snapshot:
	goreleaser release --snapshot --clean

## cross: build for all supported platforms
cross:
	GOOS=darwin  GOARCH=amd64  go build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o bin/$(BIN)-darwin-amd64   ./cmd/kcm
	GOOS=darwin  GOARCH=arm64  go build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o bin/$(BIN)-darwin-arm64   ./cmd/kcm
	GOOS=linux   GOARCH=amd64  go build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o bin/$(BIN)-linux-amd64    ./cmd/kcm
	GOOS=linux   GOARCH=arm64  go build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o bin/$(BIN)-linux-arm64    ./cmd/kcm

## clean: remove build artifacts
clean:
	rm -rf bin/ dist/ coverage.out coverage.html

## size: show binary size (after build)
size: build
	@ls -lh bin/$(BIN)
	@file bin/$(BIN)
