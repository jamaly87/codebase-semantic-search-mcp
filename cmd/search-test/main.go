package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"log/slog"
	"os"
	"time"

	"github.com/jamaly87/codebase-semantic-search/internal/embeddings"
	"github.com/jamaly87/codebase-semantic-search/internal/search"
	"github.com/jamaly87/codebase-semantic-search/internal/vectordb"
	"github.com/jamaly87/codebase-semantic-search/pkg/config"
)

func main() {
	// Parse command line arguments
	query := flag.String("query", "", "Search query")
	repoPath := flag.String("repo", "", "Repository path")
	flag.Parse()

	// Use current directory if no repo specified
	if *repoPath == "" {
		var err error
		*repoPath, err = os.Getwd()
		if err != nil {
			log.Fatalf("Failed to get current directory: %v", err)
		}
	}

	// Use default query if none specified
	if *query == "" {
		*query = "JWT token validation"
	}

	slog.Info("Starting semantic search test", "repository", *repoPath, "query", *query)

	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// Create embeddings client
	embeddingsClient := embeddings.NewClient(&cfg.Embeddings)

	// Create vector database client
	vectorDB, err := vectordb.NewClient(&cfg.VectorDB)
	if err != nil {
		log.Fatalf("Failed to create vector DB client: %v", err)
	}
	defer vectorDB.Close()

	// Create searcher
	searcher := search.NewSearcher(&cfg.Search, embeddingsClient, vectorDB)

	// Perform search
	start := time.Now()
	results, err := searcher.Search(context.Background(), *query, *repoPath)
	if err != nil {
		log.Fatalf("Search failed: %v", err)
	}
	duration := time.Since(start)

	// Display results
	slog.Info("Search completed", "duration", duration, "results_found", len(results))

	if len(results) == 0 {
		slog.Warn("No results found")
		return
	}

	for i, result := range results {
		chunk := result.Chunk

		// Format file location
		location := fmt.Sprintf("%s:%d-%d", chunk.FilePath, chunk.StartLine, chunk.EndLine)
		if chunk.FunctionName != "" {
			location += fmt.Sprintf(" (in %s)", chunk.FunctionName)
		} else if chunk.ClassName != "" {
			location += fmt.Sprintf(" (in class %s)", chunk.ClassName)
		}

		// Log result
		slog.Info("Search result",
			"rank", i+1,
			"location", location,
			"hybrid_score", result.HybridScore,
			"semantic_score", result.SemanticScore,
			"exact_match", result.ExactMatch,
			"language", chunk.Language,
			"chunk_type", chunk.ChunkType)
	}

	// Performance stats
	resultsPerSec := 0.0
	if duration.Milliseconds() > 0 {
		resultsPerSec = float64(len(results)) / duration.Seconds()
	}

	slog.Info("Search performance",
		"search_time", duration,
		"results_count", len(results),
		"results_per_sec", resultsPerSec)
}
