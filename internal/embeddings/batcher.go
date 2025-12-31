package embeddings

import (
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/jamaly87/codebase-semantic-search/internal/models"
)

// EmbeddingGenerator interface for generating embeddings
type EmbeddingGenerator interface {
	GenerateEmbedding(text string) ([]float32, error)
}

// Batcher handles batch processing of embeddings
type Batcher struct {
	client    EmbeddingGenerator
	batchSize int
	workers   int
}

// NewBatcher creates a new embedding batcher
func NewBatcher(client EmbeddingGenerator, batchSize, workers int) *Batcher {
	if workers <= 0 {
		workers = 1
	}
	return &Batcher{
		client:    client,
		batchSize: batchSize,
		workers:   workers,
	}
}

// ProcessChunks generates embeddings for a slice of code chunks
func (b *Batcher) ProcessChunks(chunks []models.CodeChunk) ([]models.CodeChunk, error) {
	if len(chunks) == 0 {
		return chunks, nil
	}

	log.Printf("Generating embeddings for %d chunks using %d workers...", len(chunks), b.workers)
	startTime := time.Now()

	// Create batches
	batches := b.createBatches(chunks)
	log.Printf("Split into %d batches of ~%d chunks each", len(batches), b.batchSize)

	// Process batches in parallel
	results := make([][]models.CodeChunk, len(batches))
	errors := make([]error, len(batches))

	var wg sync.WaitGroup
	semaphore := make(chan struct{}, b.workers)

	for i, batch := range batches {
		wg.Add(1)
		go func(idx int, batch []models.CodeChunk) {
			defer wg.Done()

			// Acquire semaphore
			semaphore <- struct{}{}
			defer func() { <-semaphore }()

			processed, err := b.processBatch(batch, idx)
			results[idx] = processed
			errors[idx] = err
		}(i, batch)
	}

	wg.Wait()

	// Check for errors
	for i, err := range errors {
		if err != nil {
			return nil, fmt.Errorf("batch %d failed: %w", i, err)
		}
	}

	// Combine results
	var allChunks []models.CodeChunk
	for _, batch := range results {
		allChunks = append(allChunks, batch...)
	}

	duration := time.Since(startTime)
	embeddingsPerSec := float64(len(chunks)) / duration.Seconds()
	log.Printf("Generated %d embeddings in %v (%.1f embeddings/sec)",
		len(chunks), duration, embeddingsPerSec)

	return allChunks, nil
}

// processBatch processes a single batch of chunks
func (b *Batcher) processBatch(chunks []models.CodeChunk, batchIdx int) ([]models.CodeChunk, error) {
	log.Printf("Processing batch %d with %d chunks...", batchIdx, len(chunks))

	for i := range chunks {
		// Generate embedding for chunk content
		embedding, err := b.client.GenerateEmbedding(chunks[i].Content)
		if err != nil {
			return nil, fmt.Errorf("failed to generate embedding for chunk %s: %w", chunks[i].ID, err)
		}

		chunks[i].Embedding = embedding

		// Log progress for large batches
		if (i+1)%10 == 0 && len(chunks) > 20 {
			progress := float64(i+1) / float64(len(chunks)) * 100
			log.Printf("Batch %d: %.1f%% complete (%d/%d)", batchIdx, progress, i+1, len(chunks))
		}
	}

	return chunks, nil
}

// createBatches splits chunks into batches
func (b *Batcher) createBatches(chunks []models.CodeChunk) [][]models.CodeChunk {
	var batches [][]models.CodeChunk

	for i := 0; i < len(chunks); i += b.batchSize {
		end := i + b.batchSize
		if end > len(chunks) {
			end = len(chunks)
		}
		batches = append(batches, chunks[i:end])
	}

	return batches
}

// EstimateTime estimates the time to process a given number of chunks
func (b *Batcher) EstimateTime(numChunks int) time.Duration {
	// Based on nomic-embed-text performance: ~1000 embeddings/sec on CPU
	// With batch processing and parallel workers, we can achieve ~500-800 embeddings/sec
	embeddingsPerSecond := 600.0 // Conservative estimate

	seconds := float64(numChunks) / embeddingsPerSecond
	return time.Duration(seconds * float64(time.Second))
}
