

BUILD_DIR=./build
BUILD=$(shell git rev-parse --short HEAD)@$(shell date +%s)
LD_FLAGS=-ldflags "-X main.BuildVersion=$(BUILD)"
GO=go
GO_BUILD=CGO_ENABLED=0 go build $(LD_FLAGS)

.PHONY: build
build:
	$(GO_BUILD) -o $(BUILD_DIR)/ ./cmd/mcpap

.PHONY: test
test:
	$(GO) test ./...

.PHONY: format
format:
	golangci-lint fmt --no-config --enable gofmt,goimports
	golangci-lint run --no-config --fix
	go fmt ./...
	go mod tidy