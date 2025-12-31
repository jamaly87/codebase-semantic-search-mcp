package vectordb

import (
	"context"
	"fmt"
	"log"

	"github.com/google/uuid"
	"github.com/jamaly87/codebase-semantic-search/internal/models"
	"github.com/jamaly87/codebase-semantic-search/pkg/config"
	"github.com/qdrant/go-client/qdrant"
)

// Client represents a Qdrant vector database client
type Client struct {
	config     *config.VectorDBConfig
	client     *qdrant.Client
	collection string
}

// NewClient creates a new Qdrant client
func NewClient(cfg *config.VectorDBConfig) (*Client, error) {
	// Connect to Qdrant via gRPC (localhost:6334)
	qdrantConfig := &qdrant.Config{
		Host: "localhost",
		Port: 6334,
		UseTLS: false,
	}

	client, err := qdrant.NewClient(qdrantConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Qdrant: %w", err)
	}

	c := &Client{
		config:     cfg,
		client:     client,
		collection: cfg.CollectionName,
	}

	return c, nil
}

// Initialize initializes the Qdrant database and creates collections
func (c *Client) Initialize(ctx context.Context) error {
	log.Printf("Initializing Qdrant collection: %s", c.collection)

	// Check if collection exists
	exists, err := c.client.CollectionExists(ctx, c.collection)
	if err != nil {
		return fmt.Errorf("failed to check collection existence: %w", err)
	}

	if exists {
		log.Printf("Collection %s already exists", c.collection)
		return nil
	}

	// Create collection
	err = c.client.CreateCollection(ctx, &qdrant.CreateCollection{
		CollectionName: c.collection,
		VectorsConfig: &qdrant.VectorsConfig{
			Config: &qdrant.VectorsConfig_Params{
				Params: &qdrant.VectorParams{
					Size:     uint64(c.config.VectorSize),
					Distance: c.getDistanceMetric(),
				},
			},
		},
	})

	if err != nil {
		return fmt.Errorf("failed to create collection: %w", err)
	}

	log.Printf("Created collection %s with %d dimensions", c.collection, c.config.VectorSize)
	return nil
}

// UpsertChunks inserts or updates code chunks in the vector database
func (c *Client) UpsertChunks(ctx context.Context, chunks []models.CodeChunk) error {
	if len(chunks) == 0 {
		return nil
	}

	log.Printf("Upserting %d chunks to Qdrant...", len(chunks))

	// Convert chunks to Qdrant points
	points := make([]*qdrant.PointStruct, len(chunks))

	for i, chunk := range chunks {
		// Create payload
		payload := map[string]*qdrant.Value{
			"repo_path":     qdrant.NewValueString(chunk.RepoPath),
			"file_path":     qdrant.NewValueString(chunk.FilePath),
			"chunk_type":    qdrant.NewValueString(string(chunk.ChunkType)),
			"content":       qdrant.NewValueString(chunk.Content),
			"language":      qdrant.NewValueString(chunk.Language),
			"start_line":    qdrant.NewValueInt(int64(chunk.StartLine)),
			"end_line":      qdrant.NewValueInt(int64(chunk.EndLine)),
			"function_name": qdrant.NewValueString(chunk.FunctionName),
			"class_name":    qdrant.NewValueString(chunk.ClassName),
		}

		// Convert embedding to []float32 if needed
		vector := make([]float32, len(chunk.Embedding))
		copy(vector, chunk.Embedding)

		points[i] = &qdrant.PointStruct{
			Id: &qdrant.PointId{
				PointIdOptions: &qdrant.PointId_Uuid{
					Uuid: chunk.ID,
				},
			},
			Vectors: &qdrant.Vectors{
				VectorsOptions: &qdrant.Vectors_Vector{
					Vector: &qdrant.Vector{
						Data: vector,
					},
				},
			},
			Payload: payload,
		}
	}

	// Upsert points
	_, err := c.client.Upsert(ctx, &qdrant.UpsertPoints{
		CollectionName: c.collection,
		Points:         points,
	})

	if err != nil {
		return fmt.Errorf("failed to upsert points: %w", err)
	}

	log.Printf("Successfully upserted %d chunks", len(chunks))
	return nil
}

// Search performs a vector similarity search
func (c *Client) Search(ctx context.Context, embedding []float32, repoPath string, limit int) ([]models.CodeChunk, []float64, error) {
	if limit <= 0 {
		limit = 5
	}

	limitUint := uint64(limit)

	// Build query with vector
	query := qdrant.NewQuery(embedding...)

	// Build query request
	queryPoints := &qdrant.QueryPoints{
		CollectionName: c.collection,
		Query:          query,
		Limit:          &limitUint,
		WithPayload:    &qdrant.WithPayloadSelector{SelectorOptions: &qdrant.WithPayloadSelector_Enable{Enable: true}},
	}

	// Add repo filter if specified
	if repoPath != "" {
		queryPoints.Filter = &qdrant.Filter{
			Must: []*qdrant.Condition{
				{
					ConditionOneOf: &qdrant.Condition_Field{
						Field: &qdrant.FieldCondition{
							Key: "repo_path",
							Match: &qdrant.Match{
								MatchValue: &qdrant.Match_Keyword{
									Keyword: repoPath,
								},
							},
						},
					},
				},
			},
		}
	}

	// Execute search
	results, err := c.client.Query(ctx, queryPoints)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to search: %w", err)
	}

	if len(results) == 0 {
		log.Printf("No results found for query")
		return []models.CodeChunk{}, []float64{}, nil
	}

	// Convert results to CodeChunks
	chunks := make([]models.CodeChunk, len(results))
	scores := make([]float64, len(results))

	for i, result := range results {
		// Extract score
		scores[i] = float64(result.Score)

		// Extract payload
		payload := result.Payload

		chunk := models.CodeChunk{
			ID:           result.Id.GetUuid(),
			RepoPath:     payload["repo_path"].GetStringValue(),
			FilePath:     payload["file_path"].GetStringValue(),
			ChunkType:    models.ChunkType(payload["chunk_type"].GetStringValue()),
			Content:      payload["content"].GetStringValue(),
			Language:     payload["language"].GetStringValue(),
			StartLine:    int(payload["start_line"].GetIntegerValue()),
			EndLine:      int(payload["end_line"].GetIntegerValue()),
			FunctionName: payload["function_name"].GetStringValue(),
			ClassName:    payload["class_name"].GetStringValue(),
		}

		chunks[i] = chunk
	}

	log.Printf("Found %d results for query (top score: %.3f)", len(chunks), scores[0])
	return chunks, scores, nil
}

// DeleteByRepo deletes all chunks for a given repository
func (c *Client) DeleteByRepo(ctx context.Context, repoPath string) error {
	_, err := c.client.Delete(ctx, &qdrant.DeletePoints{
		CollectionName: c.collection,
		Points: &qdrant.PointsSelector{
			PointsSelectorOneOf: &qdrant.PointsSelector_Filter{
				Filter: &qdrant.Filter{
					Must: []*qdrant.Condition{
						{
							ConditionOneOf: &qdrant.Condition_Field{
								Field: &qdrant.FieldCondition{
									Key: "repo_path",
									Match: &qdrant.Match{
										MatchValue: &qdrant.Match_Keyword{
											Keyword: repoPath,
										},
									},
								},
							},
						},
					},
				},
			},
		},
	})

	return err
}

// CountChunks returns the number of chunks for a given repository
func (c *Client) CountChunks(ctx context.Context, repoPath string) (int, error) {
	count, err := c.client.Count(ctx, &qdrant.CountPoints{
		CollectionName: c.collection,
		Filter: &qdrant.Filter{
			Must: []*qdrant.Condition{
				{
					ConditionOneOf: &qdrant.Condition_Field{
						Field: &qdrant.FieldCondition{
							Key: "repo_path",
							Match: &qdrant.Match{
								MatchValue: &qdrant.Match_Keyword{
									Keyword: repoPath,
								},
							},
						},
					},
				},
			},
		},
	})

	if err != nil {
		return 0, fmt.Errorf("failed to count chunks: %w", err)
	}

	return int(count), nil
}

// GetStats returns statistics about the vector database
func (c *Client) GetStats(ctx context.Context, repoPath string) (*models.RepoIndex, error) {
	// Count points for this repo
	count, err := c.client.Count(ctx, &qdrant.CountPoints{
		CollectionName: c.collection,
		Filter: &qdrant.Filter{
			Must: []*qdrant.Condition{
				{
					ConditionOneOf: &qdrant.Condition_Field{
						Field: &qdrant.FieldCondition{
							Key: "repo_path",
							Match: &qdrant.Match{
								MatchValue: &qdrant.Match_Keyword{
									Keyword: repoPath,
								},
							},
						},
					},
				},
			},
		},
	})

	if err != nil {
		return nil, fmt.Errorf("failed to count points: %w", err)
	}

	return &models.RepoIndex{
		RepoPath:    repoPath,
		TotalChunks: int(count),
		Languages:   make(map[string]int),
		Status:      models.IndexStatusCompleted,
	}, nil
}

// Close closes the Qdrant client connection
func (c *Client) Close() error {
	if c.client != nil {
		return c.client.Close()
	}
	return nil
}

// getDistanceMetric returns the Qdrant distance metric
func (c *Client) getDistanceMetric() qdrant.Distance {
	switch c.config.DistanceMetric {
	case "cosine":
		return qdrant.Distance_Cosine
	case "dot":
		return qdrant.Distance_Dot
	case "euclidean":
		return qdrant.Distance_Euclid
	default:
		return qdrant.Distance_Cosine
	}
}

// GenerateUUID generates a UUID string for Qdrant
func GenerateUUID() string {
	return uuid.New().String()
}
