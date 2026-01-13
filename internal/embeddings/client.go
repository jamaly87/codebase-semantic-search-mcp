package embeddings

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"sync"
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
	// Configure HTTP transport for optimal connection reuse and pooling
	transport := &http.Transport{
		MaxIdleConns:        100,              // Maximum idle connections across all hosts
		MaxIdleConnsPerHost: 100,              // Maximum idle connections per host (Ollama)
		MaxConnsPerHost:     100,              // Maximum total connections per host
		IdleConnTimeout:     90 * time.Second, // How long idle connections stay alive
		DisableKeepAlives:   false,            // Enable keep-alive (connection reuse)
		ForceAttemptHTTP2:   false,            // Stick with HTTP/1.1 for simplicity
	}

	client := &Client{
		config:  cfg,
		baseURL: cfg.OllamaURL,
		httpClient: &http.Client{
			Timeout:   60 * time.Second, // Generous timeout for large batches
			Transport: transport,
		},
	}

	// Log MRL configuration
	client.logMRLConfig()

	return client
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
	// nomic-embed-text has 8192 token limit (~4 chars per token)
	// Use very conservative 4000 chars (~1000 tokens) to ensure we never exceed
	// This is a safety net - chunker should already handle size limits
	maxChars := 4000
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

	// Validate we got the full dimension from the model
	fullDim := c.config.FullDimension
	if fullDim == 0 {
		fullDim = 768 // Default for nomic-embed-text
	}

	if len(response.Embedding) != fullDim {
		return nil, fmt.Errorf("expected %d dimensions from model, got %d", fullDim, len(response.Embedding))
	}

	embedding := response.Embedding

	// Apply MRL dimension truncation if enabled
	if c.config.UseMRL && c.config.Dimensions < fullDim {
		embedding = applyMRL(embedding, c.config.Dimensions)
	}

	// Normalize if configured (after MRL slicing)
	if c.config.Normalize {
		embedding = normalize(embedding)
	}

	return embedding, nil
}

// GenerateEmbeddings generates embeddings for multiple texts (batch)
// Uses concurrent requests with connection pooling for optimal performance
func (c *Client) GenerateEmbeddings(texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}

	// For single text, use the simple method
	if len(texts) == 1 {
		embedding, err := c.GenerateEmbedding(texts[0])
		if err != nil {
			return nil, err
		}
		return [][]float32{embedding}, nil
	}

	// Use concurrent requests with connection pooling for better performance
	// The http.Client with keep-alive will reuse connections
	// Create a context with cancellation to stop remaining work on first error
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	embeddings := make([][]float32, len(texts))
	errors := make([]error, len(texts))

	// Limit concurrency to avoid overwhelming Ollama
	// Use a semaphore to control concurrent requests
	maxConcurrent := 10 // Process up to 10 requests concurrently
	semaphore := make(chan struct{}, maxConcurrent)
	var wg sync.WaitGroup
	var firstError sync.Once

	for i, text := range texts {
		wg.Add(1)
		go func(idx int, txt string) {
			defer wg.Done()

			// Acquire semaphore with context cancellation check
			select {
			case semaphore <- struct{}{}:
			case <-ctx.Done():
				return
			}
			// Always release semaphore after successful acquisition
			defer func() { <-semaphore }()

			// Check context again before starting expensive work
			select {
			case <-ctx.Done():
				return
			default:
			}

			embedding, err := c.GenerateEmbedding(txt)
			if err != nil {
				errors[idx] = fmt.Errorf("failed to generate embedding for item %d: %w", idx, err)
				// Cancel context on first error to stop remaining goroutines
				firstError.Do(func() {
					cancel()
				})
				return
			}
			embeddings[idx] = embedding
		}(i, text)
	}

	wg.Wait()

	// Check for errors
	for i, err := range errors {
		if err != nil {
			return nil, fmt.Errorf("batch embedding failed at index %d: %w", i, err)
		}
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

// applyMRL applies Matryoshka Representation Learning dimension truncation
// This truncates the embedding to a smaller dimension while maintaining semantic meaning
// nomic-embed-text is trained with MRL, so dimensions 64, 128, 256, 512, 768 all work well
func applyMRL(embedding []float32, targetDim int) []float32 {
	// Validate target dimension
	validDims := []int{64, 128, 256, 512, 768}
	isValid := false
	for _, dim := range validDims {
		if targetDim == dim {
			isValid = true
			break
		}
	}

	if !isValid {
		// If invalid dimension, return closest valid one
		if targetDim < 64 {
			targetDim = 64
		} else if targetDim > 768 {
			targetDim = 768
		} else {
			// Round to nearest valid dimension
			for i := 0; i < len(validDims)-1; i++ {
				if targetDim > validDims[i] && targetDim < validDims[i+1] {
					// Choose closer one
					if targetDim-validDims[i] < validDims[i+1]-targetDim {
						targetDim = validDims[i]
					} else {
						targetDim = validDims[i+1]
					}
					break
				}
			}
		}
	}

	// Ensure we don't exceed embedding length
	if targetDim > len(embedding) {
		targetDim = len(embedding)
	}

	// Slice to target dimension
	// Note: Ideally we'd apply layer normalization before slicing, but since we're
	// receiving embeddings from Ollama post-generation, we can only slice and renormalize.
	// This still works well because nomic-embed-text is specifically trained for MRL.
	sliced := make([]float32, targetDim)
	copy(sliced, embedding[:targetDim])

	return sliced
}

// logMRLConfig logs the MRL configuration on client initialization
func (c *Client) logMRLConfig() {
	fullDim := c.config.FullDimension
	if fullDim == 0 {
		fullDim = 768
	}

	if c.config.UseMRL {
		reduction := float64(fullDim-c.config.Dimensions) / float64(fullDim) * 100
		log.Printf("✓ MRL Enabled: %dd → %dd (%.0f%% smaller, ~95%% accuracy)",
			fullDim, c.config.Dimensions, reduction)
	} else {
		log.Printf("MRL Disabled: Using full %dd embeddings", fullDim)
	}
}
