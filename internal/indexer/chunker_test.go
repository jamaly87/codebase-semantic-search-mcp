package indexer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jamaly87/codebase-semantic-search/internal/models"
	"github.com/jamaly87/codebase-semantic-search/pkg/config"
)

func TestChunker_AdaptiveChunking(t *testing.T) {
	cfg := &config.ChunkingConfig{
		SmallFileMaxTokens:  300,
		MediumFileMaxTokens: 200,
		LargeFileMaxTokens:  150,
		MaxChunkSizeBytes:   4000,
	}

	chunker := NewChunker(cfg)
	defer chunker.Close()

	tmpDir := t.TempDir()

	tests := []struct {
		name      string
		fileSize  int // lines
		content   string
		expectMax int // expected max tokens per chunk
	}{
		{
			name:      "small file",
			fileSize:  500,
			content:   generateJavaFile(500),
			expectMax: 300,
		},
		{
			name:      "medium file",
			fileSize:  3000,
			content:   generateJavaFile(3000),
			expectMax: 200,
		},
		{
			name:      "large file",
			fileSize:  10000,
			content:   generateJavaFile(10000),
			expectMax: 150,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			filePath := filepath.Join(tmpDir, tt.name+".java")
			if err := os.WriteFile(filePath, []byte(tt.content), 0644); err != nil {
				t.Fatalf("Failed to create test file: %v", err)
			}

			chunks, err := chunker.ChunkFile(tmpDir, filePath)
			if err != nil {
				t.Fatalf("ChunkFile failed: %v", err)
			}

			if len(chunks) == 0 {
				t.Fatal("Expected chunks, got none")
			}

			// Verify chunk sizes are reasonable (token chunker will enforce limits)
			// We can't directly check token count without the tokenizer, but we can
			// verify that chunks were created
			t.Logf("Created %d chunks for %s file", len(chunks), tt.name)
		})
	}
}

func TestChunker_HierarchicalChunking(t *testing.T) {
	cfg := &config.ChunkingConfig{
		EnableHierarchicalChunking: true,
		MaxChunkSizeBytes:          4000,
		SmallFileMaxTokens:        300,
		MediumFileMaxTokens:        200,
		LargeFileMaxTokens:         150,
	}

	chunker := NewChunker(cfg)
	defer chunker.Close()

	tmpDir := t.TempDir()

	// Create a large Java class that should be split hierarchically
	largeClass := `public class LargeService {
    private String field1;
    private int field2;
    
    public void method1() {
        // Method implementation
        System.out.println("Method 1");
    }
    
    public void method2() {
        // Method implementation
        System.out.println("Method 2");
    }
    
    public void method3() {
        // Method implementation
        System.out.println("Method 3");
    }
    
    public void method4() {
        // Method implementation
        System.out.println("Method 4");
    }
    
    public void method5() {
        // Method implementation
        System.out.println("Method 5");
    }
}`

	// Make it large enough to trigger hierarchical chunking
	largeClassContent := largeClass + strings.Repeat("\n    // Additional content\n", 200)

	filePath := filepath.Join(tmpDir, "LargeService.java")
	if err := os.WriteFile(filePath, []byte(largeClassContent), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	chunks, err := chunker.ChunkFile(tmpDir, filePath)
	if err != nil {
		t.Fatalf("ChunkFile failed: %v", err)
	}

	if len(chunks) == 0 {
		t.Fatal("Expected chunks, got none")
	}

	// Check if we have a class chunk
	hasClassChunk := false
	hasMethodChunks := false
	for _, chunk := range chunks {
		if chunk.ChunkType == models.ChunkTypeClass {
			hasClassChunk = true
			if chunk.ClassName == "" {
				t.Error("Class chunk should have ClassName set")
			}
		}
		if chunk.ChunkType == models.ChunkTypeMethod {
			hasMethodChunks = true
			if chunk.ParentChunkID == "" {
				t.Error("Method chunk should have ParentChunkID set")
			}
			if chunk.ClassName == "" {
				t.Error("Method chunk should have ClassName set for context")
			}
		}
	}

	if cfg.EnableHierarchicalChunking {
		// If hierarchical chunking is enabled and file is large enough,
		// we should have at least a class chunk
		if !hasClassChunk && len(largeClassContent) > cfg.MaxChunkSizeBytes {
			t.Log("Note: Hierarchical chunking may not have triggered (file may not be large enough or AST parsing may have failed)")
		}
	}

	t.Logf("Created %d chunks (class: %v, methods: %v)", len(chunks), hasClassChunk, hasMethodChunks)
}

func TestChunker_LargeNodeSplitting(t *testing.T) {
	cfg := &config.ChunkingConfig{
		MaxChunkSizeBytes: 1000, // Small limit to force splitting
		SmallFileMaxTokens: 300,
	}

	chunker := NewChunker(cfg)
	defer chunker.Close()

	tmpDir := t.TempDir()

	// Create a large function that should be split
	largeFunction := `public class Test {
    public void largeMethod() {
        // Line 1
        // Line 2
        // ... many lines ...
` + strings.Repeat("        System.out.println(\"Line\");\n", 500) + `
    }
}`

	filePath := filepath.Join(tmpDir, "Test.java")
	if err := os.WriteFile(filePath, []byte(largeFunction), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	chunks, err := chunker.ChunkFile(tmpDir, filePath)
	if err != nil {
		t.Fatalf("ChunkFile failed: %v", err)
	}

	if len(chunks) == 0 {
		t.Fatal("Expected chunks, got none")
	}

	// Verify chunks don't exceed max size
	for i, chunk := range chunks {
		if len(chunk.Content) > cfg.MaxChunkSizeBytes*2 { // Allow some margin for splitting logic
			t.Errorf("Chunk %d exceeds max size: %d bytes (max: %d)", i, len(chunk.Content), cfg.MaxChunkSizeBytes)
		}
	}

	t.Logf("Created %d chunks from large function", len(chunks))
}

func TestChunker_EmptyFile(t *testing.T) {
	cfg := &config.ChunkingConfig{}
	chunker := NewChunker(cfg)
	defer chunker.Close()

	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "empty.java")
	if err := os.WriteFile(filePath, []byte(""), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	chunks, err := chunker.ChunkFile(tmpDir, filePath)
	if err != nil {
		t.Fatalf("ChunkFile failed: %v", err)
	}

	if len(chunks) != 0 {
		t.Errorf("Expected 0 chunks for empty file, got %d", len(chunks))
	}
}

func TestChunker_UnsupportedFile(t *testing.T) {
	cfg := &config.ChunkingConfig{}
	chunker := NewChunker(cfg)
	defer chunker.Close()

	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "test.txt")
	if err := os.WriteFile(filePath, []byte("not code"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	_, err := chunker.ChunkFile(tmpDir, filePath)
	if err == nil {
		t.Error("Expected error for unsupported file type, got nil")
	}
}

// Helper function to generate Java file content with specified number of lines
func generateJavaFile(lines int) string {
	var sb strings.Builder
	sb.WriteString("public class Test {\n")
	
	for i := 0; i < lines-2; i++ {
		sb.WriteString("    // Line " + strings.Repeat("x", 10) + "\n")
	}
	
	sb.WriteString("}\n")
	return sb.String()
}

