# Define variables for binary name and path
BINARY_NAME=reposqueeze
BINARY_PATH=./bin/$(BINARY_NAME)
BINARY_PATH_LINUX=./bin/$(BINARY_NAME)-linux

# Phony targets to avoid conflicts with files of the same name
.PHONY: all build build-linux pack pack-linux tui help clipsync

# The default target is 'help'
all: 
	$(MAKE) build
	$(MAKE) build-linux
	$(MAKE) pack
	$(MAKE) pack-linux

# Build the Go application
# Compiles the source code from cmd/app/main.go and places the output in bin/
build:
	@echo "Building the application..."
	@go build -o $(BINARY_PATH) ./cmd/app/main.go
	@echo "Build complete: $(BINARY_PATH)"

# Pack the binary using upx
# This target depends on 'build', so 'build' will run automatically before 'pack'
pack: build
	@echo "Packing the binary with upx..."
	@upx --best --lzma --force-macos $(BINARY_PATH)
	@echo "Packing complete."

# Build the Go application for Linux
build-linux:
	@echo "Building the application for Linux..."
	@GOOS=linux GOARCH=amd64 go build -o $(BINARY_PATH_LINUX) ./cmd/app/main.go
	@echo "Build complete: $(BINARY_PATH_LINUX)"

# Pack the Linux binary using upx
pack-linux: build-linux
	@echo "Packing the Linux binary with upx..."
	@upx --best --lzma $(BINARY_PATH_LINUX)
	@echo "Packing complete."

# Run the TUI mode (builds first)
tui: build
	@$(BINARY_PATH) tui

# Help target for self-documentation
help:
	@echo "Usage: make [target]"
	@echo "------------------"
	@echo "Available targets:"
	@echo "  build         Compile the application for the current OS."
	@echo "  tui           Build and launch the interactive TUI mode."
	@echo "  pack          Build and then pack the application with upx."
	@echo "  build-linux   Compile the application for Linux."
	@echo "  pack-linux    Build and then pack the Linux application with upx."
	@echo "  clipsync      Build the clipboard helper into ~/.clipsync/bin (macOS)."
	@echo "  help          Show this help message."

# Build the clipboard helper (macOS, needs Xcode command line tools) as a
# universal binary into ~/.clipsync/bin, where `reposqueeze clip` looks for it.
CLIPSYNC_DIR=$(HOME)/.clipsync/bin
clipsync:
	@mkdir -p $(CLIPSYNC_DIR) ./bin
	@swiftc -O -target arm64-apple-macos12 -o ./bin/clipsync-arm64 tools/clipsync/clipsync.swift
	@swiftc -O -target x86_64-apple-macos12 -o ./bin/clipsync-x86_64 tools/clipsync/clipsync.swift
	@lipo -create -output $(CLIPSYNC_DIR)/clipsync ./bin/clipsync-arm64 ./bin/clipsync-x86_64
	@rm -f ./bin/clipsync-arm64 ./bin/clipsync-x86_64
	@echo "Installed: $(CLIPSYNC_DIR)/clipsync"
