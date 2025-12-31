# Codebase Semantic Search

> **Search your codebase using natural language** - powered by local AI, integrated with Claude Code

Ask "where do we handle authentication?" and find relevant code even if it uses terms like "login", "session", or "credentials". No external APIs, everything runs locally.

[![Go Version](https://img.shields.io/badge/Go-1.23+-00ADD8?style=flat&logo=go)](https://go.dev/)
[![License](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

---

## What is this?

A **semantic search MCP server** that integrates with [Claude Code](https://www.claude.ai/code) to search codebases by meaning, not just keywords.

### Key Features

- 🔒 **100% Local** - No external APIs, all processing on your machine
- ⚡ **Fast** - Hybrid search combining semantic similarity + exact matching
- 🔄 **Smart Indexing** - Only reprocesses changed files
- 🤖 **MCP Integration** - Works seamlessly with Claude Code

### Supported Languages

- Go (`.go`)
- Java (`.java`)
- TypeScript (`.ts`, `.tsx`)
- JavaScript (`.js`, `.jsx`, `.mjs`, `.cjs`)

---

## Installation

### Prerequisites

- **Docker & Docker Compose** - For Qdrant vector database
- **Go 1.23+** - For building the binary
- **Ollama** - Installed automatically by the installer (or install manually from https://ollama.com)

### One-Command Install

```bash
# Clone the repository
git clone https://github.com/jamaly87/codebase-semantic-search.git
cd codebase-semantic-search

# Run the installer
chmod +x install.sh
./install.sh
```

The installer will:
1. ✅ Build the MCP server binary
2. ✅ Install Ollama (native, better performance)
3. ✅ Start Qdrant (Docker container)
4. ✅ Download embedding model (274MB)
5. ✅ Configure Claude Code MCP
6. ✅ Add to your PATH

---

## Quick Start

Once installed, use it directly in Claude Code:

### 1. Index Your Codebase

```
You: "Index this codebase for semantic search"
Claude: ✅ Indexed 193 files, 1,498 chunks in 18.5s
```

### 2. Search with Natural Language

```
You: "Where do we handle JWT token validation?"
Claude: JWT validation is handled in:
        1. service/auth/JWTValidator.java:45-67
        2. service/security/TokenService.java:120-145
```

### 3. Check Status

```
You: "Is this repository indexed?"
Claude: ✅ Repository indexed. 193 files, 1,498 chunks
```

---

## How It Works

```
Claude Code
     ↓ (MCP Protocol)
Semantic Search Server (Go binary)
     ├─→ Ollama (native, embeddings)
     └─→ Qdrant (Docker, vector DB)
```

1. **Indexing**: Scans code → generates embeddings → stores in vector database
2. **Searching**: Query → embedding → finds similar code + exact matches
3. **Hybrid Scoring**: Combines semantic similarity (70%) with exact keyword matching (30%)

---

## Available MCP Tools

The server provides 4 tools to Claude Code:

| Tool | Description |
|------|-------------|
| `semantic_search` | Search code using natural language |
| `index_codebase` | Index a repository (incremental) |
| `get_index_status` | Get indexing statistics |
| `clear_cache` | Clear file hash cache |

---

## Configuration

Edit `~/.semantic-search/mcp-config.yaml` to customize:

```yaml
# Search tuning
search:
  max_results: 5          # Number of results
  semantic_weight: 0.7    # Semantic vs exact match weight
  exact_match_boost: 1.5  # Boost for exact keyword matches

# Code chunking
chunking:
  max_lines: 25           # Lines per chunk
  overlap_lines: 5        # Overlap between chunks

# Indexing
indexing:
  parallel_workers: 14    # Parallel processing
  incremental: true       # Only reprocess changed files
```

---

## Management

### Check Services

```bash
# View running services
docker-compose ps

# View logs
docker-compose logs -f

# Check MCP status
claude mcp list
```

### Stop Services

```bash
docker-compose stop
```

### Restart Services

```bash
docker-compose restart
```

### Uninstall

```bash
./uninstall.sh
```

---

## Troubleshooting

### Services Not Starting

```bash
# Check Docker is running
docker info

# Restart services
docker-compose restart

# View logs
docker-compose logs
```

### MCP Not Working

```bash
# Verify MCP configuration
claude mcp list

# Should show:
# semantic-search - ✓ Connected

# Re-configure if needed
claude mcp remove semantic-search
claude mcp add --transport stdio semantic-search --scope user -- ~/.local/bin/semantic-search
```

### Port Conflicts

**If ports 6333, 6334, or 11434 are already in use:**

```bash
# Find what's using the port
lsof -i :6334

# Stop conflicting service
docker stop <container_name>
```

### Slow Indexing

Edit `~/.semantic-search/mcp-config.yaml`:

```yaml
indexing:
  parallel_workers: 8      # Reduce from 14
embeddings:
  batch_size: 8            # Reduce from 16
```

---

## Architecture Details

### Technology Stack

- **Server**: Go 1.23+ with [mcp-go](https://github.com/mark3labs/mcp-go)
- **Vector Database**: [Qdrant](https://qdrant.tech/) (Docker container)
- **Embeddings**: [Ollama](https://ollama.ai/) (native installation) + nomic-embed-text (768 dimensions)
- **Search**: Hybrid scoring (semantic + exact match)

### Performance

| Operation | Speed |
|-----------|-------|
| First indexing (193 files) | 18.5s |
| Incremental indexing | 10ms (175x faster) |
| Search (first query) | 500ms |
| Search (cached) | 60ms |

### Memory Usage

- Ollama (native): ~400MB (optimized)
- Qdrant (Docker): ~200MB
- MCP Server: ~150MB

---

## Contributing

Contributions welcome! Please feel free to submit issues or pull requests.

---

## License

MIT License - See [LICENSE](LICENSE) for details

---
