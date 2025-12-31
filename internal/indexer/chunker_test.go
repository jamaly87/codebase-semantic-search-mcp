package indexer

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/jamaly87/codebase-semantic-search/internal/models"
	"github.com/jamaly87/codebase-semantic-search/pkg/config"
)

func TestChunkFile(t *testing.T) {
	// Create temporary test file
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.java")

	content := `package com.test;

public class Test {
    public void method1() {
        // Line 5
        int x = 1;
        int y = 2;
        return x + y;
    }

    public void method2() {
        // Line 12
        String s = "test";
        return s;
    }
}
`

	err := os.WriteFile(testFile, []byte(content), 0644)
	if err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	cfg := &config.ChunkingConfig{
		MaxLines:     10,
		OverlapLines: 2,
	}

	chunker := NewChunker(cfg)

	chunks, err := chunker.ChunkFile(tmpDir, testFile)
	if err != nil {
		t.Fatalf("ChunkFile failed: %v", err)
	}

	// Should have at least 1 file-level chunk
	if len(chunks) < 1 {
		t.Errorf("Expected at least 1 chunk, got %d", len(chunks))
	}

	// Verify file-level chunk exists
	foundFileChunk := false
	for _, chunk := range chunks {
		if chunk.ChunkType == models.ChunkTypeFile {
			foundFileChunk = true

			// Check metadata
			if chunk.FilePath != testFile {
				t.Errorf("Expected file path %s, got %s", testFile, chunk.FilePath)
			}

			if chunk.Language != "java" {
				t.Errorf("Expected language java, got %s", chunk.Language)
			}

			if chunk.StartLine != 1 {
				t.Errorf("Expected start line 1, got %d", chunk.StartLine)
			}

			// Content should not be empty
			if len(chunk.Content) == 0 {
				t.Error("File chunk content is empty")
			}
		}
	}

	if !foundFileChunk {
		t.Error("No file-level chunk found")
	}
}

func TestChunkTypes(t *testing.T) {
	cfg := &config.ChunkingConfig{
		MaxLines:     5,
		OverlapLines: 1,
	}

	chunker := NewChunker(cfg)

	// Create test file with multiple lines
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.java")

	lines := ""
	for i := 1; i <= 20; i++ {
		lines += "// Line " + string(rune(i)) + "\n"
	}

	err := os.WriteFile(testFile, []byte(lines), 0644)
	if err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	chunks, err := chunker.ChunkFile(tmpDir, testFile)
	if err != nil {
		t.Fatalf("ChunkFile failed: %v", err)
	}

	// Should have file chunk + line chunks
	if len(chunks) < 2 {
		t.Errorf("Expected multiple chunks, got %d", len(chunks))
	}

	// Verify chunk types
	hasFileChunk := false
	hasFunctionChunk := false

	for _, chunk := range chunks {
		switch chunk.ChunkType {
		case models.ChunkTypeFile:
			hasFileChunk = true
		case models.ChunkTypeFunction:
			hasFunctionChunk = true
		}
	}

	if !hasFileChunk {
		t.Error("Missing file-level chunk")
	}

	if !hasFunctionChunk {
		t.Error("Missing function-level chunks")
	}
}

func TestLanguageDetection(t *testing.T) {
	tests := []struct {
		filename string
		expected string
	}{
		{"test.java", "java"},
		{"test.ts", "typescript"},
		{"test.tsx", "typescript"},
		{"test.js", "javascript"},
		{"test.jsx", "javascript"},
		{"test.mjs", "javascript"},
		{"test.cjs", "javascript"},
		{"test.txt", "unknown"},
		{"test", "unknown"},
	}

	cfg := &config.ChunkingConfig{
		MaxLines:     10,
		OverlapLines: 2,
	}

	chunker := NewChunker(cfg)

	tmpDir := t.TempDir()

	for _, tt := range tests {
		t.Run(tt.filename, func(t *testing.T) {
			testFile := filepath.Join(tmpDir, tt.filename)

			// Create minimal file
			err := os.WriteFile(testFile, []byte("test content\n"), 0644)
			if err != nil {
				t.Fatalf("Failed to create test file: %v", err)
			}

			chunks, err := chunker.ChunkFile(tmpDir, testFile)
			if err != nil {
				// Unknown language files might be skipped
				if tt.expected == "unknown" {
					return
				}
				t.Fatalf("ChunkFile failed: %v", err)
			}

			if len(chunks) > 0 {
				if chunks[0].Language != tt.expected {
					t.Errorf("Expected language %s, got %s", tt.expected, chunks[0].Language)
				}
			}
		})
	}
}

func TestOverlappingChunks(t *testing.T) {
	cfg := &config.ChunkingConfig{
		MaxLines:     5,
		OverlapLines: 2,
	}

	chunker := NewChunker(cfg)

	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.java")

	// Create file with 12 lines
	lines := ""
	for i := 1; i <= 12; i++ {
		lines += "// Line " + string(rune(i)) + "\n"
	}

	err := os.WriteFile(testFile, []byte(lines), 0644)
	if err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	chunks, err := chunker.ChunkFile(tmpDir, testFile)
	if err != nil {
		t.Fatalf("ChunkFile failed: %v", err)
	}

	// Find line-based chunks (not file chunk)
	lineChunks := []models.CodeChunk{}
	for _, chunk := range chunks {
		if chunk.ChunkType == models.ChunkTypeFunction {
			lineChunks = append(lineChunks, chunk)
		}
	}

	// Should have overlapping chunks
	if len(lineChunks) < 2 {
		t.Errorf("Expected multiple line chunks, got %d", len(lineChunks))
		return
	}

	// Verify overlap
	for i := 1; i < len(lineChunks); i++ {
		prevChunk := lineChunks[i-1]
		currChunk := lineChunks[i]

		// Current chunk should start before previous chunk ends
		// (with overlap)
		expectedStart := prevChunk.EndLine - cfg.OverlapLines + 1
		if currChunk.StartLine > expectedStart {
			t.Errorf("Chunk %d: expected overlap, start line %d > expected %d",
				i, currChunk.StartLine, expectedStart)
		}
	}
}

func TestEmptyFile(t *testing.T) {
	cfg := &config.ChunkingConfig{
		MaxLines:     10,
		OverlapLines: 2,
	}

	chunker := NewChunker(cfg)

	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "empty.java")

	err := os.WriteFile(testFile, []byte(""), 0644)
	if err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	chunks, err := chunker.ChunkFile(tmpDir, testFile)

	// Empty file should either error or return no chunks
	if err == nil && len(chunks) > 0 {
		// If it returns chunks, they should be valid
		for i, chunk := range chunks {
			if chunk.Content == "" && chunk.ChunkType != models.ChunkTypeFile {
				t.Errorf("Chunk %d has empty content", i)
			}
		}
	}
}
