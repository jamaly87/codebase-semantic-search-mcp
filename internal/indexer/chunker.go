package indexer

import (
	"bufio"
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/google/uuid"
	"github.com/jamaly87/codebase-semantic-search/internal/models"
	"github.com/jamaly87/codebase-semantic-search/pkg/config"
)

// Chunker splits code files into chunks
type Chunker struct {
	config       *config.ChunkingConfig
	langDetector *LanguageDetector
}

// NewChunker creates a new code chunker
func NewChunker(cfg *config.ChunkingConfig) *Chunker {
	return &Chunker{
		config:       cfg,
		langDetector: NewLanguageDetector(),
	}
}

// ChunkFile splits a file into chunks
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

	// Split into lines
	lines := strings.Split(string(content), "\n")

	var chunks []models.CodeChunk

	// Create file-level chunk (whole file)
	fileChunk := c.createFileChunk(repoPath, filePath, lang.Name, string(content), len(lines))
	chunks = append(chunks, fileChunk)

	// Create line-based chunks (25 lines with overlap)
	lineChunks := c.createLineChunks(repoPath, filePath, lang.Name, lines)
	chunks = append(chunks, lineChunks...)

	// TODO: Phase 2.5 - Add function-level chunks using tree-sitter
	// This will be implemented after we integrate tree-sitter parsers

	return chunks, nil
}

// createFileChunk creates a chunk for the entire file
func (c *Chunker) createFileChunk(repoPath, filePath, language, content string, totalLines int) models.CodeChunk {
	return models.CodeChunk{
		ID:        uuid.New().String(),
		RepoPath:  repoPath,
		FilePath:  filePath,
		ChunkType: models.ChunkTypeFile,
		Content:   content,
		Language:  language,
		StartLine: 1,
		EndLine:   totalLines,
	}
}

// createLineChunks creates line-based chunks with overlap and smart boundary detection
func (c *Chunker) createLineChunks(repoPath, filePath, language string, lines []string) []models.CodeChunk {
	var chunks []models.CodeChunk

	maxLines := c.config.MaxLines
	overlap := c.config.OverlapLines

	// Skip if file is too small
	if len(lines) <= maxLines {
		return chunks // File-level chunk is enough
	}

	// Get language-specific patterns for boundary detection
	boundaryPattern := getFunctionBoundaryPattern(language)

	currentChunk := []string{}
	chunkStartLine := 0

	for i := 0; i < len(lines); i++ {
		line := lines[i]
		currentChunk = append(currentChunk, line)

		// Check if we should split
		shouldSplit := false
		isGoodSplitPoint := false

		// Check if we've exceeded max lines
		if len(currentChunk) >= maxLines {
			shouldSplit = true

			// Check if this is a good split point (function/class boundary)
			if i+1 < len(lines) && boundaryPattern != nil {
				trimmedNextLine := strings.TrimSpace(lines[i+1])
				if boundaryPattern.MatchString(trimmedNextLine) && len(currentChunk) > 5 {
					isGoodSplitPoint = true
				}
			}

			// If not at a good split point but we're past 60% of max, look ahead for one
			if !isGoodSplitPoint && len(currentChunk) >= int(float64(maxLines)*0.6) {
				for j := i + 1; j < i+10 && j < len(lines); j++ {
					trimmedLine := strings.TrimSpace(lines[j])
					if boundaryPattern != nil && boundaryPattern.MatchString(trimmedLine) {
						// Found a boundary within 10 lines, extend to there
						for k := i + 1; k <= j; k++ {
							currentChunk = append(currentChunk, lines[k])
						}
						i = j
						isGoodSplitPoint = true
						break
					}
				}
			}
		}

		// Create chunk if we should split
		if shouldSplit && len(currentChunk) > 0 {
			content := strings.Join(currentChunk, "\n")

			// Skip empty or whitespace-only chunks
			if strings.TrimSpace(content) != "" {
				chunk := models.CodeChunk{
					ID:        uuid.New().String(),
					RepoPath:  repoPath,
					FilePath:  filePath,
					ChunkType: models.ChunkTypeFunction,
					Content:   content,
					Language:  language,
					StartLine: chunkStartLine + 1,
					EndLine:   chunkStartLine + len(currentChunk),
				}
				chunks = append(chunks, chunk)
			}

			// Create overlap for next chunk
			overlapStart := len(currentChunk) - overlap
			if overlapStart < 0 {
				overlapStart = 0
			}
			currentChunk = currentChunk[overlapStart:]
			chunkStartLine = chunkStartLine + len(currentChunk) - len(currentChunk)
			if overlapStart > 0 {
				chunkStartLine += overlapStart
			}
		}
	}

	// Add remaining chunk
	if len(currentChunk) > 0 {
		content := strings.Join(currentChunk, "\n")
		if strings.TrimSpace(content) != "" {
			chunk := models.CodeChunk{
				ID:        uuid.New().String(),
				RepoPath:  repoPath,
				FilePath:  filePath,
				ChunkType: models.ChunkTypeFunction,
				Content:   content,
				Language:  language,
				StartLine: chunkStartLine + 1,
				EndLine:   len(lines),
			}
			chunks = append(chunks, chunk)
		}
	}

	return chunks
}

// getFunctionBoundaryPattern returns a regex pattern for detecting function/class boundaries
func getFunctionBoundaryPattern(language string) *regexp.Regexp {
	patterns := map[string]string{
		"java":       `^(public|private|protected)?\s*(static\s+)?(class|interface|enum|void|int|String|boolean|@)\s+\w+`,
		"javascript": `^(export\s+)?(async\s+)?(function|class|const|let|var)\s+\w+`,
		"typescript": `^(export\s+)?(async\s+)?(function|class|const|let|var|interface|type)\s+\w+`,
		"go":         `^(func|type|const|var)\s+\w+`,
	}

	pattern, ok := patterns[language]
	if !ok {
		return nil
	}

	regex, err := regexp.Compile(pattern)
	if err != nil {
		return nil
	}

	return regex
}

// parseFunctionChunks uses tree-sitter to extract function-level chunks
// TODO: Implement in Phase 2.5
func (c *Chunker) parseFunctionChunks(repoPath, filePath, language string, content []byte) ([]models.CodeChunk, error) {
	// This will be implemented when we integrate tree-sitter
	// For now, return empty slice
	return nil, nil
}

// GetStats returns statistics about chunking
func (c *Chunker) GetStats(chunks []models.CodeChunk) map[string]int {
	stats := map[string]int{
		"total":    len(chunks),
		"file":     0,
		"function": 0,
	}

	for _, chunk := range chunks {
		switch chunk.ChunkType {
		case models.ChunkTypeFile:
			stats["file"]++
		case models.ChunkTypeFunction:
			stats["function"]++
		}
	}

	return stats
}

// readFileLines reads a file and returns its lines
func readFileLines(filePath string) ([]string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var lines []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return lines, nil
}
