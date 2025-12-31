package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/jamaly87/codebase-semantic-search/internal/mcp"
	"github.com/jamaly87/codebase-semantic-search/pkg/config"
)

func main() {
	// Set up logging
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	log.SetPrefix("[semantic-search] ")

	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	log.Printf("Configuration loaded successfully")
	log.Printf("Embedding model: %s", cfg.Embeddings.Model)
	log.Printf("Ollama URL: %s", cfg.Embeddings.OllamaURL)
	log.Printf("Supported languages: Java, TypeScript, JavaScript")

	// Create MCP server
	server, err := mcp.NewServer(cfg)
	if err != nil {
		log.Fatalf("Failed to create MCP server: %v", err)
	}
	defer server.Close()

	// Set up context with cancellation
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle shutdown signals
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-sigChan
		log.Println("Received shutdown signal...")
		cancel()
	}()

	// Start the server
	log.Println("Starting MCP server...")
	if err := server.Start(ctx); err != nil {
		log.Fatalf("Server error: %v", err)
	}
}
