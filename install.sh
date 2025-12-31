#!/bin/bash
# Semantic Search MCP Server - Local Installation Script
# Run this script from the cloned repository directory
# Usage: ./install.sh

set -e

# Color codes
GREEN='\033[0;32m'
BLUE='\033[0;34m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m' # No Color

# Configuration
BINARY_NAME="semantic-search"
INSTALL_DIR="$HOME/.local/bin"
CONFIG_DIR="$HOME/.semantic-search"

echo -e "${BLUE}╔═══════════════════════════════════════════════╗${NC}"
echo -e "${BLUE}║   Semantic Search MCP Server Installation    ║${NC}"
echo -e "${BLUE}╚═══════════════════════════════════════════════╝${NC}"
echo ""

# Check if running from repository root
check_repository() {
    if [ ! -f "go.mod" ] || [ ! -f "docker-compose.yml" ]; then
        echo -e "${RED}❌ Error: This script must be run from the repository root${NC}"
        echo ""
        echo "Please run:"
        echo "  cd /path/to/codebase-semantic-search"
        echo "  ./install.sh"
        exit 1
    fi
}

# Check prerequisites
check_prerequisites() {
    echo -e "${BLUE}📋 Checking prerequisites...${NC}"

    # Check Go
    if ! command -v go &> /dev/null; then
        echo -e "${RED}❌ Go not found${NC}"
        echo "   Install Go 1.23+: https://go.dev/dl/"
        exit 1
    fi

    local go_version=$(go version | awk '{print $3}' | sed 's/go//')
    echo -e "${GREEN}   ✓ Go $go_version${NC}"

    # Check Docker
    if ! command -v docker &> /dev/null; then
        echo -e "${RED}❌ Docker not found${NC}"
        echo "   Install Docker: https://docs.docker.com/get-docker/"
        exit 1
    fi

    if ! docker info &> /dev/null; then
        echo -e "${RED}❌ Docker not running${NC}"
        echo "   Please start Docker Desktop"
        exit 1
    fi
    echo -e "${GREEN}   ✓ Docker${NC}"

    # Check docker-compose
    if ! command -v docker-compose &> /dev/null && ! docker compose version &> /dev/null; then
        echo -e "${RED}❌ docker-compose not found${NC}"
        echo "   Install docker-compose: https://docs.docker.com/compose/install/"
        exit 1
    fi
    echo -e "${GREEN}   ✓ docker-compose${NC}"

    echo ""
}

# Create installation directories
setup_directories() {
    echo -e "${BLUE}📁 Setting up directories...${NC}"
    mkdir -p "$INSTALL_DIR"
    mkdir -p "$CONFIG_DIR"
    echo -e "${GREEN}   ✓ Created $INSTALL_DIR${NC}"
    echo -e "${GREEN}   ✓ Created $CONFIG_DIR${NC}"
    echo ""
}

# Build binary from source
build_binary() {
    echo -e "${BLUE}🔨 Building binary from source...${NC}"

    echo "   Running: go build -o $INSTALL_DIR/$BINARY_NAME ./cmd/server"
    go build -o "$INSTALL_DIR/$BINARY_NAME" ./cmd/server

    chmod +x "$INSTALL_DIR/$BINARY_NAME"

    local version=$("$INSTALL_DIR/$BINARY_NAME" --version 2>/dev/null || echo "development")
    echo -e "${GREEN}   ✓ Built $BINARY_NAME${NC}"
    echo ""
}

# Copy configuration files
setup_config() {
    echo -e "${BLUE}⚙️  Setting up configuration...${NC}"

    # Copy config files
    if [ -f "mcp-config.yaml" ]; then
        cp mcp-config.yaml "$CONFIG_DIR/"
        echo -e "${GREEN}   ✓ Copied mcp-config.yaml${NC}"
    fi

    if [ -f "docker-compose.yml" ]; then
        cp docker-compose.yml "$CONFIG_DIR/"
        echo -e "${GREEN}   ✓ Copied docker-compose.yml${NC}"
    fi

    echo ""
}

# Start Docker services
start_services() {
    echo -e "${BLUE}🐳 Starting Docker services...${NC}"
    echo "   Starting Qdrant container..."

    docker-compose up -d

    echo "   Waiting for Qdrant to be healthy..."
    local retries=30
    local count=0

    while [ $count -lt $retries ]; do
        if curl -s http://localhost:6333/health &> /dev/null; then
            echo -e "${GREEN}   ✓ Qdrant healthy${NC}"
            break
        fi
        sleep 1
        count=$((count + 1))
        printf "."
    done

    echo ""

    if [ $count -eq $retries ]; then
        echo -e "${YELLOW}   ⚠ Qdrant started but health check timeout${NC}"
        echo "   Check status: docker-compose ps"
        echo ""
    fi
}

# Setup Ollama (native installation)
setup_ollama() {
    echo -e "${BLUE}🤖 Setting up Ollama...${NC}"

    # Check if Ollama is installed
    if ! command -v ollama &> /dev/null; then
        echo -e "${YELLOW}   Ollama not found, installing...${NC}"

        # Detect OS and install
        case "$(uname -s)" in
            Darwin*)
                echo "   Installing Ollama for macOS..."
                curl -fsSL https://ollama.com/install.sh | sh
                ;;
            Linux*)
                echo "   Installing Ollama for Linux..."
                curl -fsSL https://ollama.com/install.sh | sh
                ;;
            *)
                echo -e "${RED}   ✗ Unsupported OS for automatic Ollama installation${NC}"
                echo "   Please install manually: https://ollama.com/download"
                return 1
                ;;
        esac

        echo -e "${GREEN}   ✓ Ollama installed${NC}"
    else
        echo -e "${GREEN}   ✓ Ollama already installed${NC}"
    fi

    # Start Ollama service if not running
    if ! curl -s http://localhost:11434/api/tags &> /dev/null; then
        echo "   Starting Ollama service..."

        # Start Ollama in background
        nohup ollama serve > /tmp/ollama.log 2>&1 &

        # Wait for Ollama to be ready
        echo "   Waiting for Ollama to start..."
        local retries=30
        local count=0
        while [ $count -lt $retries ]; do
            if curl -s http://localhost:11434/api/tags &> /dev/null; then
                echo -e "${GREEN}   ✓ Ollama service started${NC}"
                break
            fi
            sleep 1
            count=$((count + 1))
        done

        if [ $count -eq $retries ]; then
            echo -e "${YELLOW}   ⚠ Ollama service startup timeout${NC}"
        fi
    else
        echo -e "${GREEN}   ✓ Ollama service already running${NC}"
    fi

    # Pull embedding model
    echo "   Checking for nomic-embed-text model..."
    if ollama list | grep -q "nomic-embed-text"; then
        echo -e "${GREEN}   ✓ Model nomic-embed-text already exists${NC}"
    else
        echo "   Pulling nomic-embed-text model (274MB)..."
        echo "   This may take a few minutes..."
        ollama pull nomic-embed-text
        echo -e "${GREEN}   ✓ Model downloaded${NC}"
    fi

    echo ""
}

# Configure Claude Code MCP
configure_claude_mcp() {
    echo -e "${BLUE}🔧 Configuring Claude Code MCP...${NC}"

    # Check if Claude Code is installed
    if ! command -v claude &> /dev/null; then
        echo -e "${YELLOW}   ⚠ Claude Code CLI not found${NC}"
        echo ""
        echo "   To configure manually later, run:"
        echo -e "   ${YELLOW}claude mcp add --transport stdio semantic-search --scope user -- $INSTALL_DIR/$BINARY_NAME${NC}"
        echo ""
        return
    fi

    # Check if MCP server already exists
    if claude mcp list 2>/dev/null | grep -q "semantic-search"; then
        echo -e "${YELLOW}   ⚠ MCP server 'semantic-search' already configured${NC}"
        echo "   Remove old config: ${YELLOW}claude mcp remove semantic-search${NC}"
        echo "   Then re-run this script"
    else
        # Add MCP server
        if claude mcp add --transport stdio semantic-search --scope user -- "$INSTALL_DIR/$BINARY_NAME"; then
            echo -e "${GREEN}   ✓ MCP server configured${NC}"
        else
            echo -e "${RED}   ✗ Failed to configure MCP${NC}"
            echo "   Try manually: ${YELLOW}claude mcp add --transport stdio semantic-search --scope user -- $INSTALL_DIR/$BINARY_NAME${NC}"
        fi
    fi

    echo ""
}

# Add to PATH
update_path() {
    echo -e "${BLUE}🔗 Updating PATH...${NC}"

    # Detect user's default shell
    local user_shell=$(basename "$SHELL")
    local shell_rc=""

    case "$user_shell" in
        bash)
            # Check for bash_profile first (macOS), then bashrc (Linux)
            if [ -f "$HOME/.bash_profile" ]; then
                shell_rc="$HOME/.bash_profile"
            elif [ -f "$HOME/.bashrc" ]; then
                shell_rc="$HOME/.bashrc"
            else
                shell_rc="$HOME/.profile"
            fi
            ;;
        zsh)
            shell_rc="$HOME/.zshrc"
            ;;
        fish)
            shell_rc="$HOME/.config/fish/config.fish"
            ;;
        *)
            shell_rc="$HOME/.profile"
            ;;
    esac

    # Create shell rc file if it doesn't exist
    if [ ! -f "$shell_rc" ]; then
        touch "$shell_rc"
        echo -e "${YELLOW}   Created $shell_rc${NC}"
    fi

    # Check if PATH already contains install directory
    if echo "$PATH" | grep -q "$INSTALL_DIR"; then
        echo -e "${GREEN}   ✓ $INSTALL_DIR already in PATH${NC}"
    else
        echo "export PATH=\"\$PATH:$INSTALL_DIR\"" >> "$shell_rc"
        echo -e "${GREEN}   ✓ Added $INSTALL_DIR to PATH in $shell_rc${NC}"
        echo -e "${YELLOW}   Run: source $shell_rc${NC}"
        export PATH="$PATH:$INSTALL_DIR"
    fi

    echo ""
}

# Verify installation
verify_installation() {
    echo -e "${BLUE}✅ Verifying installation...${NC}"

    # Check binary
    if [ -x "$INSTALL_DIR/$BINARY_NAME" ]; then
        echo -e "${GREEN}   ✓ Binary installed at $INSTALL_DIR/$BINARY_NAME${NC}"
    else
        echo -e "${RED}   ✗ Binary not found${NC}"
        return 1
    fi

    # Check services
    if curl -s http://localhost:6333/health &> /dev/null; then
        echo -e "${GREEN}   ✓ Qdrant running${NC}"
    else
        echo -e "${YELLOW}   ⚠ Qdrant not responding${NC}"
    fi

    if curl -s http://localhost:11434/api/tags &> /dev/null; then
        echo -e "${GREEN}   ✓ Ollama running${NC}"
    else
        echo -e "${YELLOW}   ⚠ Ollama not responding${NC}"
    fi

    echo ""
}

# Print success message
print_success() {
    echo -e "${GREEN}╔═══════════════════════════════════════════════╗${NC}"
    echo -e "${GREEN}║        Installation Complete! 🎉              ║${NC}"
    echo -e "${GREEN}╚═══════════════════════════════════════════════╝${NC}"
    echo ""
    echo -e "${BLUE}📊 Services Running:${NC}"
    echo "   • Qdrant (Vector DB):  http://localhost:6333"
    echo "   • Ollama (Embeddings): http://localhost:11434"
    echo ""
    echo -e "${BLUE}🚀 Quick Start:${NC}"
    echo "   1. Open Claude Code CLI:"
    echo -e "      ${YELLOW}claude code${NC}"
    echo ""
    echo "   2. Navigate to your project:"
    echo -e "      ${YELLOW}cd /path/to/your/project${NC}"
    echo ""
    echo "   3. Index the codebase:"
    echo -e "      ${YELLOW}Ask Claude: \"Index this codebase for semantic search\"${NC}"
    echo ""
    echo "   4. Search your code:"
    echo -e "      ${YELLOW}Ask Claude: \"Where do we handle authentication?\"${NC}"
    echo ""
    echo -e "${BLUE}📝 Useful Commands:${NC}"
    echo "   • Check MCP status:"
    echo -e "     ${YELLOW}claude mcp list${NC}"
    echo ""
    echo "   • View running services:"
    echo -e "     ${YELLOW}docker-compose ps${NC}"
    echo ""
    echo "   • View service logs:"
    echo -e "     ${YELLOW}docker-compose logs -f${NC}"
    echo ""
    echo "   • Stop services:"
    echo -e "     ${YELLOW}docker-compose stop${NC}"
    echo ""
    echo "   • Restart services:"
    echo -e "     ${YELLOW}docker-compose restart${NC}"
    echo ""
    echo "   • Uninstall everything:"
    echo -e "     ${YELLOW}./uninstall.sh${NC}"
    echo ""
    echo -e "${BLUE}📚 Documentation:${NC}"
    echo "   https://github.com/jamaly87/codebase-semantic-search"
    echo ""
    echo -e "${YELLOW}💡 TIP: Services will auto-start on system boot${NC}"
    echo ""
}

# Main installation flow
main() {
    check_repository
    check_prerequisites
    setup_directories
    build_binary
    setup_config
    setup_ollama
    start_services
    configure_claude_mcp
    update_path
    verify_installation
    print_success
}

# Run installation
main