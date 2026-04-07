.PHONY: build clean test install all uninstall

# Binary name
BINARY=aispace-men

# Build directory
BUILD_DIR=./

# Go parameters
GOCMD=go
GOBUILD=$(GOCMD) build
GOCLEAN=$(GOCMD) clean
GOTEST=$(GOCMD) test
GOINSTALL=$(GOCMD) install

# Version (from git or default)
VERSION?=$(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
BUILD_TIME=$(shell date -u '+%Y-%m-%d_%H:%M:%S')
LDFLAGS=-ldflags "-X main.version=$(VERSION)"

# Default target
all: build

# Build the binary
build:
	$(GOBUILD) $(LDFLAGS) -o $(BUILD_DIR)$(BINARY) ./cmd/aispace-men

# Build for multiple platforms
build-all: build-darwin build-linux build-windows

build-darwin:
	GOOS=darwin GOARCH=amd64 $(GOBUILD) $(LDFLAGS) -o $(BUILD_DIR)$(BINARY)-darwin-amd64 ./cmd/aispace-men
	GOOS=darwin GOARCH=arm64 $(GOBUILD) $(LDFLAGS) -o $(BUILD_DIR)$(BINARY)-darwin-arm64 ./cmd/aispace-men

build-linux:
	GOOS=linux GOARCH=amd64 $(GOBUILD) $(LDFLAGS) -o $(BUILD_DIR)$(BINARY)-linux-amd64 ./cmd/aispace-men
	GOOS=linux GOARCH=arm64 $(GOBUILD) $(LDFLAGS) -o $(BUILD_DIR)$(BINARY)-linux-arm64 ./cmd/aispace-men

build-windows:
	GOOS=windows GOARCH=amd64 $(GOBUILD) $(LDFLAGS) -o $(BUILD_DIR)$(BINARY)-windows-amd64.exe ./cmd/aispace-men

# Run tests
test:
	$(GOTEST) -v ./...

# Clean build artifacts
clean:
	$(GOCLEAN)
	rm -f $(BUILD_DIR)$(BINARY)
	rm -f $(BUILD_DIR)$(BINARY)-*

# Install locally — copies to all locations that may appear in PATH
# so there are never two different versions coexisting.
install: build
	@mkdir -p $(HOME)/.local/bin
	cp $(BUILD_DIR)$(BINARY) $(HOME)/.local/bin/$(BINARY)
	@if [ -d $(HOME)/go/bin ]; then cp $(BUILD_DIR)$(BINARY) $(HOME)/go/bin/$(BINARY); fi
	@if [ -d /opt/homebrew/bin ] && [ -f /opt/homebrew/bin/$(BINARY) ]; then cp $(BUILD_DIR)$(BINARY) /opt/homebrew/bin/$(BINARY); fi
	@echo "Installed $(BINARY) $(VERSION)"
	@echo "  → $(HOME)/.local/bin/$(BINARY)"
	@if [ -d $(HOME)/go/bin ]; then echo "  → $(HOME)/go/bin/$(BINARY)"; fi
	@if [ -d /opt/homebrew/bin ] && [ -f /opt/homebrew/bin/$(BINARY) ]; then echo "  → /opt/homebrew/bin/$(BINARY)"; fi

# Remove all installed copies
uninstall:
	@rm -f $(HOME)/.local/bin/$(BINARY)
	@rm -f $(HOME)/go/bin/$(BINARY)
	@rm -f /opt/homebrew/bin/$(BINARY)
	@echo "Uninstalled $(BINARY)"

# Run the MCP server (for testing)
run-mcp:
	./$(BINARY) mcp

# Show stats
stats:
	./$(BINARY) stats

# Development - watch for changes and rebuild
watch:
	@which reflex > /dev/null || (echo "Installing reflex..." && go install github.com/cespare/reflex@latest)
	reflex -r '\.go$$' -s -- sh -c 'make build && echo "Build complete: $(shell date)"'