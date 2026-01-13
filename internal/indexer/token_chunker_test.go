package indexer

import (
	"strings"
	"testing"
)

func TestTokenChunker_SetLimits(t *testing.T) {
	chunker, err := NewTokenChunker(200, 20)
	if err != nil {
		t.Fatalf("Failed to create token chunker: %v", err)
	}

	// Test setting new limits
	err = chunker.SetLimits(300, 30)
	if err != nil {
		t.Fatalf("SetLimits failed: %v", err)
	}

	maxTokens, overlap := chunker.GetLimits()
	if maxTokens != 300 {
		t.Errorf("Expected maxTokens=300, got %d", maxTokens)
	}
	if overlap != 30 {
		t.Errorf("Expected overlap=30, got %d", overlap)
	}
}

func TestTokenChunker_SetLimits_Invalid(t *testing.T) {
	chunker, err := NewTokenChunker(200, 20)
	if err != nil {
		t.Fatalf("Failed to create token chunker: %v", err)
	}

	tests := []struct {
		name      string
		maxTokens int
		overlap   int
		expectErr bool
	}{
		{"zero maxTokens", 0, 20, true},
		{"negative overlap", 200, -1, true},
		{"overlap >= maxTokens", 200, 200, true},
		{"overlap > maxTokens", 200, 250, true},
		{"valid limits", 300, 30, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := chunker.SetLimits(tt.maxTokens, tt.overlap)
			if tt.expectErr && err == nil {
				t.Errorf("Expected error for %s, got nil", tt.name)
			}
			if !tt.expectErr && err != nil {
				t.Errorf("Unexpected error for %s: %v", tt.name, err)
			}
		})
	}
}

func TestTokenChunker_AdaptiveChunking(t *testing.T) {
	chunker, err := NewTokenChunker(200, 20)
	if err != nil {
		t.Fatalf("Failed to create token chunker: %v", err)
	}

	// Test with different limits
	testCases := []struct {
		name      string
		maxTokens int
		overlap   int
		content   string
	}{
		{
			name:      "small file limits",
			maxTokens: 300,
			overlap:   20,
			content:   generateTestContent(100),
		},
		{
			name:      "medium file limits",
			maxTokens: 200,
			overlap:   20,
			content:   generateTestContent(500),
		},
		{
			name:      "large file limits",
			maxTokens: 150,
			overlap:   20,
			content:   generateTestContent(1000),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if err := chunker.SetLimits(tc.maxTokens, tc.overlap); err != nil {
				t.Fatalf("SetLimits failed: %v", err)
			}

			chunks, err := chunker.ChunkByTokens("/repo", "/file.java", "java", tc.content)
			if err != nil {
				t.Fatalf("ChunkByTokens failed: %v", err)
			}

			if len(chunks) == 0 {
				t.Fatal("Expected chunks, got none")
			}

			// Verify chunks respect the limits (approximately)
			// Note: exact token count depends on tokenizer, so we just check that chunks were created
			t.Logf("Created %d chunks with maxTokens=%d, overlap=%d", len(chunks), tc.maxTokens, tc.overlap)
		})
	}
}

func TestTokenChunker_OverlapCalculation(t *testing.T) {
	chunker, err := NewTokenChunker(200, 20)
	if err != nil {
		t.Fatalf("Failed to create token chunker: %v", err)
	}

	lines := []string{
		"public class Test {",
		"    private int field;",
		"    public void method() {",
		"        System.out.println(\"test\");",
		"    }",
		"}",
	}

	// Test overlap calculation with different overlap values
	overlapLines1 := chunker.calculateOverlapLines(lines, 10)
	overlapLines2 := chunker.calculateOverlapLines(lines, 50)

	if len(overlapLines1) >= len(overlapLines2) {
		t.Error("Expected smaller overlap for smaller token count")
	}

	// Overlap should not exceed original lines
	if len(overlapLines1) > len(lines) {
		t.Errorf("Overlap lines (%d) should not exceed original lines (%d)", len(overlapLines1), len(lines))
	}

	// Verify that overlap doesn't significantly exceed the requested token count
	actualTokens1 := chunker.countTokens(strings.Join(overlapLines1, "\n"))
	maxAllowed1 := int(float64(10) * maxOverlapExcessRatio)
	if actualTokens1 > maxAllowed1 {
		t.Errorf("Overlap tokens (%d) should not significantly exceed requested tokens (10), max allowed is %d", actualTokens1, maxAllowed1)
	}

	actualTokens2 := chunker.countTokens(strings.Join(overlapLines2, "\n"))
	maxAllowed2 := int(float64(50) * maxOverlapExcessRatio)
	if actualTokens2 > maxAllowed2 {
		t.Errorf("Overlap tokens (%d) should not significantly exceed requested tokens (50), max allowed is %d", actualTokens2, maxAllowed2)
	}
}

func TestTokenChunker_EmptyContent(t *testing.T) {
	chunker, err := NewTokenChunker(200, 20)
	if err != nil {
		t.Fatalf("Failed to create token chunker: %v", err)
	}

	chunks, err := chunker.ChunkByTokens("/repo", "/file.java", "java", "")
	if err != nil {
		t.Fatalf("ChunkByTokens failed: %v", err)
	}

	if len(chunks) != 0 {
		t.Errorf("Expected 0 chunks for empty content, got %d", len(chunks))
	}
}

func TestTokenChunker_OverlapWithLargeLine(t *testing.T) {
	chunker, err := NewTokenChunker(200, 20)
	if err != nil {
		t.Fatalf("Failed to create token chunker: %v", err)
	}

	// Create lines where one line has many more tokens than the overlap limit
	lines := []string{
		"short line",
		"another short line",
		"// This is a very long comment line that will have significantly more tokens than the overlap limit of 10 tokens, potentially exceeding it by a large margin",
	}

	// Request small overlap (10 tokens)
	overlapLines := chunker.calculateOverlapLines(lines, 10)

	// Should still include at least one line even if it exceeds the limit
	if len(overlapLines) == 0 {
		t.Error("Expected at least one line in overlap, got none")
	}

	// The overlap should not be excessive - in this case it should be just the last line
	// since adding the second-to-last line would exceed the 20% threshold
	actualTokens := chunker.countTokens(strings.Join(overlapLines, "\n"))
	maxAllowed := int(float64(10) * maxOverlapExcessRatio)
	if actualTokens > maxAllowed && len(overlapLines) > 1 {
		// If we have more than one line AND we exceeded the threshold, that's a problem
		t.Errorf("Overlap tokens (%d) exceeded threshold (max %d) with %d lines", actualTokens, maxAllowed, len(overlapLines))
	}
}

// Helper function to generate test content
func generateTestContent(lines int) string {
	var sb strings.Builder
	for i := 0; i < lines; i++ {
		sb.WriteString("// Line " + strings.Repeat("x", 10) + "\n")
	}
	return sb.String()
}

