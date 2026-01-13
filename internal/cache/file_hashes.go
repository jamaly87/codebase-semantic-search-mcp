package cache

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/jamaly87/codebase-semantic-search/internal/models"
)

// FileHashManager manages file hashes for incremental indexing
// Thread-safe: all operations are protected by a mutex for concurrent access
type FileHashManager struct {
	cacheDir string
	cache    *models.FileHashCache
	mux      sync.RWMutex // Protects cache access from concurrent workers
}

// NewFileHashManager creates a new file hash manager
func NewFileHashManager(cacheDir string) (*FileHashManager, error) {
	// Ensure cache directory exists
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create cache directory: %w", err)
	}

	return &FileHashManager{
		cacheDir: cacheDir,
	}, nil
}

// Load loads the file hash cache for a repository
func (fhm *FileHashManager) Load(repoPath string) error {
	fhm.mux.Lock()
	defer fhm.mux.Unlock()

	cachePath := fhm.getCachePath(repoPath)

	// If cache file doesn't exist, create new cache
	if _, err := os.Stat(cachePath); os.IsNotExist(err) {
		fhm.cache = &models.FileHashCache{
			RepoPath:  repoPath,
			Hashes:    make(map[string]models.FileHash),
			UpdatedAt: time.Now(),
		}
		return nil
	}

	// Read existing cache
	data, err := os.ReadFile(cachePath)
	if err != nil {
		return fmt.Errorf("failed to read cache file: %w", err)
	}

	var cache models.FileHashCache
	if err := json.Unmarshal(data, &cache); err != nil {
		return fmt.Errorf("failed to parse cache file: %w", err)
	}

	fhm.cache = &cache
	return nil
}

// Save saves the file hash cache
func (fhm *FileHashManager) Save() error {
	fhm.mux.RLock()
	if fhm.cache == nil {
		fhm.mux.RUnlock()
		return fmt.Errorf("no cache loaded")
	}

	// Create a copy to avoid holding lock during file I/O
	cacheCopy := *fhm.cache
	cacheCopy.Hashes = make(map[string]models.FileHash)
	for k, v := range fhm.cache.Hashes {
		cacheCopy.Hashes[k] = v
	}
	fhm.mux.RUnlock()

	cacheCopy.UpdatedAt = time.Now()

	data, err := json.MarshalIndent(cacheCopy, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal cache: %w", err)
	}

	cachePath := fhm.getCachePath(cacheCopy.RepoPath)
	if err := os.WriteFile(cachePath, data, 0644); err != nil {
		return fmt.Errorf("failed to write cache file: %w", err)
	}

	return nil
}

// NeedsReindex returns true if a file needs to be reindexed
// Thread-safe: uses read lock for concurrent access
func (fhm *FileHashManager) NeedsReindex(filePath string) (bool, error) {
	fhm.mux.RLock()
	if fhm.cache == nil {
		fhm.mux.RUnlock()
		return true, nil // No cache loaded, reindex everything
	}
	fhm.mux.RUnlock()

	// Calculate current file hash (expensive operation, do outside lock)
	currentHash, err := computeFileHash(filePath)
	if err != nil {
		return false, fmt.Errorf("failed to compute file hash: %w", err)
	}

	// Check if file exists in cache with a single lock acquisition
	fhm.mux.RLock()
	defer fhm.mux.RUnlock()
	
	// Re-check cache validity after expensive operation
	if fhm.cache == nil {
		return true, nil // Cache was cleared, reindex everything
	}

	cached, exists := fhm.cache.Hashes[filePath]
	if !exists {
		return true, nil // New file
	}

	// Compare hashes
	return cached.Hash != currentHash, nil
}

// Update updates the hash for a file
// Thread-safe: uses write lock for concurrent access
func (fhm *FileHashManager) Update(filePath string, chunkCount int) error {
	// Calculate hash outside lock (expensive operation)
	hash, err := computeFileHash(filePath)
	if err != nil {
		return fmt.Errorf("failed to compute file hash: %w", err)
	}

	fhm.mux.Lock()
	defer fhm.mux.Unlock()

	if fhm.cache == nil {
		return fmt.Errorf("no cache loaded")
	}

	fhm.cache.Hashes[filePath] = models.FileHash{
		Path:        filePath,
		Hash:        hash,
		LastIndexed: time.Now(),
		ChunkCount:  chunkCount,
	}

	return nil
}

// Remove removes a file from the cache
// Thread-safe: uses write lock for concurrent access
func (fhm *FileHashManager) Remove(filePath string) {
	fhm.mux.Lock()
	defer fhm.mux.Unlock()

	if fhm.cache != nil {
		delete(fhm.cache.Hashes, filePath)
	}
}

// GetStats returns statistics about the cache
// Thread-safe: uses read lock for concurrent access
func (fhm *FileHashManager) GetStats() map[string]interface{} {
	fhm.mux.RLock()
	defer fhm.mux.RUnlock()

	if fhm.cache == nil {
		return map[string]interface{}{
			"total_files": 0,
			"total_chunks": 0,
		}
	}

	totalChunks := 0
	for _, hash := range fhm.cache.Hashes {
		totalChunks += hash.ChunkCount
	}

	return map[string]interface{}{
		"total_files":  len(fhm.cache.Hashes),
		"total_chunks": totalChunks,
		"updated_at":   fhm.cache.UpdatedAt,
	}
}

// Clear clears the cache for a repository
// Thread-safe: uses write lock for concurrent access
func (fhm *FileHashManager) Clear(repoPath string) error {
	fhm.mux.Lock()
	defer fhm.mux.Unlock()

	cachePath := fhm.getCachePath(repoPath)
	if err := os.Remove(cachePath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove cache file: %w", err)
	}

	fhm.cache = &models.FileHashCache{
		RepoPath:  repoPath,
		Hashes:    make(map[string]models.FileHash),
		UpdatedAt: time.Now(),
	}

	return nil
}

// getCachePath returns the cache file path for a repository
func (fhm *FileHashManager) getCachePath(repoPath string) string {
	// Create a safe filename from the repo path
	hash := sha256.Sum256([]byte(repoPath))
	filename := fmt.Sprintf("file-hashes-%x.json", hash[:8])
	return filepath.Join(fhm.cacheDir, filename)
}

// computeFileHash computes SHA256 hash of a file
func computeFileHash(filePath string) (string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return "", err
	}
	defer file.Close()

	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}

	return fmt.Sprintf("%x", hash.Sum(nil)), nil
}
