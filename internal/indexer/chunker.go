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
// Uses adaptive chunking based on file size for optimal chunk granularity
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

	// Calculate file size in lines for adaptive chunking
	fileLines := strings.Count(fileContent, "\n") + 1
	maxTokens, overlapTokens := c.calculateOptimalChunkSize(fileLines)

	var chunks []models.CodeChunk

	// Strategy 1: Try AST-based chunking (highest accuracy)
	if c.astChunker != nil && c.astChunker.CanParseLanguage(lang.Name) {
		astChunks, err := c.astChunker.ChunkByAST(repoPath, filePath, lang.Name, fileContent, c.config)
		if err == nil && len(astChunks) > 0 {
			log.Printf("✓ AST chunking: %s (%d chunks, %d lines)", filePath, len(astChunks), fileLines)
			return astChunks, nil
		}
		// If AST parsing failed, fall through to token-based
		if err != nil {
			log.Printf("AST parsing failed for %s: %v, falling back to token-based", filePath, err)
		}
	}

	// Strategy 2: Token-aware chunking (fallback for all languages)
	// Pass limits directly to avoid race conditions from SetLimits
	tokenChunks, err := c.tokenChunker.ChunkByTokensWithLimits(repoPath, filePath, lang.Name, fileContent, maxTokens, overlapTokens)
	if err != nil {
		return nil, fmt.Errorf("token chunking failed: %w", err)
	}

	if len(tokenChunks) > 0 {
		log.Printf("✓ Token chunking: %s (%d chunks, %d lines, %d tokens/chunk)", filePath, len(tokenChunks), fileLines, maxTokens)
	}

	chunks = append(chunks, tokenChunks...)

	return chunks, nil
}

// calculateOptimalChunkSize determines optimal chunk size based on file size
// Returns maxTokens and overlapTokens for the token chunker
func (c *Chunker) calculateOptimalChunkSize(fileLines int) (maxTokens, overlapTokens int) {
	// Use adaptive chunking if configured, otherwise use defaults
	if c.config.SmallFileMaxTokens > 0 {
		switch {
		case fileLines < 1000:
			maxTokens = c.config.SmallFileMaxTokens
			overlapTokens = maxTokens / 15 // ~6.7% overlap
		case fileLines < 5000:
			maxTokens = c.config.MediumFileMaxTokens
			overlapTokens = maxTokens / 10 // ~10% overlap
		default:
			maxTokens = c.config.LargeFileMaxTokens
			overlapTokens = maxTokens / 7 // ~14% overlap (more overlap for large files)
		}
	} else {
		// Default values if not configured
		maxTokens = 200
		overlapTokens = 20
	}

	return maxTokens, overlapTokens
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
