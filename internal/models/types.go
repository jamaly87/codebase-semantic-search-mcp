package models

import "time"

// CodeChunk represents a chunk of code stored in the vector database
type CodeChunk struct {
	ID           string                 `json:"id"`
	RepoPath     string                 `json:"repo_path"`
	FilePath     string                 `json:"file_path"`
	ChunkType    ChunkType              `json:"chunk_type"`
	Content      string                 `json:"content"`
	Language     string                 `json:"language"`
	StartLine    int                    `json:"start_line"`
	EndLine      int                    `json:"end_line"`
	FunctionName string                 `json:"function_name,omitempty"`
	ClassName    string                 `json:"class_name,omitempty"`
	Metadata     map[string]interface{} `json:"metadata,omitempty"`
	Embedding    []float32              `json:"embedding,omitempty"`
	IndexedAt    time.Time              `json:"indexed_at"`
}

// ChunkType defines the type of code chunk
type ChunkType string

const (
	ChunkTypeFunction ChunkType = "function"
	ChunkTypeFile     ChunkType = "file"
)

// SearchResult represents a search result with score
type SearchResult struct {
	Chunk          CodeChunk `json:"chunk"`
	Score          float64   `json:"score"`
	SemanticScore  float64   `json:"semantic_score"`
	ExactScore     float64   `json:"exact_score"`
	Preview        string    `json:"preview"`
	LineRange      string    `json:"line_range"`
}

// RepoIndex represents the index status of a repository
type RepoIndex struct {
	RepoPath      string            `json:"repo_path"`
	TotalFiles    int               `json:"total_files"`
	TotalChunks   int               `json:"total_chunks"`
	Languages     map[string]int    `json:"languages"`
	LastIndexed   time.Time         `json:"last_indexed"`
	IndexDuration time.Duration     `json:"index_duration"`
	Status        IndexStatus       `json:"status"`
}

// IndexStatus represents the current status of an indexing job
type IndexStatus string

const (
	IndexStatusPending   IndexStatus = "pending"
	IndexStatusRunning   IndexStatus = "running"
	IndexStatusCompleted IndexStatus = "completed"
	IndexStatusFailed    IndexStatus = "failed"
)

// IndexJob represents a background indexing job
type IndexJob struct {
	ID           string        `json:"id"`
	RepoPath     string        `json:"repo_path"`
	Status       IndexStatus   `json:"status"`
	Progress     float64       `json:"progress"`
	StartTime    time.Time     `json:"start_time"`
	EndTime      time.Time     `json:"end_time,omitempty"`
	FilesTotal   int           `json:"files_total"`
	FilesIndexed int           `json:"files_indexed"`
	ChunksTotal  int           `json:"chunks_total"`
	Error        string        `json:"error,omitempty"`
}

// FileHash tracks file hashes for incremental indexing
type FileHash struct {
	Path        string    `json:"path"`
	Hash        string    `json:"hash"`
	LastIndexed time.Time `json:"last_indexed"`
	ChunkCount  int       `json:"chunk_count"`
}

// FileHashCache stores all file hashes for a repository
type FileHashCache struct {
	RepoPath string               `json:"repo_path"`
	Hashes   map[string]FileHash  `json:"hashes"`
	UpdatedAt time.Time           `json:"updated_at"`
}

// SearchQuery represents a semantic search query
type SearchQuery struct {
	Query     string    `json:"query"`
	RepoPath  string    `json:"repo_path"`
	ChunkType ChunkType `json:"chunk_type,omitempty"`
	Limit     int       `json:"limit"`
}

// SearchResponse contains search results
type SearchResponse struct {
	Results   []SearchResult `json:"results"`
	Query     string         `json:"query"`
	TotalTime int64          `json:"total_time_ms"`
}

// Language represents a supported programming language
type Language struct {
	Name       string   `json:"name"`
	Extensions []string `json:"extensions"`
	Parser     string   `json:"parser"`
}