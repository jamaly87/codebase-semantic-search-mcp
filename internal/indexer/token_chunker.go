package indexer

import (
	"fmt"
	"regexp"
	"strings"
	"sync"

	"github.com/google/uuid"
	"github.com/jamaly87/codebase-semantic-search/internal/models"
	"github.com/pkoukk/tiktoken-go"
)

// TokenChunker splits code into chunks based on token count (model-aware)
type TokenChunker struct {
	tokenizer *tiktoken.Tiktoken
	maxTokens int
	overlap   int
	mux       sync.RWMutex // For thread-safe limit updates
}

// NewTokenChunker creates a new token-based chunker
func NewTokenChunker(maxTokens, overlap int) (*TokenChunker, error) {
	// Use cl100k_base encoding (used by gpt-3.5-turbo and gpt-4)
	// This is compatible with most modern LLMs
	tokenizer, err := tiktoken.GetEncoding("cl100k_base")
	if err != nil {
		return nil, fmt.Errorf("failed to get tokenizer: %w", err)
	}

	return &TokenChunker{
		tokenizer: tokenizer,
		maxTokens: maxTokens,
		overlap:   overlap,
	}, nil
}

// ChunkByTokens splits content into token-aware chunks with smart boundaries
// Uses the current limits set via SetLimits
func (tc *TokenChunker) ChunkByTokens(repoPath, filePath, language, content string) ([]models.CodeChunk, error) {
	// Get current limits (thread-safe)
	tc.mux.RLock()
	maxTokens := tc.maxTokens
	overlap := tc.overlap
	tc.mux.RUnlock()

	return tc.chunkWithLimits(repoPath, filePath, language, content, maxTokens, overlap)
}

// ChunkByTokensWithLimits splits content into token-aware chunks with specified limits
// Thread-safe: uses provided limits instead of shared state
func (tc *TokenChunker) ChunkByTokensWithLimits(repoPath, filePath, language, content string, maxTokens, overlap int) ([]models.CodeChunk, error) {
	return tc.chunkWithLimits(repoPath, filePath, language, content, maxTokens, overlap)
}

// chunkWithLimits is the internal implementation that does the actual chunking
func (tc *TokenChunker) chunkWithLimits(repoPath, filePath, language, content string, maxTokens, overlap int) ([]models.CodeChunk, error) {

	// Split content into lines for boundary detection
	lines := strings.Split(content, "\n")

	var chunks []models.CodeChunk
	var currentLines []string
	var currentTokens int
	startLine := 1

	i := 0
	for i < len(lines) {
		line := lines[i]
		// Count tokens in this line
		lineTokens := len(tc.tokenizer.Encode(line, nil, nil))

		// Check if adding this line would exceed max tokens
		if currentTokens+lineTokens > maxTokens && len(currentLines) > 0 {
			// Look ahead for a natural boundary within next 10 lines
			boundaryFound := false
			for j := i; j < i+10 && j < len(lines); j++ {
				trimmed := strings.TrimSpace(lines[j])
				if IsBoundary(trimmed, language) {
					// Found a boundary, extend to there
					for k := i; k <= j; k++ {
						currentLines = append(currentLines, lines[k])
						currentTokens += len(tc.tokenizer.Encode(lines[k], nil, nil))
					}
					i = j + 1
					boundaryFound = true
					break
				}
			}

			// Create chunk
			chunk := tc.createChunk(repoPath, filePath, language, currentLines, startLine)
			if chunk != nil {
				chunks = append(chunks, *chunk)
			}

			// Create overlap for next chunk
			overlapLines := tc.calculateOverlapLines(currentLines, overlap)
			currentLines = overlapLines
			currentTokens = tc.countTokens(strings.Join(currentLines, "\n"))
			startLine = i - len(overlapLines)

			if boundaryFound {
				continue
			}
		}

		// Add current line to chunk
		currentLines = append(currentLines, line)
		currentTokens += lineTokens
		i++
	}

	// Add remaining chunk
	if len(currentLines) > 0 {
		chunk := tc.createChunk(repoPath, filePath, language, currentLines, startLine)
		if chunk != nil {
			chunks = append(chunks, *chunk)
		}
	}

	return chunks, nil
}

// createChunk creates a code chunk from lines
func (tc *TokenChunker) createChunk(repoPath, filePath, language string, lines []string, startLine int) *models.CodeChunk {
	content := strings.Join(lines, "\n")

	// Skip empty chunks
	if strings.TrimSpace(content) == "" {
		return nil
	}

	// Ensure chunk doesn't exceed safe size (4000 chars ~ 1000 tokens)
	const maxChunkSize = 4000
	if len(content) > maxChunkSize {
		content = content[:maxChunkSize]
	}

	return &models.CodeChunk{
		ID:        uuid.New().String(),
		RepoPath:  repoPath,
		FilePath:  filePath,
		ChunkType: models.ChunkTypeFunction, // Using function type for semantic chunks
		Content:   content,
		Language:  language,
		StartLine: startLine,
		EndLine:   startLine + len(lines) - 1,
	}
}

// calculateOverlapLines returns lines to overlap with next chunk
func (tc *TokenChunker) calculateOverlapLines(lines []string, overlapTokens int) []string {
	if len(lines) == 0 || overlapTokens <= 0 {
		return []string{}
	}

	// Calculate overlap in tokens
	var overlapLines []string
	currentOverlap := 0

	// Work backwards from end
	for i := len(lines) - 1; i >= 0 && currentOverlap < overlapTokens; i-- {
		line := lines[i]
		lineTokens := len(tc.tokenizer.Encode(line, nil, nil))
		currentOverlap += lineTokens
		overlapLines = append([]string{line}, overlapLines...)
	}

	return overlapLines
}

// countTokens counts total tokens in text
func (tc *TokenChunker) countTokens(text string) int {
	return len(tc.tokenizer.Encode(text, nil, nil))
}

// SetLimits updates the max tokens and overlap for adaptive chunking
// This allows different chunk sizes based on file size
func (tc *TokenChunker) SetLimits(maxTokens, overlap int) error {
	if maxTokens <= 0 {
		return fmt.Errorf("maxTokens must be positive, got %d", maxTokens)
	}
	if overlap < 0 {
		return fmt.Errorf("overlap must be non-negative, got %d", overlap)
	}
	if overlap >= maxTokens {
		return fmt.Errorf("overlap (%d) must be less than maxTokens (%d)", overlap, maxTokens)
	}

	tc.mux.Lock()
	defer tc.mux.Unlock()

	tc.maxTokens = maxTokens
	tc.overlap = overlap
	return nil
}

// GetLimits returns the current max tokens and overlap settings
func (tc *TokenChunker) GetLimits() (maxTokens, overlap int) {
	tc.mux.RLock()
	defer tc.mux.RUnlock()
	return tc.maxTokens, tc.overlap
}

// GetLanguagePatterns returns regex patterns for detecting code boundaries
// This is used as fallback when AST parsing is not available
func GetLanguagePatterns(language string) []string {
	patterns := map[string][]string{
		"java": {
			`^\s*(public|private|protected)?\s*(static\s+)?class\s+\w+`,
			`^\s*(public|private|protected)?\s*(static\s+)?interface\s+\w+`,
			`^\s*(public|private|protected)?\s*(static\s+)?enum\s+\w+`,
			`^\s*(public|private|protected)?\s*(static\s+)?[\w<>\[\]]+\s+\w+\s*\([^)]*\)\s*\{?`,
			`^\s*@\w+`, // Annotations
		},
		"javascript": {
			`^\s*export\s+(default\s+)?function\s+\w+`,
			`^\s*export\s+(default\s+)?class\s+\w+`,
			`^\s*export\s+(const|let|var)\s+\w+`,
			`^\s*(async\s+)?function\s+\w+`,
			`^\s*class\s+\w+`,
			`^\s*(const|let|var)\s+\w+\s*=\s*(async\s+)?\([^)]*\)\s*=>`,
		},
		"typescript": {
			`^\s*export\s+(default\s+)?function\s+\w+`,
			`^\s*export\s+(default\s+)?class\s+\w+`,
			`^\s*export\s+(interface|type)\s+\w+`,
			`^\s*export\s+(const|let|var)\s+\w+`,
			`^\s*(async\s+)?function\s+\w+`,
			`^\s*class\s+\w+`,
			`^\s*interface\s+\w+`,
			`^\s*type\s+\w+\s*=`,
			`^\s*(const|let|var)\s+\w+\s*=\s*(async\s+)?\([^)]*\)\s*=>`,
		},
		"go": {
			`^\s*func\s+\w+`,
			`^\s*func\s+\([^)]+\)\s+\w+`,
			`^\s*type\s+\w+\s+(struct|interface)`,
			`^\s*(const|var)\s+\w+`,
		},
		"python": {
			`^\s*def\s+\w+`,
			`^\s*class\s+\w+`,
			`^\s*async\s+def\s+\w+`,
			`^\s*@\w+`, // Decorators
		},
		"rust": {
			`^\s*(pub\s+)?fn\s+\w+`,
			`^\s*(pub\s+)?struct\s+\w+`,
			`^\s*(pub\s+)?enum\s+\w+`,
			`^\s*(pub\s+)?trait\s+\w+`,
			`^\s*(pub\s+)?impl\s+`,
		},
		"c": {
			`^\s*\w+\s+\w+\s*\([^)]*\)\s*\{?`,
			`^\s*struct\s+\w+`,
			`^\s*typedef\s+`,
		},
		"cpp": {
			`^\s*\w+\s+\w+::\w+\s*\([^)]*\)`,
			`^\s*class\s+\w+`,
			`^\s*struct\s+\w+`,
			`^\s*namespace\s+\w+`,
			`^\s*template\s*<`,
		},
	}

	if p, ok := patterns[language]; ok {
		return p
	}

	// Default patterns for unknown languages
	return []string{
		`^\s*function\s+\w+`,
		`^\s*class\s+\w+`,
		`^\s*def\s+\w+`,
	}
}

// IsBoundary checks if a line matches any boundary pattern for the language
func IsBoundary(line, language string) bool {
	patterns := GetLanguagePatterns(language)
	line = strings.TrimSpace(line)

	for _, pattern := range patterns {
		matched, err := regexp.MatchString(pattern, line)
		if err == nil && matched {
			return true
		}
	}

	return false
}