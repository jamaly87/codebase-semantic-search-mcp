#!/bin/bash
# Semantic Search MCP Server - Uninstallation Script
# Safely removes all components of the semantic search server

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

echo -e "${RED}╔═══════════════════════════════════════════════╗${NC}"
echo -e "${RED}║   Semantic Search MCP Server Uninstallation  ║${NC}"
echo -e "${RED}╚═══════════════════════════════════════════════╝${NC}"
echo ""

# Confirmation prompt
confirm_uninstall() {
    echo -e "${YELLOW}⚠️  This will remove:${NC}"
    echo "   • Binary: $INSTALL_DIR/$BINARY_NAME"
    echo "   • Config: $CONFIG_DIR"
    echo "   • Docker containers: semantic-search-ollama, semantic-search-qdrant"
    echo "   • Claude Code MCP configuration"
    echo ""
    read -p "Continue with uninstallation? (y/N): " -n 1 -r
    echo ""
    if [[ ! $REPLY =~ ^[Yy]$ ]]; then
        echo "Uninstallation cancelled."
        exit 0
    fi
    echo ""
}

# Stop Docker services
stop_services() {
    echo -e "${BLUE}🐳 Stopping Docker services...${NC}"

    local stopped=false

    # Try to find and use docker-compose.yml from common locations
    local compose_locations=(
        "$CONFIG_DIR/docker-compose.yml"
        "docker-compose.yml"
        "$PWD/docker-compose.yml"
    )

    for compose_file in "${compose_locations[@]}"; do
        if [ -f "$compose_file" ]; then
            local compose_dir=$(dirname "$compose_file")
            echo "   Trying docker-compose in $compose_dir..."
            if (cd "$compose_dir" && docker-compose down 2>/dev/null); then
                echo -e "${GREEN}   ✓ Stopped containers via docker-compose${NC}"
                stopped=true
                break
            fi
        fi
    done

    # If docker-compose didn't work, try manual stop
    if [ "$stopped" = false ]; then
        echo -e "${YELLOW}   ⚠ docker-compose not found, stopping containers manually...${NC}"
        stop_containers_manually
    fi

    echo ""
}

# Manually stop containers
stop_containers_manually() {
    local containers_found=false

    # Only stop Qdrant container (Ollama runs natively)
    for container in semantic-search-qdrant; do
        if docker ps -a --format '{{.Names}}' | grep -q "^${container}$"; then
            containers_found=true
            echo "   Stopping $container..."
            docker stop "$container" 2>/dev/null || true
            docker rm "$container" 2>/dev/null || true
            echo -e "${GREEN}   ✓ Removed container: $container${NC}"
        fi
    done

    if [ "$containers_found" = false ]; then
        echo -e "${YELLOW}   ⊘ No containers found${NC}"
    fi
}

# Stop native Ollama service
stop_ollama() {
    echo -e "${BLUE}🤖 Stopping Ollama service...${NC}"

    # Check if Ollama is running
    if pgrep -x "ollama" > /dev/null; then
        echo "   Stopping Ollama process..."
        pkill -x "ollama" 2>/dev/null || true
        sleep 2
        echo -e "${GREEN}   ✓ Ollama service stopped${NC}"
    else
        echo -e "${YELLOW}   ⊘ Ollama not running${NC}"
    fi

    echo ""
}

# Remove Docker volumes
remove_volumes() {
    echo -e "${BLUE}🗑️  Removing Docker volumes...${NC}"

    echo -e "${YELLOW}⚠️  This will delete all indexed data and downloaded models.${NC}"
    read -p "Remove Docker volumes? (y/N): " -n 1 -r
    echo ""

    if [[ $REPLY =~ ^[Yy]$ ]]; then
        local volumes_found=false

        # Find and remove volumes
        for volume in $(docker volume ls -q | grep "semantic-search" 2>/dev/null); do
            volumes_found=true
            docker volume rm "$volume" 2>/dev/null || true
            echo -e "${GREEN}   ✓ Removed volume: $volume${NC}"
        done

        # Also check for codebase-semantic-search prefix
        for volume in $(docker volume ls -q | grep "codebase-semantic-search" 2>/dev/null); do
            volumes_found=true
            docker volume rm "$volume" 2>/dev/null || true
            echo -e "${GREEN}   ✓ Removed volume: $volume${NC}"
        done

        if [ "$volumes_found" = true ]; then
            echo -e "${GREEN}   ✓ Docker volumes removed${NC}"
        else
            echo -e "${YELLOW}   ⊘ No volumes found${NC}"
        fi
    else
        echo -e "${YELLOW}   ⊘ Skipped volume removal${NC}"
    fi

    echo ""
}

# Remove Docker images
remove_images() {
    echo -e "${BLUE}🗑️  Removing Docker images...${NC}"

    # Check if Qdrant image exists
    local qdrant_exists=$(docker images -q qdrant/qdrant 2>/dev/null)

    if [ -z "$qdrant_exists" ]; then
        echo -e "${YELLOW}   ⊘ No Qdrant Docker image found${NC}"
        echo ""
        return
    fi

    echo -e "${YELLOW}⚠️  This will remove Qdrant Docker image (you can re-download it later).${NC}"
    echo "   • qdrant/qdrant (~200MB)"
    echo ""
    read -p "Remove Docker image? (y/N): " -n 1 -r
    echo ""

    if [[ $REPLY =~ ^[Yy]$ ]]; then
        docker rmi qdrant/qdrant 2>/dev/null || true
        echo -e "${GREEN}   ✓ Removed qdrant/qdrant${NC}"
    else
        echo -e "${YELLOW}   ⊘ Skipped image removal${NC}"
    fi

    echo ""
}

# Remove binary
remove_binary() {
    echo -e "${BLUE}🗑️  Removing binary...${NC}"

    if [ -f "$INSTALL_DIR/$BINARY_NAME" ]; then
        rm -f "$INSTALL_DIR/$BINARY_NAME"
        echo -e "${GREEN}   ✓ Removed $INSTALL_DIR/$BINARY_NAME${NC}"
    else
        echo -e "${YELLOW}   ⊘ Binary not found${NC}"
    fi

    echo ""
}

# Remove configuration
remove_config() {
    echo -e "${BLUE}🗑️  Removing configuration...${NC}"

    if [ -d "$CONFIG_DIR" ]; then
        rm -rf "$CONFIG_DIR"
        echo -e "${GREEN}   ✓ Removed $CONFIG_DIR${NC}"
    else
        echo -e "${YELLOW}   ⊘ Config directory not found${NC}"
    fi

    echo ""
}

# Remove Claude Code MCP configuration
remove_mcp_config() {
    echo -e "${BLUE}🗑️  Removing Claude Code MCP configuration...${NC}"

    if ! command -v claude &> /dev/null; then
        echo -e "${YELLOW}   ⊘ Claude Code CLI not found${NC}"
        echo ""
        return
    fi

    if claude mcp list 2>/dev/null | grep -q "semantic-search"; then
        if claude mcp remove semantic-search 2>/dev/null; then
            echo -e "${GREEN}   ✓ Removed MCP server 'semantic-search'${NC}"
        else
            echo -e "${YELLOW}   ⚠ Failed to remove MCP config (may need manual removal)${NC}"
        fi
    else
        echo -e "${YELLOW}   ⊘ MCP server 'semantic-search' not configured${NC}"
    fi

    echo ""
}

# Clean up PATH (optional)
cleanup_path() {
    echo -e "${BLUE}🔗 Cleaning up PATH...${NC}"

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

    # Check if shell config has the PATH entry
    if [ -f "$shell_rc" ] && grep -q "$INSTALL_DIR" "$shell_rc"; then
        read -p "Remove $INSTALL_DIR from PATH in $shell_rc? (y/N): " -n 1 -r
        echo ""

        if [[ $REPLY =~ ^[Yy]$ ]]; then
            # Remove the PATH line
            if [[ "$OSTYPE" == "darwin"* ]]; then
                # macOS
                sed -i '' "\|$INSTALL_DIR|d" "$shell_rc"
            else
                # Linux
                sed -i "\|$INSTALL_DIR|d" "$shell_rc"
            fi
            echo -e "${GREEN}   ✓ Removed from PATH in $shell_rc${NC}"
            echo -e "${YELLOW}   Run: source $shell_rc${NC}"
        else
            echo -e "${YELLOW}   ⊘ Skipped PATH cleanup${NC}"
        fi
    else
        echo -e "${YELLOW}   ⊘ No PATH entry found in $shell_rc${NC}"
    fi

    echo ""
}

# Remove cache directory
remove_cache() {
    echo -e "${BLUE}🗑️  Removing cache...${NC}"

    local cache_dir="$HOME/.cache/semantic-search"
    if [ -d "$cache_dir" ]; then
        rm -rf "$cache_dir"
        echo -e "${GREEN}   ✓ Removed cache directory${NC}"
    else
        echo -e "${YELLOW}   ⊘ No cache directory found${NC}"
    fi

    echo ""
}

# Verify cleanup
verify_cleanup() {
    echo -e "${BLUE}✅ Verifying cleanup...${NC}"

    local all_clean=true

    if [ -f "$INSTALL_DIR/$BINARY_NAME" ]; then
        echo -e "${YELLOW}   ⚠ Binary still exists${NC}"
        all_clean=false
    fi

    if [ -d "$CONFIG_DIR" ]; then
        echo -e "${YELLOW}   ⚠ Config directory still exists${NC}"
        all_clean=false
    fi

    if docker ps -a --format '{{.Names}}' | grep -q "semantic-search"; then
        echo -e "${YELLOW}   ⚠ Docker containers still exist${NC}"
        all_clean=false
    fi

    if $all_clean; then
        echo -e "${GREEN}   ✓ All components removed${NC}"
    fi

    echo ""
}

# Print completion message
print_completion() {
    echo -e "${GREEN}╔═══════════════════════════════════════════════╗${NC}"
    echo -e "${GREEN}║       Uninstallation Complete! 👋            ║${NC}"
    echo -e "${GREEN}╚═══════════════════════════════════════════════╝${NC}"
    echo ""
    echo -e "${BLUE}📋 What was removed:${NC}"
    echo "   ✓ Semantic search binary"
    echo "   ✓ Configuration files"
    echo "   ✓ Docker containers"
    echo "   ✓ Claude Code MCP configuration"
    echo ""
    echo -e "${BLUE}💡 Note:${NC}"
    echo "   • Docker volumes may have been preserved (contains indexed data)"
    echo "   • To remove all volumes manually:"
    echo -e "     ${YELLOW}docker volume ls | grep semantic-search${NC}"
    echo -e "     ${YELLOW}docker volume rm <volume_name>${NC}"
    echo ""
    echo -e "${BLUE}📚 Thanks for using Semantic Search!${NC}"
    echo "   Feedback: https://github.com/jamaly87/codebase-semantic-search/issues"
    echo ""
}

# Main uninstallation flow
main() {
    confirm_uninstall
    stop_services
    stop_ollama
    remove_volumes
    remove_images
    remove_binary
    remove_config
    remove_mcp_config
    cleanup_path
    remove_cache
    verify_cleanup
    print_completion
}

# Run uninstallation
main