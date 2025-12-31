package search

import (
	"context"
	"fmt"
	"log"
	"sort"
	"strings"

	"github.com/jamaly87/codebase-semantic-search/internal/models"
	"github.com/jamaly87/codebase-semantic-search/pkg/config"
)

// EmbeddingsClient interface for generating embeddings
type EmbeddingsClient interface {
	GenerateEmbedding(text string) ([]float32, error)
}

// VectorDB interface for vector database operations
type VectorDB interface {
	Search(ctx context.Context, embedding []float32, repoPath string, limit int) ([]models.CodeChunk, []float64, error)
}

// SearchResult represents a search result with scoring information
type SearchResult struct {
	Chunk          models.CodeChunk
	SemanticScore  float64
	ExactMatch     bool
	HybridScore    float64
	MatchPositions []int
}

// Searcher handles semantic search operations
type Searcher struct {
	config           *config.SearchConfig
	embeddingsClient EmbeddingsClient
	vectorDB         VectorDB
}

// NewSearcher creates a new search service
func NewSearcher(cfg *config.SearchConfig, embeddingsClient EmbeddingsClient, vectorDB VectorDB) *Searcher {
	return &Searcher{
		config:           cfg,
		embeddingsClient: embeddingsClient,
		vectorDB:         vectorDB,
	}
}

// Search performs a semantic search with hybrid scoring
func (s *Searcher) Search(ctx context.Context, query string, repoPath string) ([]SearchResult, error) {
	log.Printf("Searching for: %q in repo: %s", query, repoPath)

	// Generate embedding for query
	queryEmbedding, err := s.embeddingsClient.GenerateEmbedding(query)
	if err != nil {
		return nil, fmt.Errorf("failed to generate query embedding: %w", err)
	}

	// Search vector database
	// Request more results than needed to allow for reranking
	searchLimit := s.config.MaxResults * 3
	chunks, semanticScores, err := s.vectorDB.Search(ctx, queryEmbedding, repoPath, searchLimit)
	if err != nil {
		return nil, fmt.Errorf("failed to search vector database: %w", err)
	}

	if len(chunks) == 0 {
		log.Printf("No results found for query: %q", query)
		return []SearchResult{}, nil
	}

	// Apply hybrid scoring
	results := s.applyHybridScoring(query, chunks, semanticScores)

	// Sort by hybrid score (descending)
	sort.Slice(results, func(i, j int) bool {
		return results[i].HybridScore > results[j].HybridScore
	})

	// Limit to max results
	if len(results) > s.config.MaxResults {
		results = results[:s.config.MaxResults]
	}

	log.Printf("Returning %d results (top score: %.3f)", len(results), results[0].HybridScore)
	return results, nil
}

// applyHybridScoring applies hybrid scoring: semantic similarity + exact match boost + file path scoring
func (s *Searcher) applyHybridScoring(query string, chunks []models.CodeChunk, semanticScores []float64) []SearchResult {
	results := make([]SearchResult, len(chunks))
	queryLower := strings.ToLower(query)
	queryWords := strings.Fields(queryLower)

	for i, chunk := range chunks {
		result := SearchResult{
			Chunk:         chunk,
			SemanticScore: semanticScores[i],
			ExactMatch:    false,
			HybridScore:   0,
		}

		// Start with semantic score (scaled by weight)
		hybridScore := semanticScores[i] * s.config.SemanticWeight

		// Check for exact match (case-insensitive)
		contentLower := strings.ToLower(chunk.Content)
		if strings.Contains(contentLower, queryLower) {
			result.ExactMatch = true
			result.MatchPositions = findMatchPositions(contentLower, queryLower)

			// ADDITIVE boost for exact match (not multiplicative)
			hybridScore += s.config.ExactMatchBoost
			log.Printf("Exact match found in %s:%d-%d (score: %.3f + %.3f = %.3f)",
				chunk.FilePath, chunk.StartLine, chunk.EndLine,
				semanticScores[i]*s.config.SemanticWeight, s.config.ExactMatchBoost, hybridScore)
		} else {
			// Partial word matching - score based on matched query words
			matchedWords := 0
			for _, word := range queryWords {
				if len(word) > 2 && strings.Contains(contentLower, word) {
					matchedWords++
				}
			}

			if matchedWords > 0 && len(queryWords) > 0 {
				partialMatchBoost := (float64(matchedWords) / float64(len(queryWords))) * 0.3
				hybridScore += partialMatchBoost
				log.Printf("Partial match in %s:%d-%d (%d/%d words matched, boost: +%.3f)",
					chunk.FilePath, chunk.StartLine, chunk.EndLine,
					matchedWords, len(queryWords), partialMatchBoost)
			}
		}

		// File path scoring: penalize test files, boost source files
		pathScore := calculateFilePathScore(chunk.FilePath)
		hybridScore *= pathScore

		if pathScore != 1.0 {
			log.Printf("File path adjustment for %s: %.2fx (score: %.3f -> %.3f)",
				chunk.FilePath, pathScore, hybridScore/pathScore, hybridScore)
		}

		result.HybridScore = hybridScore
		results[i] = result
	}

	return results
}

// calculateFilePathScore returns a multiplier based on file path characteristics
// Penalizes test files, boosts main source files
func calculateFilePathScore(filePath string) float64 {
	pathLower := strings.ToLower(filePath)

	// Extreme penalty for test files (0.05x - rank 95% lower)
	if isTestFile(pathLower) {
		return 0.05
	}

	// Boost for main source files (1.3x - rank 30% higher)
	if isMainSourceFile(pathLower) {
		return 1.3
	}

	// Heavy penalty for generated/vendor code (0.2x)
	if isGeneratedOrVendor(pathLower) {
		return 0.2
	}

	// Neutral for other files
	return 1.0
}

// isTestFile detects test files by common patterns
func isTestFile(pathLower string) bool {
	// Directory-based detection
	if strings.Contains(pathLower, "/test/") ||
		strings.Contains(pathLower, "/tests/") ||
		strings.Contains(pathLower, "/__tests__/") ||
		strings.Contains(pathLower, "/spec/") {
		return true
	}

	// File name-based detection
	if strings.HasSuffix(pathLower, "_test.go") ||
		strings.HasSuffix(pathLower, "_test.js") ||
		strings.HasSuffix(pathLower, "_test.ts") ||
		strings.HasSuffix(pathLower, ".test.js") ||
		strings.HasSuffix(pathLower, ".test.ts") ||
		strings.HasSuffix(pathLower, ".test.jsx") ||
		strings.HasSuffix(pathLower, ".test.tsx") ||
		strings.HasSuffix(pathLower, ".spec.js") ||
		strings.HasSuffix(pathLower, ".spec.ts") ||
		strings.HasSuffix(pathLower, ".spec.jsx") ||
		strings.HasSuffix(pathLower, ".spec.tsx") ||
		strings.HasSuffix(pathLower, "test.java") ||
		strings.HasSuffix(pathLower, "tests.java") {
		return true
	}

	return false
}

// isMainSourceFile detects main source files
func isMainSourceFile(pathLower string) bool {
	return strings.Contains(pathLower, "/src/main/") ||
		strings.Contains(pathLower, "/src/core/") ||
		strings.Contains(pathLower, "/lib/") ||
		strings.Contains(pathLower, "/pkg/") ||
		strings.Contains(pathLower, "/internal/") ||
		(strings.Contains(pathLower, "/cmd/") && !strings.Contains(pathLower, "/test"))
}

// isGeneratedOrVendor detects generated or vendor code
func isGeneratedOrVendor(pathLower string) bool {
	return strings.Contains(pathLower, "/vendor/") ||
		strings.Contains(pathLower, "/node_modules/") ||
		strings.Contains(pathLower, "/target/") ||
		strings.Contains(pathLower, "/build/") ||
		strings.Contains(pathLower, "/dist/") ||
		strings.Contains(pathLower, ".generated.") ||
		strings.Contains(pathLower, "_generated.")
}

// findMatchPositions finds all positions where the query appears in the content
func findMatchPositions(content, query string) []int {
	var positions []int
	pos := 0

	for {
		idx := strings.Index(content[pos:], query)
		if idx == -1 {
			break
		}
		positions = append(positions, pos+idx)
		pos += idx + len(query)
	}

	return positions
}

// FormatResults formats search results for display
func FormatResults(results []SearchResult) string {
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
