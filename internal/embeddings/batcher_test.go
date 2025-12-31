package embeddings

import (
	"testing"

	"github.com/jamaly87/codebase-semantic-search/internal/models"
)

// Mock client for testing
type mockClient struct {
	embeddings []float32
	callCount  int
}

func (m *mockClient) GenerateEmbedding(text string) ([]float32, error) {
	m.callCount++
	// Return simple embedding based on text length
	return []float32{float32(len(text)), 0.5, 0.3}, nil
}

func (m *mockClient) GenerateEmbeddings(texts []string) ([][]float32, error) {
	embeddings := make([][]float32, len(texts))
	for i, text := range texts {
		embedding, err := m.GenerateEmbedding(text)
		if err != nil {
			return nil, err
		}
		embeddings[i] = embedding
	}
	return embeddings, nil
}

func TestBatchCreation(t *testing.T) {
	tests := []struct {
		name          string
		chunks        []models.CodeChunk
		batchSize     int
		expectedBatch int
	}{
		{
			name: "exact batch size",
			chunks: []models.CodeChunk{
				{ID: "1", Content: "a"},
				{ID: "2", Content: "b"},
				{ID: "3", Content: "c"},
				{ID: "4", Content: "d"},
			},
			batchSize:     2,
			expectedBatch: 2,
		},
		{
			name: "partial last batch",
			chunks: []models.CodeChunk{
				{ID: "1", Content: "a"},
				{ID: "2", Content: "b"},
				{ID: "3", Content: "c"},
			},
			batchSize:     2,
			expectedBatch: 2, // 2 batches: [a,b], [c]
		},
		{
			name: "single chunk",
			chunks: []models.CodeChunk{
				{ID: "1", Content: "a"},
			},
			batchSize:     10,
			expectedBatch: 1,
		},
		{
			name:          "empty chunks",
			chunks:        []models.CodeChunk{},
			batchSize:     10,
			expectedBatch: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			batches := createBatches(tt.chunks, tt.batchSize)

			if len(batches) != tt.expectedBatch {
				t.Errorf("Expected %d batches, got %d", tt.expectedBatch, len(batches))
			}

			// Verify all chunks are included
			totalChunks := 0
			for _, batch := range batches {
				totalChunks += len(batch)

				// Each batch should be <= batchSize
				if len(batch) > tt.batchSize {
					t.Errorf("Batch size %d exceeds max %d", len(batch), tt.batchSize)
				}
			}

			if totalChunks != len(tt.chunks) {
				t.Errorf("Expected %d total chunks, got %d", len(tt.chunks), totalChunks)
			}
		})
	}
}

func TestBatchProcessing(t *testing.T) {
	mockClient := &mockClient{}

	batcher := &Batcher{
		client:    mockClient,
		batchSize: 2,
		workers:   2,
	}

	chunks := []models.CodeChunk{
		{ID: "1", Content: "test1"},
		{ID: "2", Content: "test2"},
		{ID: "3", Content: "test3"},
	}

	result, err := batcher.ProcessChunks(chunks)
	if err != nil {
		t.Fatalf("ProcessChunks failed: %v", err)
	}

	// Check all chunks processed
	if len(result) != len(chunks) {
		t.Errorf("Expected %d results, got %d", len(chunks), len(result))
	}

	// Check embeddings were added
	for i, chunk := range result {
		if len(chunk.Embedding) == 0 {
			t.Errorf("Chunk %d missing embedding", i)
		}

		// Verify embedding has correct dimension
		if len(chunk.Embedding) != 3 {
			t.Errorf("Expected embedding dimension 3, got %d", len(chunk.Embedding))
		}

		// Verify ID preserved
		if chunk.ID != chunks[i].ID {
			t.Errorf("Chunk ID mismatch: expected %s, got %s", chunks[i].ID, chunk.ID)
		}
	}

	// Verify client was called for each chunk
	if mockClient.callCount != len(chunks) {
		t.Errorf("Expected %d API calls, got %d", len(chunks), mockClient.callCount)
	}
}

func TestWorkerPoolSize(t *testing.T) {
	tests := []struct {
		name            string
		workers         int
		expectedWorkers int
	}{
		{
			name:            "default workers",
			workers:         4,
			expectedWorkers: 4,
		},
		{
			name:            "single worker",
			workers:         1,
			expectedWorkers: 1,
		},
		{
			name:            "many workers",
			workers:         16,
			expectedWorkers: 16,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockClient := &mockClient{}
			batcher := NewBatcher(mockClient, 10, tt.workers)

			if batcher.workers != tt.expectedWorkers {
				t.Errorf("Expected %d workers, got %d", tt.expectedWorkers, batcher.workers)
			}
		})
	}
}

// Helper function to create batches (mimics internal logic)
func createBatches(chunks []models.CodeChunk, batchSize int) [][]models.CodeChunk {
	if len(chunks) == 0 {
		return [][]models.CodeChunk{}
	}

	var batches [][]models.CodeChunk
	for i := 0; i < len(chunks); i += batchSize {
		end := i + batchSize
		if end > len(chunks) {
			end = len(chunks)
		}
		batches = append(batches, chunks[i:end])
	}
	return batches
}
