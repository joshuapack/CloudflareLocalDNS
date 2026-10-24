.DEFAULT_GOAL := help

SERVICE_NAME := cloudflarelocaldns
SERVICE_FILE := /etc/systemd/system/$(SERVICE_NAME).service
PROJECT_DIR := $(CURDIR)
BIN_PATH := $(PROJECT_DIR)/$(SERVICE_NAME)
ENV_PATH := $(PROJECT_DIR)/.env

.PHONY: help build build-mac build-linux-arm build-linux-arm64 build-all run clean \
        install-service service-start service-stop service-restart service-status \
        service-logs uninstall-service

help:
	@echo "Usage: make <target>"
	@echo ""
	@echo "Build targets:"
	@echo "  help               Show this help message"
	@echo "  build              Build binary for current system (cloudflarelocaldns)"
	@echo "  build-mac          Build binary for macOS arm64 (cloudflarelocaldns-mac)"
	@echo "  build-linux-arm    Build binary for Linux arm64 (cloudflarelocaldns-linux-arm)"
	@echo "  build-all          Build binaries for both Mac and Linux ARM64"
	@echo "  run                Run directly with 'go run main.go'"
	@echo "  clean              Remove all built binaries"
	@echo ""
	@echo "Service management (Debian / Raspberry Pi systemd):"
	@echo "  install-service    Install, configure, enable, and start systemd service"
	@echo "  service-start      Start the service"
	@echo "  service-stop       Stop the service"
	@echo "  service-restart    Restart the service"
	@echo "  service-status     Check service status"
	@echo "  service-logs       Follow live service logs via journalctl"
	@echo "  uninstall-service  Stop, disable, and remove the systemd service"

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

install-service:
	@if ! command -v systemctl >/dev/null 2>&1; then \
		echo "Error: systemctl not found. This installation requires a systemd-based system (e.g. Debian, Ubuntu, Raspberry Pi OS)."; \
		exit 1; \
	fi
	@if [ "$$(id -u)" -ne 0 ]; then \
		echo "Error: install-service requires root privileges. Please run: sudo make install-service"; \
		exit 1; \
	fi
	@if [ -f "$(SERVICE_FILE)" ]; then \
		echo "Error: Service is already installed at $(SERVICE_FILE)."; \
		echo "Use 'make service-restart' to apply changes, or 'sudo make uninstall-service' before reinstalling."; \
		exit 1; \
	fi
	@echo "Locating binary for service..."
	@if [ -f "$(PROJECT_DIR)/cloudflarelocaldns" ]; then \
		echo "Found $(BIN_PATH)..."; \
	elif [ -f "$(PROJECT_DIR)/cloudflarelocaldns-linux-arm" ]; then \
		echo "Found cloudflarelocaldns-linux-arm, setting up $(BIN_PATH)..."; \
		cp $(PROJECT_DIR)/cloudflarelocaldns-linux-arm $(BIN_PATH); \
	elif command -v go >/dev/null 2>&1; then \
		echo "Building binary with local Go..."; \
		go build -o $(BIN_PATH) main.go; \
	else \
		echo "Error: No compiled binary found and 'go' is not installed."; \
		echo "Please run 'make build-linux-arm' or 'make build' first, or install Go."; \
		exit 1; \
	fi
	@chmod +x $(BIN_PATH)
	@echo "Service configured to run: $(BIN_PATH)"
	@if [ ! -f "$(ENV_PATH)" ]; then \
		echo "Warning: No .env file found at $(ENV_PATH). Please create it with AUTH_KEY and DOMAIN."; \
	fi
	@echo "Creating systemd unit file at $(SERVICE_FILE)..."
	@printf "[Unit]\nDescription=Cloudflare Local DNS Updater\nAfter=network-online.target\nWants=network-online.target\n\n[Service]\nType=simple\nUser=root\nWorkingDirectory=$(PROJECT_DIR)\nEnvironmentFile=$(ENV_PATH)\nEnvironment=ENV_FILE=$(ENV_PATH)\nExecStart=$(BIN_PATH)\nRestart=always\nRestartSec=10\n\n[Install]\nWantedBy=multi-user.target\n" > $(SERVICE_FILE)
	@systemctl daemon-reload
	@systemctl enable $(SERVICE_NAME)
	@systemctl start $(SERVICE_NAME)
	@echo "--------------------------------------------------------"
	@echo "Service '$(SERVICE_NAME)' successfully installed & started!"
	@echo "Config: $(ENV_PATH)"
	@echo "Status: make service-status"
	@echo "Logs:   make service-logs"
	@echo "--------------------------------------------------------"

service-start:
	@if ! command -v systemctl >/dev/null 2>&1; then \
		echo "Error: systemctl not found. This target requires a systemd-based system."; \
		exit 1; \
	fi
	@if [ ! -f "$(SERVICE_FILE)" ]; then \
		echo "Error: Service is not installed. Run 'sudo make install-service' first."; \
		exit 1; \
	fi
	@if [ "$$(id -u)" -ne 0 ]; then sudo systemctl start $(SERVICE_NAME); else systemctl start $(SERVICE_NAME); fi
	@echo "Service '$(SERVICE_NAME)' started."

service-stop:
	@if ! command -v systemctl >/dev/null 2>&1; then \
		echo "Error: systemctl not found. This target requires a systemd-based system."; \
		exit 1; \
	fi
	@if [ ! -f "$(SERVICE_FILE)" ]; then \
		echo "Error: Service is not installed."; \
		exit 1; \
	fi
	@if [ "$$(id -u)" -ne 0 ]; then sudo systemctl stop $(SERVICE_NAME); else systemctl stop $(SERVICE_NAME); fi
	@echo "Service '$(SERVICE_NAME)' stopped."

service-restart:
	@if ! command -v systemctl >/dev/null 2>&1; then \
		echo "Error: systemctl not found. This target requires a systemd-based system."; \
		exit 1; \
	fi
	@if [ ! -f "$(SERVICE_FILE)" ]; then \
		echo "Error: Service is not installed. Run 'sudo make install-service' first."; \
		exit 1; \
	fi
	@if [ "$$(id -u)" -ne 0 ]; then \
		sudo systemctl daemon-reload; \
		sudo systemctl restart $(SERVICE_NAME); \
	else \
		systemctl daemon-reload; \
		systemctl restart $(SERVICE_NAME); \
	fi
	@echo "Service '$(SERVICE_NAME)' restarted."

service-status:
	@if ! command -v systemctl >/dev/null 2>&1; then \
		echo "Error: systemctl not found. This target requires a systemd-based system."; \
		exit 1; \
	fi
	@systemctl status $(SERVICE_NAME)

service-logs:
	@if ! command -v journalctl >/dev/null 2>&1; then \
		echo "Error: journalctl not found. This target requires a systemd-based system."; \
		exit 1; \
	fi
	@journalctl -u $(SERVICE_NAME) -f

uninstall-service:
	@if ! command -v systemctl >/dev/null 2>&1; then \
		echo "Error: systemctl not found. This target requires a systemd-based system."; \
		exit 1; \
	fi
	@if [ "$$(id -u)" -ne 0 ]; then \
		echo "Error: uninstall-service requires root privileges. Please run: sudo make uninstall-service"; \
		exit 1; \
	fi
	@if [ ! -f "$(SERVICE_FILE)" ]; then \
		echo "Service '$(SERVICE_NAME)' is not installed."; \
		exit 0; \
	fi
	@echo "Stopping and disabling service..."
	@systemctl stop $(SERVICE_NAME) 2>/dev/null || true
	@systemctl disable $(SERVICE_NAME) 2>/dev/null || true
	@rm -f $(SERVICE_FILE)
	@systemctl daemon-reload
	@echo "Service '$(SERVICE_NAME)' has been removed."
