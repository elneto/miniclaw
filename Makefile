# MiniClaw Makefile
# Build for your machine, or cross-compile for Raspberry Pi

APP      := miniclaw
VERSION  := 0.1.0
BUILD    := $(shell date -u +%Y%m%d%H%M%S)

# Default: build for current platform
.PHONY: build
build:
	go build -ldflags "-s -w" -o $(APP) .

# Raspberry Pi (ARM 32-bit, e.g. Pi 3/4 with 32-bit OS)
.PHONY: pi32
pi32:
	GOOS=linux GOARCH=arm GOARM=7 go build -ldflags "-s -w" -o $(APP)-linux-arm .

# Raspberry Pi (ARM 64-bit, e.g. Pi 4/5 with 64-bit OS)
.PHONY: pi64
pi64:
	GOOS=linux GOARCH=arm64 go build -ldflags "-s -w" -o $(APP)-linux-arm64 .

# Linux x86_64 (laptop/desktop)
.PHONY: linux
linux:
	GOOS=linux GOARCH=amd64 go build -ldflags "-s -w" -o $(APP)-linux-amd64 .

# macOS (Apple Silicon)
.PHONY: mac-arm
mac-arm:
	GOOS=darwin GOARCH=arm64 go build -ldflags "-s -w" -o $(APP)-darwin-arm64 .

# macOS (Intel)
.PHONY: mac-intel
mac-intel:
	GOOS=darwin GOARCH=amd64 go build -ldflags "-s -w" -o $(APP)-darwin-amd64 .

# Build ALL platforms
.PHONY: all
all: build pi32 pi64 linux mac-arm mac-intel
	@echo "✅ Built for all platforms"

# Run locally
.PHONY: run
run: build
	./$(APP)

# Clean build artifacts
.PHONY: clean
clean:
	rm -f $(APP) $(APP)-*

# Install to system (Linux/macOS)
.PHONY: install
install: build
	mkdir -p ~/.miniclaw/workspace
	cp $(APP) /usr/local/bin/$(APP)
	@if [ ! -f ~/.miniclaw/config.yaml ]; then \
		cp config.yaml ~/.miniclaw/config.yaml; \
		echo "📝 Edit ~/.miniclaw/config.yaml with your Telegram token and user ID"; \
	fi
	@echo "✅ Installed. Run with: miniclaw -config ~/.miniclaw/config.yaml"

# Create systemd service (Linux)
.PHONY: systemd
systemd:
	@echo "[Unit]" > /tmp/miniclaw.service
	@echo "Description=MiniClaw Remote Agent" >> /tmp/miniclaw.service
	@echo "After=network.target ollama.service" >> /tmp/miniclaw.service
	@echo "" >> /tmp/miniclaw.service
	@echo "[Service]" >> /tmp/miniclaw.service
	@echo "Type=simple" >> /tmp/miniclaw.service
	@echo "User=$(shell whoami)" >> /tmp/miniclaw.service
	@echo "ExecStart=/usr/local/bin/miniclaw -config $(HOME)/.miniclaw/config.yaml" >> /tmp/miniclaw.service
	@echo "Restart=always" >> /tmp/miniclaw.service
	@echo "RestartSec=5" >> /tmp/miniclaw.service
	@echo "" >> /tmp/miniclaw.service
	@echo "[Install]" >> /tmp/miniclaw.service
	@echo "WantedBy=multi-user.target" >> /tmp/miniclaw.service
	sudo mv /tmp/miniclaw.service /etc/systemd/system/miniclaw.service
	sudo systemctl daemon-reload
	@echo "✅ Service created. Enable with:"
	@echo "   sudo systemctl enable miniclaw"
	@echo "   sudo systemctl start miniclaw"

.PHONY: help
help:
	@echo "MiniClaw v$(VERSION) — Build targets:"
	@echo ""
	@echo "  make build      Build for current platform"
	@echo "  make pi32       Build for Raspberry Pi (32-bit)"
	@echo "  make pi64       Build for Raspberry Pi (64-bit)"
	@echo "  make linux      Build for Linux x86_64"
	@echo "  make mac-arm    Build for macOS Apple Silicon"
	@echo "  make mac-intel  Build for macOS Intel"
	@echo "  make all        Build for all platforms"
	@echo "  make run        Build and run"
	@echo "  make install    Install to /usr/local/bin"
	@echo "  make systemd    Create systemd service"
	@echo "  make clean      Remove build artifacts"
