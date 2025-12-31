# Setup Guide - Semantic Search Server

This guide provides detailed setup instructions for the semantic search server.

## Quick Setup (Recommended)

**Use the automated installer:**

```bash
git clone https://github.com/jamaly87/codebase-semantic-search.git
cd codebase-semantic-search
chmod +x install.sh
./install.sh
```

The installer handles everything automatically. See the main [README.md](README.md) for details.

---

## Manual Setup

If you prefer to set up components manually, follow these steps:

### Prerequisites

- **Go 1.23+** - For building the server
- **Docker & Docker Compose** - For running services
- **Claude Code CLI** (optional) - For MCP integration

### 1. Clone Repository

```bash
git clone https://github.com/jamaly87/codebase-semantic-search.git
cd codebase-semantic-search
```

### 2. Build Binary

```bash
# Build the MCP server
go build -o semantic-search ./cmd/server

# Build test tool (optional)
go build -o test-search ./cmd/search-test

# Install to PATH
mkdir -p ~/.local/bin
cp semantic-search ~/.local/bin/
chmod +x ~/.local/bin/semantic-search
```

### 3. Start Services with Docker Compose

```bash
# Start Ollama and Qdrant
docker-compose up -d

# Verify services are running
docker-compose ps

# Should show:
# semantic-search-ollama    Up (healthy)
# semantic-search-qdrant    Up (healthy)
```

### 4. Download Embedding Model

```bash
# Wait for Ollama to be ready
curl http://localhost:11434/api/tags

# Pull the embedding model (274MB)
docker exec semantic-search-ollama ollama pull nomic-embed-text

# Verify
docker exec semantic-search-ollama ollama list
```

### 5. Configure Claude Code MCP

```bash
# Add MCP server
claude mcp add --transport stdio semantic-search --scope user -- ~/.local/bin/semantic-search

# Verify
claude mcp list
# Should show: semantic-search - ✓ Connected
```

### 6. Add to PATH (Optional)

```bash
# For bash
echo 'export PATH="$PATH:~/.local/bin"' >> ~/.bashrc
source ~/.bashrc

# For zsh
echo 'export PATH="$PATH:~/.local/bin"' >> ~/.zshrc
source ~/.zshrc
```

---

## Configuration

### Config File Location

After installation, configuration is at:
```
~/.semantic-search/mcp-config.yaml
```

### Default Configuration

```yaml
# Code chunking
chunking:
  max_lines: 25           # Lines per chunk
  overlap_lines: 5        # Overlap between chunks

# Search tuning
search:
  max_results: 5          # Number of results to return
  semantic_weight: 0.7    # Semantic similarity weight (0-1)
  exact_match_boost: 1.5  # Boost multiplier for exact matches

# Embeddings
embeddings:
  model: "nomic-embed-text"
  ollama_url: "http://localhost:11434"
  batch_size: 16          # Embeddings per batch
  dimensions: 768

# Vector database
vectordb:
  type: "qdrant"
  host: "localhost"
  port: 6334              # gRPC port
  collection_name: "code_chunks"

# Indexing
indexing:
  parallel_workers: 14    # Parallel processing
  incremental: true       # Enable file hash tracking
  max_file_size_mb: 5     # Skip files larger than this
```

### Environment Variables

Override config with environment variables:

```bash
export OLLAMA_URL="http://localhost:11434"
export QDRANT_HOST="localhost"
export QDRANT_PORT="6334"
```

---

## Service Management

### Check Status

```bash
# View running services
docker-compose ps

# View logs
docker-compose logs -f

# Check specific service
docker-compose logs ollama
docker-compose logs qdrant
```

### Stop Services

```bash
# Stop all services
docker-compose stop

# Stop specific service
docker-compose stop qdrant
```

### Restart Services

```bash
# Restart all
docker-compose restart

# Restart specific service
docker-compose restart ollama
```

### Remove Everything

```bash
# Stop and remove containers
docker-compose down

# Also remove volumes (deletes indexed data)
docker-compose down -v
```

---

## Troubleshooting

### Services Not Starting

**Check Docker is running:**
```bash
docker info
```

**View service logs:**
```bash
docker-compose logs
```

**Restart services:**
```bash
docker-compose restart
```

### Port Conflicts

If ports 6333, 6334, or 11434 are in use:

```bash
# Find what's using the port
lsof -i :6334

# Stop conflicting service
docker stop <container_name>
```

### Ollama Model Issues

**Model not found:**
```bash
# Pull model again
docker exec semantic-search-ollama ollama pull nomic-embed-text

# List available models
docker exec semantic-search-ollama ollama list
```

**Ollama not responding:**
```bash
# Check Ollama health
curl http://localhost:11434/api/tags

# Restart Ollama container
docker-compose restart ollama

# View logs
docker-compose logs ollama
```

### Qdrant Issues

**Connection failed:**
```bash
# Check Qdrant health
curl http://localhost:6333/health

# Restart Qdrant
docker-compose restart qdrant

# View logs
docker-compose logs qdrant
```

**Data persistence:**
```bash
# Qdrant data is stored in Docker volume: qdrant_data
# To view volume info:
docker volume inspect codebase-semantic-search-server_qdrant_data
```

### Performance Issues

**Slow indexing:**

Edit `~/.semantic-search/mcp-config.yaml`:
```yaml
indexing:
  parallel_workers: 8      # Reduce from 14
embeddings:
  batch_size: 8            # Reduce from 16
```

**Out of memory:**

Increase Docker memory:
- Docker Desktop → Settings → Resources → Memory
- Set to 8GB minimum

**High CPU usage:**
```yaml
indexing:
  parallel_workers: 4      # Reduce parallelism
```

---

## Verification

### Test MCP Server

```bash
# Check binary exists
which semantic-search
# Should output: /Users/username/.local/bin/semantic-search

# Check it's executable
ls -l ~/.local/bin/semantic-search
# Should show: -rwxr-xr-x
```

### Test Services

```bash
# Test Ollama
curl http://localhost:11434/api/tags

# Test Qdrant
curl http://localhost:6333/health

# Should both return JSON responses
```

### Test MCP Integration

```bash
# Verify MCP configuration
claude mcp list

# Should show:
# semantic-search - ✓ Connected
```

---

## Performance Expectations

### Typical Performance (M1/M2 Mac, 8GB RAM)

**Small repository (200 files, 1,500 chunks):**
- First indexing: 15-20 seconds
- Incremental: 10ms (cached)
- Search: 60-500ms

**Large repository (1000 files, 10K chunks):**
- First indexing: 10-15 minutes
- Incremental: 100ms
- Search: 100-800ms

### Memory Usage

- Ollama: ~800MB
- Qdrant: ~200MB
- MCP Server: ~150MB
- Total: ~1.2GB

---

## Uninstallation

To completely remove the semantic search server:

```bash
./uninstall.sh
```

This will:
- Stop and remove Docker containers
- Remove the binary from `~/.local/bin`
- Remove configuration from `~/.semantic-search`
- Remove Claude Code MCP configuration
- Optionally remove Docker volumes (indexed data)

---

## Resources

- **Main Documentation**: [README.md](README.md)
- **Ollama**: https://ollama.com
- **Qdrant**: https://qdrant.tech
- **nomic-embed-text**: https://huggingface.co/nomic-ai/nomic-embed-text-v1
- **Claude Code**: https://www.claude.ai/code

---

## Getting Help

- **Issues**: https://github.com/jamaly87/codebase-semantic-search/issues
- **Discussions**: https://github.com/jamaly87/codebase-semantic-search/discussions
