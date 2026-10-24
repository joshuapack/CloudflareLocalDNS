.DEFAULT_GOAL := help

.PHONY: help build build-mac build-linux-arm build-linux-arm64 build-all run clean

help:
	@echo "Usage: make <target>"
	@echo ""
	@echo "Available targets:"
	@echo "  help               Show this help message"
	@echo "  build              Build binary for current system (cloudflarelocaldns)"
	@echo "  build-mac          Build binary for macOS arm64 (cloudflarelocaldns-mac)"
	@echo "  build-linux-arm    Build binary for Linux arm64 (cloudflarelocaldns-linux-arm)"
	@echo "  build-all          Build binaries for both Mac and Linux ARM64"
	@echo "  run                Run directly with 'go run main.go'"
	@echo "  clean              Remove all built binaries"

build:
	go build -o cloudflarelocaldns main.go

build-mac:
	GOOS=darwin GOARCH=arm64 go build -o cloudflarelocaldns-mac main.go

build-linux-arm:
	GOOS=linux GOARCH=arm64 go build -o cloudflarelocaldns-linux-arm main.go

build-linux-arm64: build-linux-arm

build-all: build-mac build-linux-arm

run:
	go run main.go

clean:
	rm -f cloudflarelocaldns cloudflarelocaldns-mac cloudflarelocaldns-linux-arm
