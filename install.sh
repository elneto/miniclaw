#!/bin/bash
# MiniClaw Quick Setup Script
# Run on your laptop or Raspberry Pi

set -e

echo "🐾 MiniClaw Quick Setup"
echo "======================="

# Check for Go
if ! command -v go &> /dev/null; then
    echo "📦 Go not found. Installing..."
    
    ARCH=$(uname -m)
    case $ARCH in
        x86_64)   GOARCH="amd64" ;;
        aarch64)  GOARCH="arm64" ;;
        armv7l)   GOARCH="armv6l" ;;
        *)        echo "❌ Unsupported architecture: $ARCH"; exit 1 ;;
    esac
    
    GO_VERSION="1.22.5"
    GO_TAR="go${GO_VERSION}.linux-${GOARCH}.tar.gz"
    
    wget -q "https://go.dev/dl/${GO_TAR}" -O /tmp/${GO_TAR}
    sudo rm -rf /usr/local/go
    sudo tar -C /usr/local -xzf /tmp/${GO_TAR}
    rm /tmp/${GO_TAR}
    
    export PATH=$PATH:/usr/local/go/bin
    echo 'export PATH=$PATH:/usr/local/go/bin' >> ~/.bashrc
    echo "✅ Go ${GO_VERSION} installed"
else
    echo "✅ Go found: $(go version)"
fi

# Check for Ollama
if ! command -v ollama &> /dev/null; then
    echo ""
    echo "📦 Ollama not found. Install it?"
    echo "   curl -fsSL https://ollama.ai/install.sh | sh"
    echo ""
    read -p "Install now? [y/N] " -n 1 -r
    echo
    if [[ $REPLY =~ ^[Yy]$ ]]; then
        curl -fsSL https://ollama.ai/install.sh | sh
        echo "✅ Ollama installed"
    fi
else
    echo "✅ Ollama found"
fi

# Build MiniClaw
echo ""
echo "🔨 Building MiniClaw..."
cd "$(dirname "$0")"
go mod tidy
make build
echo "✅ Built successfully"

# Setup config
mkdir -p ~/.miniclaw/workspace
if [ ! -f ~/.miniclaw/config.yaml ]; then
    cp config.yaml ~/.miniclaw/config.yaml
    echo ""
    echo "📝 NEXT STEPS:"
    echo "   1. Get a Telegram bot token from @BotFather"
    echo "   2. Get your Telegram user ID from @userinfobot"
    echo "   3. Edit ~/.miniclaw/config.yaml with your token and ID"
    echo "   4. Pull an Ollama model: ollama pull llama3.2:3b"
    echo "   5. Start MiniClaw: ./miniclaw -config ~/.miniclaw/config.yaml"
    echo ""
    echo "   For auto-start on boot:"
    echo "   make install && make systemd"
    echo "   sudo systemctl enable --now miniclaw"
else
    echo "✅ Config already exists at ~/.miniclaw/config.yaml"
fi

echo ""
echo "🐾 Setup complete!"
