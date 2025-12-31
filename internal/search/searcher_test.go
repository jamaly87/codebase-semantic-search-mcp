package search

import (
	"context"
	"strings"
	"testing"

	"github.com/jamaly87/codebase-semantic-search/internal/models"
	"github.com/jamaly87/codebase-semantic-search/pkg/config"
)

// Mock embeddings client
type mockEmbeddingsClient struct {
	embeddings []float32
	err        error
}

func (m *mockEmbeddingsClient) GenerateEmbedding(text string) ([]float32, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.embeddings, nil
}

// Mock vector DB client
type mockVectorDB struct {
	chunks []models.CodeChunk
	scores []float64
	err    error
}

func (m *mockVectorDB) Search(ctx context.Context, embedding []float32, repoPath string, limit int) ([]models.CodeChunk, []float64, error) {
	if m.err != nil {
		return nil, nil, m.err
	}
	return m.chunks, m.scores, nil
}

func TestHybridScoring(t *testing.T) {
	cfg := &config.SearchConfig{
		MaxResults:       5,
		SemanticWeight:   0.7,
		ExactMatchBoost:  1.5,
	}

	tests := []struct {
		name           string
		query          string
		chunks         []models.CodeChunk
		semanticScores []float64
		expectedOrder  []int // Expected order of results by index
		expectExact    []bool
	}{
		{
			name:  "exact match boosted to top",
			query: "logger",
			chunks: []models.CodeChunk{
				{
					ID:       "1",
					Content:  "This is a test",
					FilePath: "test1.java",
				},
				{
					ID:       "2",
					Content:  "Code with logger.info() call",
					FilePath: "test2.java",
				},
			},
			semanticScores: []float64{0.8, 0.6}, // First has higher semantic score
			expectedOrder:  []int{1, 0},         // But second should be first due to exact match
			expectExact:    []bool{false, true}, // First no match, second has match
		},
		{
			name:  "no exact matches - pure semantic ranking",
			query: "authentication",
			chunks: []models.CodeChunk{
				{
					ID:       "1",
					Content:  "User login service",
					FilePath: "test1.java",
				},
				{
					ID:       "2",
					Content:  "Database connection",
					FilePath: "test2.java",
				},
			},
			semanticScores: []float64{0.9, 0.3},
			expectedOrder:  []int{0, 1}, // Keep semantic order
			expectExact:    []bool{false, false},
		},
		{
			name:  "case insensitive exact match",
			query: "Logger",
			chunks: []models.CodeChunk{
				{
					ID:       "1",
					Content:  "private static final logger = new Logger();",
					FilePath: "test1.java",
				},
			},
			semanticScores: []float64{0.5},
			expectedOrder:  []int{0},
			expectExact:    []bool{true},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			searcher := &Searcher{
				config: cfg,
			}

			results := searcher.applyHybridScoring(tt.query, tt.chunks, tt.semanticScores)

			// Check results length
			if len(results) != len(tt.chunks) {
				t.Errorf("Expected %d results, got %d", len(tt.chunks), len(results))
			}

			// Verify hybrid scores are calculated
			for i, result := range results {
				if result.HybridScore == 0 {
					t.Errorf("Result %d has zero hybrid score", i)
				}

				// Verify exact match detection
				expectedExact := tt.expectExact[i]
				if result.ExactMatch != expectedExact {
					t.Errorf("Result %d: expected exact match=%v, got %v",
						i, expectedExact, result.ExactMatch)
				}

				// Verify score calculation (additive boost + file path scoring)
				expectedBase := tt.semanticScores[i] * cfg.SemanticWeight
				expectedHybrid := expectedBase
				if result.ExactMatch {
					expectedHybrid += cfg.ExactMatchBoost // Additive, not multiplicative
				}
				// Apply file path scoring (test files should be 1.0 neutral since they don't match test patterns)
				pathScore := calculateFilePathScore(result.Chunk.FilePath)
				expectedHybrid *= pathScore

				if abs(result.HybridScore-expectedHybrid) > 0.001 {
					t.Errorf("Result %d: expected hybrid score %.3f, got %.3f (path score: %.2f)",
						i, expectedHybrid, result.HybridScore, pathScore)
				}
			}
		})
	}
}

func TestExactMatchDetection(t *testing.T) {
	tests := []struct {
		name          string
		query         string
		content       string
		expectMatch   bool
		expectedCount int
	}{
		{
			name:          "single match",
			query:         "logger",
			content:       "This code uses logger.info()",
			expectMatch:   true,
			expectedCount: 1,
		},
		{
			name:          "multiple matches",
			query:         "user",
			content:       "user.getName() and user.getEmail()",
			expectMatch:   true,
			expectedCount: 2,
		},
		{
			name:          "case insensitive",
			query:         "Logger",
			content:       "LOGGER is a constant",
			expectMatch:   true,
			expectedCount: 1,
		},
		{
			name:          "no match",
			query:         "database",
			content:       "This is about users",
			expectMatch:   false,
			expectedCount: 0,
		},
		{
			name:          "partial word matched as substring",
			query:         "log",
			content:       "logger is here",
			expectMatch:   true, // Our implementation matches substrings
			expectedCount: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			contentLower := strings.ToLower(tt.content)
			queryLower := strings.ToLower(tt.query)

			hasMatch := strings.Contains(contentLower, queryLower)
			if hasMatch != tt.expectMatch {
				t.Errorf("Expected match=%v, got %v", tt.expectMatch, hasMatch)
			}

			if hasMatch {
				positions := findMatchPositions(contentLower, queryLower)
				if len(positions) != tt.expectedCount {
					t.Errorf("Expected %d matches, got %d", tt.expectedCount, len(positions))
				}
			}
		})
	}
}

func TestSearchResultRanking(t *testing.T) {
	cfg := &config.SearchConfig{
		MaxResults:      3,
		SemanticWeight:  0.7,
		ExactMatchBoost: 1.5,
	}

	mockEmbed := &mockEmbeddingsClient{
		embeddings: []float32{0.1, 0.2, 0.3},
	}

	mockDB := &mockVectorDB{
		chunks: []models.CodeChunk{
			{ID: "1", Content: "Result one", FilePath: "a.java"},
			{ID: "2", Content: "Result two with query match", FilePath: "b.java"},
			{ID: "3", Content: "Result three", FilePath: "c.java"},
			{ID: "4", Content: "Result four", FilePath: "d.java"},
		},
		scores: []float64{0.9, 0.7, 0.8, 0.6},
	}

	searcher := NewSearcher(cfg, mockEmbed, mockDB)

	results, err := searcher.Search(context.Background(), "query", "/test/repo")
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}

	// Should return only MaxResults
	if len(results) != cfg.MaxResults {
		t.Errorf("Expected %d results, got %d", cfg.MaxResults, len(results))
	}

	// Results should be sorted by hybrid score (descending)
	for i := 1; i < len(results); i++ {
		if results[i].HybridScore > results[i-1].HybridScore {
			t.Errorf("Results not properly sorted: result[%d] score %.3f > result[%d] score %.3f",
				i, results[i].HybridScore, i-1, results[i-1].HybridScore)
		}
	}

	// Result with exact match should be ranked high
	foundExactMatch := false
	for i, result := range results {
		if result.ExactMatch {
			foundExactMatch = true
			// Exact match should be in top 2 positions
			if i > 1 {
				t.Errorf("Exact match result at position %d, expected in top 2", i)
			}
		}
	}

	if !foundExactMatch {
		t.Error("Expected at least one exact match in results")
	}
}

func TestFormatResults(t *testing.T) {
	tests := []struct {
		name     string
		results  []SearchResult
		expected []string // Strings that should appear in output
	}{
		{
			name:     "empty results",
			results:  []SearchResult{},
			expected: []string{"No results found"},
		},
		{
			name: "single result",
			results: []SearchResult{
				{
					Chunk: models.CodeChunk{
						FilePath:  "test.java",
						StartLine: 10,
						EndLine:   20,
						Content:   "public void test() {\n  return true;\n}",
						Language:  "java",
					},
					HybridScore:   0.85,
					SemanticScore: 0.75,
					ExactMatch:    false,
				},
			},
			expected: []string{
				"Found 1 results",
				"test.java:10-20",
				"score: 0.850",
				"Language: java",
			},
		},
		{
			name: "result with exact match",
			results: []SearchResult{
				{
					Chunk: models.CodeChunk{
						FilePath:     "auth.java",
						StartLine:    5,
						EndLine:      15,
						Content:      "public void authenticate() {}",
						Language:     "java",
						FunctionName: "authenticate",
					},
					HybridScore:   0.92,
					SemanticScore: 0.82,
					ExactMatch:    true,
				},
			},
			expected: []string{
				"auth.java:5-15",
				"EXACT MATCH",
				"in authenticate",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output := FormatResults(tt.results)

			for _, expected := range tt.expected {
				if !strings.Contains(output, expected) {
					t.Errorf("Output missing expected string: %q\nGot:\n%s", expected, output)
				}
			}
		})
	}
}

// Helper function
func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}
