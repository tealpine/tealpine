

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
	@echo "Running tests..."
	@go test -v ./...

.PHONY: format
format:
	golangci-lint fmt --no-config --enable gofmt,goimports
	golangci-lint run --no-config --fix
	go fmt ./...
	go mod tidy


.PHONY: start-mcps
start-mcps:
	@echo "Starting MCPs..."
	@go run test/mcp_servers/main.go calc server > calc.log 2>&1 & echo $$! > calc.pid
	@go run test/mcp_servers/main.go temp server > temp.log 2>&1 & echo $$! > temp.pid
	@echo "MCPs started"

.PHONY: stop-mcps
stop-mcps:
	@echo "Stopping MCPs..."
	@if [ -f calc.pid ]; then \
		kill `cat calc.pid` 2>/dev/null || true; \
		rm -f calc.pid; \
	fi
	@if [ -f temp.pid ]; then \
		kill `cat temp.pid` 2>/dev/null || true; \
		rm -f temp.pid; \
	fi
	@rm -f temp.log calc.log
	@echo "MCPs stopped"

.PHONY: clean
clean: stop-mcps
	rm -f temp.pid temp.log calc.pid calc.log