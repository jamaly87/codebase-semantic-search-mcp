package main

import (
	"log"
	"log/slog"
	"os"
	"time"

	"github.com/jamaly87/codebase-semantic-search/internal/indexer"
	"github.com/jamaly87/codebase-semantic-search/pkg/config"
)

func main() {
	// Get repo path from args or use current directory
	repoPath, err := os.Getwd()
	if err != nil {
		log.Fatalf("Failed to get current directory: %v", err)
	}

	if len(os.Args) > 1 {
		repoPath = os.Args[1]
	}

	slog.Info("Starting repository indexing", "repository", repoPath)

	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// Ensure synchronous mode
	cfg.Indexing.Background = false

	slog.Info("Configuration loaded",
		"model", cfg.Embeddings.Model,
		"batch_size", cfg.Embeddings.BatchSize,
		"workers", cfg.Indexing.ParallelWorkers,
		"background", cfg.Indexing.Background)

	// Create indexer
	slog.Info("Initializing indexer")
	idx, err := indexer.NewIndexer(cfg)
	if err != nil {
		log.Fatalf("Failed to create indexer: %v", err)
	}
	slog.Info("Indexer ready")

	// Index the repository
	slog.Info("Starting indexing process")
	startTime := time.Now()

	job, err := idx.Index(repoPath, true) // force reindex
	if err != nil {
		log.Fatalf("Failed to start indexing: %v", err)
	}

	duration := time.Since(startTime)

	if job.Error != "" {
		slog.Error("Indexing failed",
			"error", job.Error,
			"job_id", job.ID,
			"repository", job.RepoPath,
			"files_total", job.FilesTotal,
			"files_indexed", job.FilesIndexed,
			"chunks_total", job.ChunksTotal,
			"duration", duration)
		os.Exit(1)
	}

	slog.Info("Indexing completed successfully",
		"job_id", job.ID,
		"status", job.Status,
		"repository", job.RepoPath,
		"files_total", job.FilesTotal,
		"files_indexed", job.FilesIndexed,
		"chunks_total", job.ChunksTotal,
		"duration", duration)
}
