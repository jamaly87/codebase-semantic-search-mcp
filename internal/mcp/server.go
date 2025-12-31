package mcp

import (
	"context"
	"fmt"
	"log"

	"github.com/jamaly87/codebase-semantic-search/internal/embeddings"
	"github.com/jamaly87/codebase-semantic-search/internal/indexer"
	"github.com/jamaly87/codebase-semantic-search/internal/search"
	"github.com/jamaly87/codebase-semantic-search/internal/vectordb"
	"github.com/jamaly87/codebase-semantic-search/pkg/config"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// Server represents the MCP server
type Server struct {
	config    *config.Config
	mcpServer *server.MCPServer
	indexer   *indexer.Indexer
	searcher  *search.Searcher
}

// NewServer creates a new MCP server instance
func NewServer(cfg *config.Config) (*Server, error) {
	// Create embeddings client
	embeddingsClient := embeddings.NewClient(&cfg.Embeddings)

	// Create vector database client
	vectorDB, err := vectordb.NewClient(&cfg.VectorDB)
	if err != nil {
		return nil, fmt.Errorf("failed to create vector DB client: %w", err)
	}

	// Initialize vector DB (create collection if needed)
	ctx := context.Background()
	if err := vectorDB.Initialize(ctx); err != nil {
		return nil, fmt.Errorf("failed to initialize vector DB: %w", err)
	}

	// Create indexer
	idx, err := indexer.NewIndexer(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create indexer: %w", err)
	}

	// Create searcher
	searcher := search.NewSearcher(&cfg.Search, embeddingsClient, vectorDB)

	s := &Server{
		config:   cfg,
		indexer:  idx,
		searcher: searcher,
	}

	// Create MCP server
	mcpServer := server.NewMCPServer(
		cfg.Server.Name,
		cfg.Server.Version,
	)

	// Register tools
	tools := s.getTools()
	for _, tool := range tools {
		mcpServer.AddTool(tool, s.createToolHandler(tool.Name))
	}

	s.mcpServer = mcpServer

	log.Printf("MCP server initialized: %s v%s", cfg.Server.Name, cfg.Server.Version)
	log.Printf("Registered %d tools", len(tools))

	return s, nil
}

// createToolHandler creates a handler function for a given tool name
func (s *Server) createToolHandler(toolName string) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		log.Printf("Handling tool call: %s", toolName)

		// Extract and type assert arguments from request
		var args map[string]interface{}
		if request.Params.Arguments != nil {
			var ok bool
			args, ok = request.Params.Arguments.(map[string]interface{})
			if !ok {
				return errorResult("invalid arguments format"), nil
			}
		} else {
			args = make(map[string]interface{})
		}

		// Route to appropriate handler based on tool name
		switch toolName {
		case "semantic_search":
			return s.handleSemanticSearch(ctx, args)
		case "index_codebase":
			return s.handleIndexCodebase(ctx, args)
		case "clear_cache":
			return s.handleClearCache(ctx, args)
		case "get_index_status":
			return s.handleGetIndexStatus(ctx, args)
		default:
			return errorResult(fmt.Sprintf("unknown tool: %s", toolName)), nil
		}
	}
}

// Start starts the MCP server with stdio transport
func (s *Server) Start(ctx context.Context) error {
	log.Printf("Starting MCP server on stdio transport...")

	// Start the server with stdio transport
	if err := server.ServeStdio(s.mcpServer); err != nil {
		return fmt.Errorf("server error: %w", err)
	}

	return nil
}

// Close closes the server and cleans up resources
func (s *Server) Close() error {
	log.Printf("Shutting down MCP server...")
	// TODO: Close connections to Qdrant, cleanup resources
	return nil
}
