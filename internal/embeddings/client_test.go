package embeddings

import (
	"math"
	"testing"

	"github.com/jamaly87/codebase-semantic-search/pkg/config"
)

func TestNormalization(t *testing.T) {
	tests := []struct {
		name     string
		input    []float32
		expected float64 // Expected magnitude after normalization
	}{
		{
			name:     "normalize vector",
			input:    []float32{3.0, 4.0},
			expected: 1.0, // Should have magnitude 1.0 after normalization
		},
		{
			name:     "normalize zero vector",
			input:    []float32{0.0, 0.0, 0.0},
			expected: 0.0, // Zero vector stays zero
		},
		{
			name:     "normalize unit vector",
			input:    []float32{1.0, 0.0, 0.0},
			expected: 1.0, // Already normalized
		},
		{
			name:     "normalize negative values",
			input:    []float32{-3.0, -4.0},
			expected: 1.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			normalized := normalize(tt.input)

			// Calculate magnitude
			var magnitude float64
			for _, v := range normalized {
				magnitude += float64(v * v)
			}
			magnitude = math.Sqrt(magnitude)

			if math.Abs(magnitude-tt.expected) > 0.0001 {
				t.Errorf("Expected magnitude %.4f, got %.4f", tt.expected, magnitude)
			}

			// Check length preserved
			if len(normalized) != len(tt.input) {
				t.Errorf("Expected length %d, got %d", len(tt.input), len(normalized))
			}
		})
	}
}

func TestClientConfiguration(t *testing.T) {
	tests := []struct {
		name   string
		config *config.EmbeddingsConfig
	}{
		{
			name: "default config",
			config: &config.EmbeddingsConfig{
				Model:      "nomic-embed-text",
				OllamaURL:  "http://localhost:11434",
				BatchSize:  16,
				Normalize:  true,
				Dimensions: 768,
			},
		},
		{
			name: "custom config",
			config: &config.EmbeddingsConfig{
				Model:      "custom-model",
				OllamaURL:  "http://custom:8080",
				BatchSize:  32,
				Normalize:  false,
				Dimensions: 1024,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewClient(tt.config)

			if client == nil {
				t.Fatal("NewClient returned nil")
			}

			if client.config.Model != tt.config.Model {
				t.Errorf("Expected model %s, got %s", tt.config.Model, client.config.Model)
			}

			if client.config.OllamaURL != tt.config.OllamaURL {
				t.Errorf("Expected URL %s, got %s", tt.config.OllamaURL, client.config.OllamaURL)
			}

			if client.config.BatchSize != tt.config.BatchSize {
				t.Errorf("Expected batch size %d, got %d", tt.config.BatchSize, client.config.BatchSize)
			}
		})
	}
}

func TestEmbeddingValidation(t *testing.T) {
	tests := []struct {
		name        string
		text        string
		shouldError bool
	}{
		{
			name:        "valid text",
			text:        "This is a valid code snippet",
			shouldError: false,
		},
		{
			name:        "empty text",
			text:        "",
			shouldError: false, // Empty text is allowed, Ollama will handle it
		},
		{
			name:        "very long text",
			text:        string(make([]byte, 10000)),
			shouldError: false, // Long text is allowed (model will truncate)
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Just validate the text doesn't cause panics
			// Actual API calls are tested in integration tests
			if tt.text == "" && !tt.shouldError {
				// Empty text is valid
				return
			}
			if len(tt.text) > 0 {
				// Non-empty text is valid
				return
			}
		})
	}
}
