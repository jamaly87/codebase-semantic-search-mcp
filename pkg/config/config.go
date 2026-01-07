package config

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"gopkg.in/yaml.v3"
)

// Config holds all configuration for the semantic search server
type Config struct {
	Server      ServerConfig      `yaml:"server"`
	Chunking    ChunkingConfig    `yaml:"chunking"`
	Indexing    IndexingConfig    `yaml:"indexing"`
	Search      SearchConfig      `yaml:"search"`
	Embeddings  EmbeddingsConfig  `yaml:"embeddings"`
	VectorDB    VectorDBConfig    `yaml:"vectordb"`
	Cache       CacheConfig       `yaml:"cache"`
	Logging     LoggingConfig     `yaml:"logging"`
	Ignore      IgnoreConfig      `yaml:"ignore_patterns"`
	Languages   LanguagesConfig   `yaml:"supported_languages"`
}

type ServerConfig struct {
	Name    string `yaml:"name"`
	Version string `yaml:"version"`
}

type ChunkingConfig struct {
	MaxLines           int  `yaml:"max_lines"`
	OverlapLines       int  `yaml:"overlap_lines"`
	RespectBoundaries  bool `yaml:"respect_boundaries"`
	// Adaptive chunking: different token limits based on file size
	SmallFileMaxTokens int  `yaml:"small_file_max_tokens"` // Files < 1000 lines
	MediumFileMaxTokens int  `yaml:"medium_file_max_tokens"` // Files 1000-5000 lines
	LargeFileMaxTokens  int  `yaml:"large_file_max_tokens"`  // Files > 5000 lines
	// Hierarchical chunking: split large classes/interfaces
	EnableHierarchicalChunking bool `yaml:"enable_hierarchical_chunking"`
	MaxChunkSizeBytes          int  `yaml:"max_chunk_size_bytes"` // Max size before splitting
}

type IndexingConfig struct {
	BatchSize       int  `yaml:"batch_size"`
	MaxFileSizeMB   int  `yaml:"max_file_size_mb"`
	ParallelWorkers int  `yaml:"parallel_workers"`
	Background      bool `yaml:"background"`
	Incremental     bool `yaml:"incremental"`
}

type SearchConfig struct {
	MaxResults         int     `yaml:"max_results"`
	SemanticWeight     float64 `yaml:"semantic_weight"`
	ExactMatchBoost    float64 `yaml:"exact_match_boost"`
	MinScoreThreshold  float64 `yaml:"min_score_threshold"`
}

type EmbeddingsConfig struct {
	Model         string `yaml:"model"`
	OllamaURL     string `yaml:"ollama_url"`
	BatchSize     int    `yaml:"batch_size"`
	Dimensions    int    `yaml:"dimensions"`     // Target MRL dimension (64, 128, 256, 512, 768)
	FullDimension int    `yaml:"full_dimension"` // Full embedding dimension from model (768 for nomic)
	ContextLength int    `yaml:"context_length"`
	Normalize     bool   `yaml:"normalize"`
	UseMRL        bool   `yaml:"use_mrl"` // Enable MRL dimension truncation
}

type VectorDBConfig struct {
	Type           string `yaml:"type"`
	CollectionName string `yaml:"collection_name"`
	DistanceMetric string `yaml:"distance_metric"`
	VectorSize     int    `yaml:"vector_size"`
	OnDiskPayload  bool   `yaml:"on_disk_payload"`
}

type CacheConfig struct {
	Enabled         bool   `yaml:"enabled"`
	Directory       string `yaml:"directory"`
	EmbeddingsFile  string `yaml:"embeddings_file"`
	HashesFile      string `yaml:"hashes_file"`
}

type LoggingConfig struct {
	Enabled    bool   `yaml:"enabled"`
	Directory  string `yaml:"directory"`
	MaxSizeMB  int    `yaml:"max_size_mb"`
	MaxBackups int    `yaml:"max_backups"`
	MaxAgeDays int    `yaml:"max_age_days"`
	Compress   bool   `yaml:"compress"`
}

type IgnoreConfig struct {
	Patterns []string `yaml:"patterns"`
}

type LanguagesConfig struct {
	Java       LanguageConfig `yaml:"java"`
	TypeScript LanguageConfig `yaml:"typescript"`
	JavaScript LanguageConfig `yaml:"javascript"`
}

type LanguageConfig struct {
	Extensions []string `yaml:"extensions"`
	Parser     string   `yaml:"parser"`
}

// Load loads configuration from file or returns defaults
func Load() (*Config, error) {
	cfg := DefaultConfig()

	// Try to load from config file
	configPath := getConfigPath()
	if configPath != "" {
		if err := loadFromFile(cfg, configPath); err != nil {
			return nil, fmt.Errorf("failed to load config: %w", err)
		}
	}

	// Override with environment variables
	applyEnvOverrides(cfg)

	// Expand home directory in paths
	cfg.Cache.Directory = expandPath(cfg.Cache.Directory)
	cfg.Logging.Directory = expandPath(cfg.Logging.Directory)

	return cfg, nil
}

// DefaultConfig returns the default configuration
func DefaultConfig() *Config {
	return &Config{
		Server: ServerConfig{
			Name:    "semantic-search",
			Version: "0.0.1",
		},
		Chunking: ChunkingConfig{
			MaxLines:           25,
			OverlapLines:       5,
			RespectBoundaries:  true,
			SmallFileMaxTokens: 300,  // ~1200 chars
			MediumFileMaxTokens: 200, // ~800 chars
			LargeFileMaxTokens:  150, // ~600 chars
			EnableHierarchicalChunking: true,
			MaxChunkSizeBytes:          4000, // 4KB before splitting
		},
		Indexing: IndexingConfig{
			BatchSize:       100,
			MaxFileSizeMB:   1,
			ParallelWorkers: runtime.NumCPU(),
			Background:      true,
			Incremental:     true,
		},
		Search: SearchConfig{
			MaxResults:        5,
			SemanticWeight:    0.7,
			ExactMatchBoost:   1.5,
			MinScoreThreshold: 0.5,
		},
		Embeddings: EmbeddingsConfig{
			Model:         "nomic-embed-text",
			OllamaURL:     "http://localhost:11434",
			BatchSize:     16,
			Dimensions:    256,  // MRL target dimension (3x smaller, ~95% accuracy)
			FullDimension: 768,  // Full dimension from nomic-embed-text
			ContextLength: 8192,
			Normalize:     true,
			UseMRL:        true, // Enable MRL truncation
		},
		VectorDB: VectorDBConfig{
			Type:           "embedded",
			CollectionName: "code_chunks",
			DistanceMetric: "cosine",
			VectorSize:     256,  // Match MRL dimension
			OnDiskPayload:  true,
		},
		Cache: CacheConfig{
			Enabled:        true,
			Directory:      "~/.semantic-search/cache",
			EmbeddingsFile: "embeddings.db",
			HashesFile:     "file-hashes.json",
		},
		Logging: LoggingConfig{
			Enabled:    true,
			Directory:  "~/.semantic-search/logs",
			MaxSizeMB:  10,
			MaxBackups: 5,
			MaxAgeDays: 30,
			Compress:   true,
		},
		Ignore: IgnoreConfig{
			Patterns: []string{
				"target/**",
				"build/**",
				"dist/**",
				"out/**",
				"node_modules/**",
				".pnp/**",
				"**/*.min.js",
				"**/*.bundle.js",
				".git/**",
				".idea/**",
				".vscode/**",
				"*.iml",
			},
		},
		Languages: LanguagesConfig{
			Java: LanguageConfig{
				Extensions: []string{".java"},
				Parser:     "tree-sitter-java",
			},
			TypeScript: LanguageConfig{
				Extensions: []string{".ts", ".tsx"},
				Parser:     "tree-sitter-typescript",
			},
			JavaScript: LanguageConfig{
				Extensions: []string{".js", ".jsx", ".mjs", ".cjs"},
				Parser:     "tree-sitter-javascript",
			},
		},
	}
}

func getConfigPath() string {
	// Check environment variable first
	if path := os.Getenv("SEMANTIC_SEARCH_CONFIG"); path != "" {
		return path
	}

	// Check current directory
	if _, err := os.Stat("config.yaml"); err == nil {
		return "config.yaml"
	}

	// Check home directory
	home, err := os.UserHomeDir()
	if err == nil {
		path := filepath.Join(home, ".semantic-search", "config.yaml")
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}

	return ""
}

func loadFromFile(cfg *Config, path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	return yaml.Unmarshal(data, cfg)
}

func applyEnvOverrides(cfg *Config) {
	if url := os.Getenv("OLLAMA_URL"); url != "" {
		cfg.Embeddings.OllamaURL = url
	}
	if model := os.Getenv("EMBEDDING_MODEL"); model != "" {
		cfg.Embeddings.Model = model
	}
}

func expandPath(path string) string {
	if len(path) > 0 && path[0] == '~' {
		home, err := os.UserHomeDir()
		if err == nil {
			return filepath.Join(home, path[1:])
		}
	}
	return path
}
