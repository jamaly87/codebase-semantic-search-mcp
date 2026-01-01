package indexer

import (
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/jamaly87/codebase-semantic-search/internal/models"
	"github.com/jamaly87/codebase-semantic-search/pkg/config"
)

// Chunker splits code files into semantic chunks using AST and token-aware strategies
type Chunker struct {
	config       *config.ChunkingConfig
	langDetector *LanguageDetector
	astChunker   *ASTChunker
	tokenChunker *TokenChunker
}

// NewChunker creates a new code chunker with AST and token-based strategies
func NewChunker(cfg *config.ChunkingConfig) *Chunker {
	// Create AST chunker (tries to use Tree-sitter when available)
	astChunker, err := NewASTChunker()
	if err != nil {
		log.Printf("Warning: AST chunker initialization failed: %v", err)
	}

	// Create token-based chunker (fallback strategy)
	// maxTokens: ~200 tokens per chunk (conservative for nomic-embed-text's 8192 limit)
	// overlap: ~20 tokens to maintain context
	tokenChunker, err := NewTokenChunker(200, 20)
	if err != nil {
		log.Fatalf("Failed to create token chunker: %v", err)
	}

	chunker := &Chunker{
		config:       cfg,
		langDetector: NewLanguageDetector(),
		astChunker:   astChunker,
		tokenChunker: tokenChunker,
	}

	// Log parser status
	if astChunker != nil {
		astChunker.LogParserStatus()
	}

	return chunker
}

// ChunkFile splits a file into semantic chunks using the best available strategy
// Strategy priority:
//   1. AST-based (if Tree-sitter parser available for language) - 80-95% accuracy
//   2. Token-aware (fallback for all languages) - 60-75% accuracy
//
// File-level chunks are REMOVED entirely to prevent context length errors
func (c *Chunker) ChunkFile(repoPath, filePath string) ([]models.CodeChunk, error) {
	// Detect language
	lang, ok := c.langDetector.Detect(filePath)
	if !ok {
		return nil, fmt.Errorf("unsupported file type: %s", filePath)
	}

	// Read file content
	content, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	fileContent := string(content)
	if strings.TrimSpace(fileContent) == "" {
		return nil, nil // Skip empty files
	}

	var chunks []models.CodeChunk

	// Strategy 1: Try AST-based chunking (highest accuracy)
	if c.astChunker != nil && c.astChunker.CanParseLanguage(lang.Name) {
		astChunks, err := c.astChunker.ChunkByAST(repoPath, filePath, lang.Name, fileContent)
		if err == nil && len(astChunks) > 0 {
			log.Printf("✓ AST chunking: %s (%d chunks)", filePath, len(astChunks))
			return astChunks, nil
		}
		// If AST parsing failed, fall through to token-based
		if err != nil {
			log.Printf("AST parsing failed for %s: %v, falling back to token-based", filePath, err)
		}
	}

	// Strategy 2: Token-aware chunking (fallback for all languages)
	tokenChunks, err := c.tokenChunker.ChunkByTokens(repoPath, filePath, lang.Name, fileContent)
	if err != nil {
		return nil, fmt.Errorf("token chunking failed: %w", err)
	}

	if len(tokenChunks) > 0 {
		log.Printf("✓ Token chunking: %s (%d chunks)", filePath, len(tokenChunks))
	}

	chunks = append(chunks, tokenChunks...)

	return chunks, nil
}

// GetStats returns statistics about chunking
func (c *Chunker) GetStats(chunks []models.CodeChunk) map[string]int {
	stats := map[string]int{
		"total":    len(chunks),
		"function": 0,
	}

	for _, chunk := range chunks {
		if chunk.ChunkType == models.ChunkTypeFunction {
			stats["function"]++
		}
	}

	return stats
}

// Close cleans up resources
func (c *Chunker) Close() {
	if c.astChunker != nil {
		c.astChunker.Close()
	}
}
