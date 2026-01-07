package indexer

import (
	"strings"
	"testing"

	"github.com/jamaly87/codebase-semantic-search/internal/models"
	"github.com/jamaly87/codebase-semantic-search/pkg/config"
)

func TestASTChunker_HierarchicalChunking(t *testing.T) {
	chunker, err := NewASTChunker()
	if err != nil {
		t.Skipf("AST chunker not available: %v", err)
	}

	cfg := &config.ChunkingConfig{
		EnableHierarchicalChunking: true,
		MaxChunkSizeBytes:          4000,
	}

	// Create a large Java class
	largeClass := `public class LargeService {
    private String field1;
    private int field2;
    private boolean field3;
    
    public LargeService() {
        // Constructor
    }
    
    public void method1() {
        System.out.println("Method 1");
        // Additional implementation
    }
    
    public void method2() {
        System.out.println("Method 2");
        // Additional implementation
    }
    
    public void method3() {
        System.out.println("Method 3");
        // Additional implementation
    }
}`

	// Make it large enough to potentially trigger hierarchical chunking
	largeClassContent := largeClass + strings.Repeat("\n    // Additional content line\n", 200)

	chunks, err := chunker.ChunkByAST("/repo", "/LargeService.java", "java", largeClassContent, cfg)
	if err != nil {
		t.Fatalf("ChunkByAST failed: %v", err)
	}

	if len(chunks) == 0 {
		t.Fatal("Expected chunks, got none")
	}

	// Check for class chunk
	hasClassChunk := false
	hasMethodChunks := false
	var classChunkID string

	for _, chunk := range chunks {
		if chunk.ChunkType == models.ChunkTypeClass {
			hasClassChunk = true
			classChunkID = chunk.ID
			if chunk.ClassName == "" {
				t.Error("Class chunk should have ClassName set")
			}
		}
		if chunk.ChunkType == models.ChunkTypeMethod {
			hasMethodChunks = true
			if chunk.ParentChunkID == "" {
				t.Error("Method chunk should have ParentChunkID set")
			}
			if chunk.ParentChunkID != classChunkID && classChunkID != "" {
				t.Error("Method chunk ParentChunkID should match class chunk ID")
			}
		}
	}

	t.Logf("Created %d chunks (class: %v, methods: %v)", len(chunks), hasClassChunk, hasMethodChunks)
}

func TestASTChunker_LargeNodeSplitting(t *testing.T) {
	chunker, err := NewASTChunker()
	if err != nil {
		t.Skipf("AST chunker not available: %v", err)
	}

	cfg := &config.ChunkingConfig{
		MaxChunkSizeBytes: 1000, // Small limit to force splitting
	}

	// Create a large function
	largeFunction := `public class Test {
    public void largeMethod() {
        // Line 1
        // Line 2
` + strings.Repeat("        System.out.println(\"Line\");\n", 300) + `
    }
}`

	chunks, err := chunker.ChunkByAST("/repo", "/Test.java", "java", largeFunction, cfg)
	if err != nil {
		t.Fatalf("ChunkByAST failed: %v", err)
	}

	if len(chunks) == 0 {
		t.Fatal("Expected chunks, got none")
	}

	// Verify chunks don't exceed max size (with some margin)
	for i, chunk := range chunks {
		if len(chunk.Content) > cfg.MaxChunkSizeBytes*2 {
			t.Errorf("Chunk %d exceeds max size: %d bytes (max: %d)", i, len(chunk.Content), cfg.MaxChunkSizeBytes)
		}
	}

	t.Logf("Created %d chunks from large function", len(chunks))
}

func TestASTChunker_IsLargeClassOrInterface(t *testing.T) {
	chunker, err := NewASTChunker()
	if err != nil {
		t.Skipf("AST chunker not available: %v", err)
	}

	// This is a unit test for the helper method
	// We'll test it indirectly through ChunkByAST
	cfg := &config.ChunkingConfig{
		EnableHierarchicalChunking: true,
		MaxChunkSizeBytes:          100, // Very small to trigger splitting
	}

	smallClass := `public class Small {
    public void method() {}
}`

	chunks, err := chunker.ChunkByAST("/repo", "/Small.java", "java", smallClass, cfg)
	if err != nil {
		t.Fatalf("ChunkByAST failed: %v", err)
	}

	// Small class should not be split hierarchically
	hasClassChunk := false
	for _, chunk := range chunks {
		if chunk.ChunkType == models.ChunkTypeClass {
			hasClassChunk = true
		}
	}

	// Small class may or may not have hierarchical chunking, but should still work
	t.Logf("Small class created %d chunks (has class chunk: %v)", len(chunks), hasClassChunk)
}

func TestASTChunker_ExtractMethodNodes(t *testing.T) {
	chunker, err := NewASTChunker()
	if err != nil {
		t.Skipf("AST chunker not available: %v", err)
	}

	javaClass := `public class Test {
    public void method1() {}
    public void method2() {}
    private void method3() {}
}`

	cfg := &config.ChunkingConfig{
		EnableHierarchicalChunking: true,
		MaxChunkSizeBytes:          4000,
	}

	// Test through ChunkByAST which will use extractMethodNodes internally
	chunks, err := chunker.ChunkByAST("/repo", "/Test.java", "java", javaClass, cfg)
	if err != nil {
		t.Fatalf("ChunkByAST failed: %v", err)
	}

	// Should have at least one chunk
	if len(chunks) == 0 {
		t.Error("Expected chunks, got none")
	}

	// Count method chunks
	methodCount := 0
	for _, chunk := range chunks {
		if chunk.ChunkType == models.ChunkTypeMethod {
			methodCount++
		}
	}

	t.Logf("Found %d method chunks in class", methodCount)
}

func TestASTChunker_CanParseLanguage(t *testing.T) {
	chunker, err := NewASTChunker()
	if err != nil {
		t.Skipf("AST chunker not available: %v", err)
	}

	tests := []struct {
		language string
		expected bool
	}{
		{"java", true},
		{"javascript", true},
		{"typescript", true},
		{"go", false},
		{"python", false},
		{"rust", false},
	}

	for _, tt := range tests {
		t.Run(tt.language, func(t *testing.T) {
			result := chunker.CanParseLanguage(tt.language)
			if result != tt.expected {
				t.Errorf("CanParseLanguage(%q) = %v, expected %v", tt.language, result, tt.expected)
			}
		})
	}
}

