package embeddings

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/jamaly87/codebase-semantic-search/pkg/config"
)

// Client handles communication with Ollama for embeddings
type Client struct {
	config     *config.EmbeddingsConfig
	httpClient *http.Client
	baseURL    string
}

// NewClient creates a new Ollama embeddings client
func NewClient(cfg *config.EmbeddingsConfig) *Client {
	return &Client{
		config:  cfg,
		baseURL: cfg.OllamaURL,
		httpClient: &http.Client{
			Timeout: 60 * time.Second, // Generous timeout for large batches
		},
	}
}

// EmbedRequest represents a request to generate embeddings
type EmbedRequest struct {
	Model  string `json:"model"`
	Prompt string `json:"prompt"`
}

// EmbedResponse represents the response from Ollama
type EmbedResponse struct {
	Embedding []float32 `json:"embedding"`
}

// GenerateEmbedding generates an embedding for a single text
func (c *Client) GenerateEmbedding(text string) ([]float32, error) {
	// Truncate text if it exceeds safe length
	// nomic-embed-text has 8192 token limit
	// Use conservative 6000 chars to stay well under token limit
	maxChars := 6000
	if len(text) > maxChars {
		text = text[:maxChars]
	}

	request := EmbedRequest{
		Model:  c.config.Model,
		Prompt: text,
	}

	reqBody, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	url := fmt.Sprintf("%s/api/embeddings", c.baseURL)
	req, err := http.NewRequest("POST", url, bytes.NewBuffer(reqBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("ollama returned status %d: %s", resp.StatusCode, string(body))
	}

	var response EmbedResponse
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	if len(response.Embedding) != c.config.Dimensions {
		return nil, fmt.Errorf("expected %d dimensions, got %d", c.config.Dimensions, len(response.Embedding))
	}

	// Normalize if configured
	if c.config.Normalize {
		response.Embedding = normalize(response.Embedding)
	}

	return response.Embedding, nil
}

// GenerateEmbeddings generates embeddings for multiple texts (batch)
func (c *Client) GenerateEmbeddings(texts []string) ([][]float32, error) {
	embeddings := make([][]float32, len(texts))

	for i, text := range texts {
		embedding, err := c.GenerateEmbedding(text)
		if err != nil {
			return nil, fmt.Errorf("failed to generate embedding for item %d: %w", i, err)
		}
		embeddings[i] = embedding
	}

	return embeddings, nil
}

// HealthCheck checks if Ollama is available and the model is loaded
func (c *Client) HealthCheck() error {
	// Try to generate a simple embedding
	_, err := c.GenerateEmbedding("test")
	if err != nil {
		return fmt.Errorf("ollama health check failed: %w", err)
	}
	return nil
}

// normalize performs L2 normalization on a vector
func normalize(vec []float32) []float32 {
	var sum float32
	for _, v := range vec {
		sum += v * v
	}

	if sum == 0 {
		return vec
	}

	magnitude := float32(1.0) / float32(sqrt64(float64(sum)))

	normalized := make([]float32, len(vec))
	for i, v := range vec {
		normalized[i] = v * magnitude
	}

	return normalized
}

// sqrt64 is a helper function for square root
func sqrt64(x float64) float64 {
	if x < 0 {
		return 0
	}

	// Newton's method for square root
	z := x
	for i := 0; i < 10; i++ {
		z = z - (z*z-x)/(2*z)
	}
	return z
}
