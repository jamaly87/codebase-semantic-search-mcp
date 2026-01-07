package indexer

import (
	"context"
	"fmt"
	"log"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jamaly87/codebase-semantic-search/internal/cache"
	"github.com/jamaly87/codebase-semantic-search/internal/embeddings"
	"github.com/jamaly87/codebase-semantic-search/internal/models"
	"github.com/jamaly87/codebase-semantic-search/internal/vectordb"
	"github.com/jamaly87/codebase-semantic-search/pkg/config"
)

// Indexer orchestrates the code indexing process
type Indexer struct {
	config           *config.Config
	scanner          *Scanner
	chunker          *Chunker
	hashManager      *cache.FileHashManager
	embeddingsClient *embeddings.Client
	batcher          *embeddings.Batcher
	vectorDB         *vectordb.Client
	jobs             map[string]*models.IndexJob
	jobsMux          sync.RWMutex
}

// NewIndexer creates a new code indexer
func NewIndexer(cfg *config.Config) (*Indexer, error) {
	// Create cache directory
	hashManager, err := cache.NewFileHashManager(cfg.Cache.Directory)
	if err != nil {
		return nil, fmt.Errorf("failed to create hash manager: %w", err)
	}

	// Create scanner with ignore patterns
	scanner := NewScanner(&cfg.Indexing, cfg.Ignore.Patterns)

	// Create chunker
	chunker := NewChunker(&cfg.Chunking)

	// Create embeddings client
	embeddingsClient := embeddings.NewClient(&cfg.Embeddings)

	// Create batcher
	batcher := embeddings.NewBatcher(
		embeddingsClient,
		cfg.Embeddings.BatchSize,
		cfg.Indexing.ParallelWorkers,
	)

	// Create vector database client
	vectorDB, err := vectordb.NewClient(&cfg.VectorDB)
	if err != nil {
		return nil, fmt.Errorf("failed to create vector DB client: %w", err)
	}

	// Initialize vector DB (create collection if needed)
	ctx := context.Background()
	if err := vectorDB.Initialize(ctx); err != nil {
		return nil, fmt.Errorf("failed to initialize vector DB: %w", err)
	}

	return &Indexer{
		config:           cfg,
		scanner:          scanner,
		chunker:          chunker,
		hashManager:      hashManager,
		embeddingsClient: embeddingsClient,
		batcher:          batcher,
		vectorDB:         vectorDB,
		jobs:             make(map[string]*models.IndexJob),
	}, nil
}

// Index indexes a repository
func (idx *Indexer) Index(repoPath string, forceReindex bool) (*models.IndexJob, error) {
	// Create job
	job := &models.IndexJob{
		ID:       fmt.Sprintf("job-%d", time.Now().UnixNano()),
		RepoPath: repoPath,
		Status:   models.IndexStatusRunning,
		StartTime: time.Now(),
	}

	// Store job
	idx.jobsMux.Lock()
	idx.jobs[job.ID] = job
	idx.jobsMux.Unlock()

	// Run indexing
	if idx.config.Indexing.Background {
		// Run in background
		go idx.doIndex(job, forceReindex)
	} else {
		// Run synchronously
		idx.doIndex(job, forceReindex)
	}

	return job, nil
}

// doIndex performs the actual indexing
func (idx *Indexer) doIndex(job *models.IndexJob, forceReindex bool) {
	defer func() {
		job.EndTime = time.Now()
	}()

	log.Printf("[%s] Starting indexing for %s", job.ID, job.RepoPath)

	// Load file hash cache
	if !forceReindex && idx.config.Indexing.Incremental {
		if err := idx.hashManager.Load(job.RepoPath); err != nil {
			log.Printf("[%s] Warning: Failed to load hash cache: %v", job.ID, err)
		}
	}

	// Scan repository
	log.Printf("[%s] Scanning repository...", job.ID)
	scanResult, err := idx.scanner.Scan(job.RepoPath)
	if err != nil {
		job.Status = models.IndexStatusFailed
		job.Error = fmt.Sprintf("scan failed: %v", err)
		log.Printf("[%s] Scan failed: %v", job.ID, err)
		return
	}

	job.FilesTotal = len(scanResult.Files)
	log.Printf("[%s] Found %d files to process", job.ID, job.FilesTotal)

	// Process files in parallel using worker pool
	allChunks := idx.processFilesInParallel(job, scanResult.Files, forceReindex)

	job.ChunksTotal = len(allChunks)

	log.Printf("[%s] Generated %d chunks from %d files", job.ID, len(allChunks), job.FilesIndexed)

	// Phase 3: Generate embeddings
	if len(allChunks) > 0 {
		log.Printf("[%s] Generating embeddings for %d chunks...", job.ID, len(allChunks))
		embeddingStart := time.Now()

		chunksWithEmbeddings, err := idx.batcher.ProcessChunks(allChunks)
		if err != nil {
			job.Status = models.IndexStatusFailed
			job.Error = fmt.Sprintf("Embedding generation failed: %v. Cache was NOT updated - files will be reprocessed on next attempt.", err)
			log.Printf("[%s] Embedding generation failed: %v", job.ID, err)
			// DO NOT save cache - let next indexing attempt retry these files
			return
		}

		embeddingDuration := time.Since(embeddingStart)
		log.Printf("[%s] Generated embeddings in %v", job.ID, embeddingDuration)

		// Phase 4: Store in vector database
		log.Printf("[%s] Storing chunks in vector database...", job.ID)
		storageStart := time.Now()

		ctx := context.Background()
		if err := idx.vectorDB.UpsertChunks(ctx, chunksWithEmbeddings); err != nil {
			job.Status = models.IndexStatusFailed
			job.Error = fmt.Sprintf("Vector database storage failed: %v. Cache was NOT updated - files will be reprocessed on next attempt. Check if Qdrant is running: docker-compose ps", err)
			log.Printf("[%s] Vector storage failed: %v", job.ID, err)
			// DO NOT save cache - let next indexing attempt retry these files
			return
		}

		storageDuration := time.Since(storageStart)
		log.Printf("[%s] Stored chunks in %v", job.ID, storageDuration)
	}

	// CRITICAL: Save hash cache ONLY after successful Qdrant storage
	// This prevents false positives where cache says files are indexed but they're not in Qdrant
	if idx.config.Indexing.Incremental {
		if err := idx.hashManager.Save(); err != nil {
			log.Printf("[%s] Warning: Failed to save hash cache: %v", job.ID, err)
			job.Status = models.IndexStatusFailed
			job.Error = fmt.Sprintf("Cache save failed: %v. Chunks are in Qdrant but cache is inconsistent. Run with force_reindex=true to fix.", err)
			return
		}
	}

	// Update job status
	job.Status = models.IndexStatusCompleted
	job.EndTime = time.Now()
	log.Printf("[%s] Indexing completed successfully in %v", job.ID, time.Since(job.StartTime))
}

// processFilesInParallel processes files in parallel using a worker pool pattern
func (idx *Indexer) processFilesInParallel(job *models.IndexJob, files []string, forceReindex bool) []models.CodeChunk {
	// Determine number of workers
	numWorkers := idx.config.Indexing.ParallelWorkers
	if numWorkers <= 0 {
		numWorkers = 4 // Default to 4 workers
	}

	// Channel for file paths
	fileChan := make(chan string, len(files))
	for _, filePath := range files {
		fileChan <- filePath
	}
	close(fileChan)

	// Channel for chunks from workers
	chunkChan := make(chan []models.CodeChunk, numWorkers*2)

	// Track progress atomically
	var processedFiles int64
	var allChunks []models.CodeChunk
	var chunksMux sync.Mutex

	// Worker pool
	var wg sync.WaitGroup

	// Start workers
	log.Printf("[%s] Starting %d workers for parallel processing", job.ID, numWorkers)
	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			log.Printf("[%s] Worker %d started", job.ID, workerID)

			fileCount := 0
			for filePath := range fileChan {
				fileCount++
				log.Printf("[%s] Worker %d: Processing file %d: %s", job.ID, workerID, fileCount, filePath)

				// Check if file needs reindexing
				if !forceReindex && idx.config.Indexing.Incremental {
					needsReindex, err := idx.hashManager.NeedsReindex(filePath)
					if err != nil {
						log.Printf("[%s] Warning: Failed to check hash for %s: %v", job.ID, filePath, err)
					} else if !needsReindex {
						// Skip file, it hasn't changed
						log.Printf("[%s] Worker %d: Skipping unchanged file %s", job.ID, workerID, filePath)
						atomic.AddInt64(&processedFiles, 1)
						current := atomic.LoadInt64(&processedFiles)
						job.FilesIndexed = int(current)
						job.Progress = float64(current) / float64(job.FilesTotal)
						continue
					}
				}

				// Chunk file
				log.Printf("[%s] Worker %d: Chunking file %s", job.ID, workerID, filePath)
				chunks, err := idx.chunker.ChunkFile(job.RepoPath, filePath)
				if err != nil {
					log.Printf("[%s] Warning: Failed to chunk %s: %v", job.ID, filePath, err)
					atomic.AddInt64(&processedFiles, 1)
					current := atomic.LoadInt64(&processedFiles)
					job.FilesIndexed = int(current)
					job.Progress = float64(current) / float64(job.FilesTotal)
					continue
				}
				log.Printf("[%s] Worker %d: Generated %d chunks from %s", job.ID, workerID, len(chunks), filePath)

				// Add timestamp to chunks
				now := time.Now()
				for i := range chunks {
					chunks[i].IndexedAt = now
				}

				// Send chunks to channel
				log.Printf("[%s] Worker %d: Sending %d chunks to channel", job.ID, workerID, len(chunks))
				chunkChan <- chunks
				log.Printf("[%s] Worker %d: Sent chunks to channel", job.ID, workerID)

				// Update hash cache
				if idx.config.Indexing.Incremental {
					if err := idx.hashManager.Update(filePath, len(chunks)); err != nil {
						log.Printf("[%s] Warning: Failed to update hash for %s: %v", job.ID, filePath, err)
					}
				}

				// Update progress
				atomic.AddInt64(&processedFiles, 1)
				current := atomic.LoadInt64(&processedFiles)
				job.FilesIndexed = int(current)
				job.Progress = float64(current) / float64(job.FilesTotal)

				if current%10 == 0 || current == 1 {
					log.Printf("[%s] Progress: %d/%d files (%.1f%%)",
						job.ID, current, job.FilesTotal, job.Progress*100)
				}
				
				log.Printf("[%s] Worker %d: Completed processing %s", job.ID, workerID, filePath)
			}
			log.Printf("[%s] Worker %d: Finished processing all files (processed %d files)", job.ID, workerID, fileCount)
		}(i)
	}

	// Collect chunks in a separate goroutine
	done := make(chan bool)
	chunkCount := int64(0)
	go func() {
		log.Printf("[%s] Chunk collector goroutine started", job.ID)
		for chunks := range chunkChan {
			receivedCount := atomic.AddInt64(&chunkCount, int64(len(chunks)))
			log.Printf("[%s] Chunk collector: Received %d chunks (total: %d)", job.ID, len(chunks), receivedCount)
			chunksMux.Lock()
			allChunks = append(allChunks, chunks...)
			chunksMux.Unlock()
			log.Printf("[%s] Chunk collector: Added chunks to list (total chunks: %d)", job.ID, len(allChunks))
		}
		log.Printf("[%s] Chunk collector: Channel closed, finished collecting", job.ID)
		done <- true
	}()

	// Wait for all workers to finish
	log.Printf("[%s] Waiting for all %d workers to finish...", job.ID, numWorkers)
	wg.Wait()
	log.Printf("[%s] All workers finished, closing chunk channel", job.ID)
	close(chunkChan)

	// Wait for chunk collection to finish
	log.Printf("[%s] Waiting for chunk collector to finish...", job.ID)
	<-done
	log.Printf("[%s] Chunk collector finished", job.ID)

	finalProcessed := atomic.LoadInt64(&processedFiles)
	log.Printf("[%s] Generated %d chunks from %d files", job.ID, len(allChunks), finalProcessed)
	return allChunks
}

// GetJob returns a job by ID
func (idx *Indexer) GetJob(jobID string) (*models.IndexJob, error) {
	idx.jobsMux.RLock()
	defer idx.jobsMux.RUnlock()

	job, ok := idx.jobs[jobID]
	if !ok {
		return nil, fmt.Errorf("job not found: %s", jobID)
	}

	return job, nil
}

// GetRepoIndex returns index statistics for a repository
// This checks Qdrant for the actual chunk count (source of truth)
// and uses cache for metadata like last indexed time
func (idx *Indexer) GetRepoIndex(repoPath string) (*models.RepoIndex, error) {
	// Check if there's an active indexing job for this repo
	idx.jobsMux.RLock()
	for _, job := range idx.jobs {
		if job.RepoPath == repoPath && job.Status == models.IndexStatusRunning {
			idx.jobsMux.RUnlock()
			return &models.RepoIndex{
				RepoPath:    repoPath,
				TotalFiles:  job.FilesIndexed,
				TotalChunks: job.ChunksTotal,
				Languages:   make(map[string]int),
				LastIndexed: job.StartTime,
				Status:      models.IndexStatusRunning,
			}, nil
		}
	}
	idx.jobsMux.RUnlock()

	// Query Qdrant for actual chunk count (source of truth)
	ctx := context.Background()
	chunkCount, err := idx.vectorDB.CountChunks(ctx, repoPath)
	if err != nil {
		return nil, fmt.Errorf("failed to query Qdrant: %w", err)
	}

	// Try to load cache for metadata (last indexed time, file count)
	var lastIndexed time.Time
	var totalFiles int

	if err := idx.hashManager.Load(repoPath); err == nil {
		stats := idx.hashManager.GetStats()
		if files, ok := stats["total_files"].(int); ok {
			totalFiles = files
		}
		if updated, ok := stats["updated_at"].(time.Time); ok {
			lastIndexed = updated
		}
	}

	// If no chunks in Qdrant and no cache, repo is not indexed
	if chunkCount == 0 && totalFiles == 0 {
		return &models.RepoIndex{
			RepoPath:    repoPath,
			TotalFiles:  0,
			TotalChunks: 0,
			Languages:   make(map[string]int),
			LastIndexed: time.Time{},
			Status:      "not_indexed",
		}, nil
	}

	return &models.RepoIndex{
		RepoPath:    repoPath,
		TotalFiles:  totalFiles,
		TotalChunks: chunkCount, // Use Qdrant as source of truth
		Languages:   make(map[string]int),
		LastIndexed: lastIndexed,
		Status:      models.IndexStatusCompleted,
	}, nil
}

// ClearCache clears the cache for a repository
func (idx *Indexer) ClearCache(repoPath string) error {
	return idx.hashManager.Clear(repoPath)
}
