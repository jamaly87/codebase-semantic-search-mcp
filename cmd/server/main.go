package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/jamaly87/codebase-semantic-search/internal/mcp"
	"github.com/jamaly87/codebase-semantic-search/pkg/config"
)

func main() {
	// Load configuration first (before setting up logging)
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	// Set up context with cancellation for logging
	logCtx, logCancel := context.WithCancel(context.Background())
	defer logCancel()

	// Set up logging with file output
	logCloser, err := setupLogging(logCtx, cfg)
	if err != nil {
		log.Fatalf("Failed to setup logging: %v", err)
	}
	if logCloser != nil {
		defer logCloser.Close()
	}

	log.Printf("Configuration loaded successfully")
	log.Printf("Embedding model: %s", cfg.Embeddings.Model)
	log.Printf("Ollama URL: %s", cfg.Embeddings.OllamaURL)
	log.Printf("Supported languages: Java, TypeScript, JavaScript")
	if cfg.Logging.Enabled {
		log.Printf("Logging to: %s", filepath.Join(cfg.Logging.Directory, "semantic-search.log"))
	}

	// Create MCP server
	server, err := mcp.NewServer(cfg)
	if err != nil {
		log.Fatalf("Failed to create MCP server: %v", err)
	}
	defer server.Close()

	// Set up context with cancellation
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle shutdown signals
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-sigChan
		log.Println("Received shutdown signal...")
		cancel()
	}()

	// Start the server
	log.Println("Starting MCP server...")
	if err := server.Start(ctx); err != nil {
		log.Fatalf("Server error: %v", err)
	}
}

// logManager handles log file rotation with proper synchronization
type logManager struct {
	mu          sync.Mutex
	logFilePath string
	logFile     *os.File
	config      config.LoggingConfig
}

// newLogManager creates a new log manager
func newLogManager(logFilePath string, cfg config.LoggingConfig) (*logManager, error) {
	lm := &logManager{
		logFilePath: logFilePath,
		config:      cfg,
	}
	
	// Open initial log file
	if err := lm.openLogFile(); err != nil {
		return nil, err
	}
	
	return lm, nil
}

// openLogFile opens or reopens the log file
func (lm *logManager) openLogFile() error {
	logFile, err := os.OpenFile(lm.logFilePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return fmt.Errorf("failed to open log file: %w", err)
	}
	
	lm.logFile = logFile
	
	// Update log output to write to both file and stderr
	multiWriter := io.MultiWriter(os.Stderr, logFile)
	log.SetOutput(multiWriter)
	
	return nil
}

// rotate performs log rotation and reopens the file
func (lm *logManager) rotate() error {
	lm.mu.Lock()
	defer lm.mu.Unlock()
	
	// Close current log file
	if lm.logFile != nil {
		lm.logFile.Close()
	}
	
	// Rotate: rename current log file with timestamp
	timestamp := time.Now().Format("2006-01-02-15-04-05")
	backupPath := fmt.Sprintf("%s.%s", lm.logFilePath, timestamp)
	
	if err := os.Rename(lm.logFilePath, backupPath); err != nil {
		// Reopen the original file even if rename failed
		lm.openLogFile()
		return fmt.Errorf("failed to rotate log file: %w", err)
	}
	
	// Reopen log file with the original path
	if err := lm.openLogFile(); err != nil {
		return err
	}
	
	log.Printf("Log file rotated: %s", backupPath)
	
	// Compress if enabled
	if lm.config.Compress {
		go compressLogFile(backupPath)
	}
	
	// Clean up old backups
	cleanOldLogFiles(filepath.Dir(lm.logFilePath), lm.config.MaxBackups, lm.config.MaxAgeDays)
	
	return nil
}

// Close closes the log file
func (lm *logManager) Close() error {
	lm.mu.Lock()
	defer lm.mu.Unlock()
	
	if lm.logFile != nil {
		return lm.logFile.Close()
	}
	return nil
}

// setupLogging configures logging to write to both file and stderr
func setupLogging(ctx context.Context, cfg *config.Config) (io.Closer, error) {
	// Set basic log format
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	log.SetPrefix("[semantic-search] ")

	// If logging is disabled or no directory specified, just log to stderr
	if !cfg.Logging.Enabled || cfg.Logging.Directory == "" {
		return nil, nil
	}

	// Create log directory if it doesn't exist
	if err := os.MkdirAll(cfg.Logging.Directory, 0755); err != nil {
		return nil, fmt.Errorf("failed to create log directory: %w", err)
	}

	// Create log file with timestamp-based rotation
	logFileName := "semantic-search.log"
	logFilePath := filepath.Join(cfg.Logging.Directory, logFileName)

	// Create log manager
	logMgr, err := newLogManager(logFilePath, cfg.Logging)
	if err != nil {
		return nil, err
	}

	// Start log rotation with context for proper cleanup
	go rotateLogFileWithContext(ctx, logMgr)

	return logMgr, nil
}

// rotateLogFileWithContext periodically checks and rotates log files based on configuration
// It respects the context and exits gracefully when the context is cancelled
func rotateLogFileWithContext(ctx context.Context, logMgr *logManager) {
	ticker := time.NewTicker(1 * time.Hour) // Check every hour
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			// Context cancelled, exit gracefully
			log.Println("Log rotation goroutine shutting down...")
			return
		case <-ticker.C:
			fileInfo, err := os.Stat(logMgr.logFilePath)
			if err != nil {
				continue
			}

			// Check if file size exceeds max size
			maxSizeBytes := int64(logMgr.config.MaxSizeMB) * 1024 * 1024
			if fileInfo.Size() > maxSizeBytes {
				if err := logMgr.rotate(); err != nil {
					log.Printf("Failed to rotate log file: %v", err)
				}
			}
		}
	}
}

// compressLogFile compresses a log file using gzip
func compressLogFile(filePath string) {
	// Note: For simplicity, we're skipping compression implementation
	// In production, you'd use gzip.Writer here
	log.Printf("Log compression requested for: %s (not implemented)", filePath)
}

// cleanOldLogFiles removes old log backup files based on retention policy
func cleanOldLogFiles(logDir string, maxBackups, maxAgeDays int) {
	entries, err := os.ReadDir(logDir)
	if err != nil {
		return
	}

	var backupFiles []os.DirEntry
	for _, entry := range entries {
		if !entry.IsDir() && filepath.Ext(entry.Name()) == ".log" && entry.Name() != "semantic-search.log" {
			backupFiles = append(backupFiles, entry)
		}
	}

	// Remove files older than maxAgeDays
	now := time.Now()
	maxAge := time.Duration(maxAgeDays) * 24 * time.Hour

	for _, file := range backupFiles {
		info, err := file.Info()
		if err != nil {
			continue
		}

		if now.Sub(info.ModTime()) > maxAge {
			filePath := filepath.Join(logDir, file.Name())
			os.Remove(filePath)
			log.Printf("Removed old log file: %s", filePath)
		}
	}

	// If still too many backups, remove oldest ones
	if len(backupFiles) > maxBackups {
		// Sort by modification time and remove oldest
		// (Simplified - in production you'd implement proper sorting)
		log.Printf("Log backup count (%d) exceeds max (%d), oldest files should be removed", len(backupFiles), maxBackups)
	}
}
