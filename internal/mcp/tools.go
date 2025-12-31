package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/jamaly87/codebase-semantic-search/internal/search"
	"github.com/mark3labs/mcp-go/mcp"
)

// Tool definitions for the MCP server
func (s *Server) getTools() []mcp.Tool {
	return []mcp.Tool{
		{
			Name:        "semantic_search",
			Description: "Search for code in a repository using natural language queries. Use this tool when the user asks questions like 'where is...', 'find...', 'show me...', 'how do we...', or any question about locating specific code, functions, classes, or logic in the codebase. Returns ranked code matches with exact file locations, line numbers, and relevance scores. Works with semantic understanding (e.g., 'authentication logic' finds auth-related code even without exact keyword matches).",
			InputSchema: mcp.ToolInputSchema{
				Type: "object",
				Properties: map[string]interface{}{
					"query": map[string]interface{}{
						"type":        "string",
						"description": "Natural language search query describing what code to find. Examples: 'JWT token validation', 'CSV file parsing', 'database connection setup', 'user authentication logic', 'error handling for API requests'. Can be short phrases or questions.",
					},
					"repo_path": map[string]interface{}{
						"type":        "string",
						"description": "Absolute path to the repository to search",
					},
					"limit": map[string]interface{}{
						"type":        "number",
						"description": "Maximum number of results to return (default: 5)",
						"default":     5,
					},
					"chunk_type": map[string]interface{}{
						"type":        "string",
						"description": "Type of chunks to search: 'function', 'file', or 'all' (default: 'all')",
						"enum":        []string{"function", "file", "all"},
						"default":     "all",
					},
				},
				Required: []string{"query", "repo_path"},
			},
		},
		{
			Name:        "index_codebase",
			Description: "Index a code repository to enable semantic search. Use this tool when: (1) First time working with a new repository, (2) User explicitly asks to 'index', 'scan', or 'prepare' a codebase, (3) Before the first search query on a repository. This scans all code files, breaks them into chunks, generates embeddings using the local LLM, and stores them in the vector database. Supports incremental indexing (only reprocesses changed files). Required before semantic_search can work on a repository.",
			InputSchema: mcp.ToolInputSchema{
				Type: "object",
				Properties: map[string]interface{}{
					"repo_path": map[string]interface{}{
						"type":        "string",
						"description": "Absolute path to the repository to index",
					},
					"force_reindex": map[string]interface{}{
						"type":        "boolean",
						"description": "Force full reindex even if repository is already indexed (default: false)",
						"default":     false,
					},
				},
				Required: []string{"repo_path"},
			},
		},
		{
			Name:        "clear_cache",
			Description: "Clear the index cache for a repository. Use this tool when: (1) User reports incorrect or stale search results, (2) Repository structure has changed significantly (files moved/renamed), (3) User explicitly asks to 'clear cache', 'reset index', or 'start fresh', (4) Debugging indexing issues. After clearing cache, the repository must be reindexed using index_codebase.",
			InputSchema: mcp.ToolInputSchema{
				Type: "object",
				Properties: map[string]interface{}{
					"repo_path": map[string]interface{}{
						"type":        "string",
						"description": "Absolute path to the repository whose cache should be cleared",
					},
				},
				Required: []string{"repo_path"},
			},
		},
		{
			Name:        "get_index_status",
			Description: "Get indexing status and statistics for a repository. Use this tool when: (1) User asks if a repository is indexed or 'is this repo ready?', (2) User asks 'how many files are indexed?', (3) Checking if indexing is needed before a search, (4) User asks about index freshness or 'when was this indexed?'. Returns: total files indexed, number of code chunks, last index timestamp, and repository status.",
			InputSchema: mcp.ToolInputSchema{
				Type: "object",
				Properties: map[string]interface{}{
					"repo_path": map[string]interface{}{
						"type":        "string",
						"description": "Absolute path to the repository",
					},
				},
				Required: []string{"repo_path"},
			},
		},
	}
}

// Tool handlers

func (s *Server) handleSemanticSearch(ctx context.Context, args map[string]interface{}) (*mcp.CallToolResult, error) {
	// Extract arguments
	query, ok := args["query"].(string)
	if !ok || query == "" {
		return errorResult("query is required and must be a string"), nil
	}

	repoPath, ok := args["repo_path"].(string)
	if !ok || repoPath == "" {
		return errorResult("repo_path is required and must be a string"), nil
	}

	// Note: limit is not used here - searcher uses config.Search.MaxResults
	// chunk_type filtering can be added in future enhancement

	// Perform semantic search
	results, err := s.searcher.Search(ctx, query, repoPath)
	if err != nil {
		return errorResult(fmt.Sprintf("search failed: %v", err)), nil
	}

	// Format results for display
	formattedResults := formatSearchResults(results)

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			mcp.TextContent{
				Type: "text",
				Text: formattedResults,
			},
		},
	}, nil
}

func (s *Server) handleIndexCodebase(ctx context.Context, args map[string]interface{}) (*mcp.CallToolResult, error) {
	repoPath, ok := args["repo_path"].(string)
	if !ok || repoPath == "" {
		return errorResult("repo_path is required and must be a string"), nil
	}

	forceReindex := false
	if fr, ok := args["force_reindex"].(bool); ok {
		forceReindex = fr
	}

	// Start indexing
	job, err := s.indexer.Index(repoPath, forceReindex)
	if err != nil {
		return errorResult(fmt.Sprintf("failed to start indexing: %v", err)), nil
	}

	// If running synchronously, wait for completion
	if !s.config.Indexing.Background {
		// Poll for job completion
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return errorResult("indexing cancelled"), nil
			case <-ticker.C:
				currentJob, err := s.indexer.GetJob(job.ID)
				if err != nil {
					return errorResult(fmt.Sprintf("failed to get job status: %v", err)), nil
				}

				// Check if job is complete
				if currentJob.Status == "completed" || currentJob.Status == "failed" {
					duration := currentJob.EndTime.Sub(currentJob.StartTime)

					if currentJob.Status == "failed" {
						// Failed indexing - provide detailed error with troubleshooting steps
						errorMsg := fmt.Sprintf(`❌ Indexing Failed

Error: %s

Files scanned: %d/%d
Chunks created: %d
Duration: %.1fs

🔧 Troubleshooting:
1. Check if Qdrant is running: docker-compose ps
2. Check if Ollama is running: curl http://localhost:11434/api/tags
3. Check logs for details: docker-compose logs qdrant
4. If issue persists, try: force_reindex=true

Note: Cache was NOT updated. Files will be reprocessed on next attempt.`,
							currentJob.Error,
							currentJob.FilesIndexed,
							currentJob.FilesTotal,
							currentJob.ChunksTotal,
							duration.Seconds())

						return errorResult(errorMsg), nil
					}

					// Successful indexing
					successMsg := fmt.Sprintf(`✅ Indexing Completed Successfully

Files indexed: %d
Code chunks: %d
Duration: %.1fs

You can now search this codebase with semantic queries.`,
						currentJob.FilesIndexed,
						currentJob.ChunksTotal,
						duration.Seconds())

					return &mcp.CallToolResult{
						Content: []mcp.Content{
							mcp.TextContent{
								Type: "text",
								Text: successMsg,
							},
						},
					}, nil
				}
			}
		}
	}

	// Background mode: return immediately
	response := map[string]interface{}{
		"message":       "Indexing started in background",
		"job_id":        job.ID,
		"repo":          repoPath,
		"force_reindex": forceReindex,
		"status":        job.Status,
		"background":    true,
		"note":          "Use get_index_status to check progress",
	}

	return successResult(response), nil
}

func (s *Server) handleClearCache(ctx context.Context, args map[string]interface{}) (*mcp.CallToolResult, error) {
	repoPath, ok := args["repo_path"].(string)
	if !ok || repoPath == "" {
		return errorResult("repo_path is required and must be a string"), nil
	}

	// Clear cache
	if err := s.indexer.ClearCache(repoPath); err != nil {
		return errorResult(fmt.Sprintf("failed to clear cache: %v", err)), nil
	}

	response := map[string]interface{}{
		"message": "Cache cleared successfully",
		"repo":    repoPath,
	}

	return successResult(response), nil
}

func (s *Server) handleGetIndexStatus(ctx context.Context, args map[string]interface{}) (*mcp.CallToolResult, error) {
	repoPath, ok := args["repo_path"].(string)
	if !ok || repoPath == "" {
		return errorResult("repo_path is required and must be a string"), nil
	}

	// Get repository index
	repoIndex, err := s.indexer.GetRepoIndex(repoPath)
	if err != nil {
		return errorResult(fmt.Sprintf("failed to get index status: %v", err)), nil
	}

	return successResult(repoIndex), nil
}

// Helper functions

func successResult(data interface{}) *mcp.CallToolResult {
	jsonData, _ := json.MarshalIndent(data, "", "  ")
	return &mcp.CallToolResult{
		Content: []mcp.Content{
			mcp.TextContent{
				Type: "text",
				Text: string(jsonData),
			},
		},
	}
}

func errorResult(message string) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Content: []mcp.Content{
			mcp.TextContent{
				Type: "text",
				Text: fmt.Sprintf("Error: %s", message),
			},
		},
		IsError: true,
	}
}

func formatSearchResults(results []search.SearchResult) string {
	if len(results) == 0 {
		return "No results found."
	}

	var output strings.Builder
	output.WriteString(fmt.Sprintf("Found %d results:\n\n", len(results)))

	for i, result := range results {
		chunk := result.Chunk

		// Format file location
		location := fmt.Sprintf("%s:%d-%d", chunk.FilePath, chunk.StartLine, chunk.EndLine)
		if chunk.FunctionName != "" {
			location += fmt.Sprintf(" (in %s)", chunk.FunctionName)
		} else if chunk.ClassName != "" {
			location += fmt.Sprintf(" (in %s)", chunk.ClassName)
		}

		// Format score info
		scoreInfo := fmt.Sprintf("score: %.3f", result.HybridScore)
		if result.ExactMatch {
			scoreInfo += " [EXACT MATCH]"
		}

		// Write result
		output.WriteString(fmt.Sprintf("%d. %s\n", i+1, location))
		output.WriteString(fmt.Sprintf("   %s\n", scoreInfo))
		output.WriteString(fmt.Sprintf("   Language: %s, Type: %s\n", chunk.Language, chunk.ChunkType))

		// Show content preview (first 3 lines)
		lines := strings.Split(chunk.Content, "\n")
		previewLines := 3
		if len(lines) < previewLines {
			previewLines = len(lines)
		}

		output.WriteString("   Preview:\n")
		for j := 0; j < previewLines; j++ {
			line := strings.TrimSpace(lines[j])
			if len(line) > 80 {
				line = line[:80] + "..."
			}
			output.WriteString(fmt.Sprintf("   │ %s\n", line))
		}
		if len(lines) > previewLines {
			output.WriteString(fmt.Sprintf("   │ ... (%d more lines)\n", len(lines)-previewLines))
		}

		output.WriteString("\n")
	}

	return output.String()
}
