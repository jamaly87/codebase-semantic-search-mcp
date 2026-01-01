package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"path/filepath"
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

	// Set up logging with file output
	logFile, err := setupLogging(cfg)
	if err != nil {
		log.Fatalf("Failed to setup logging: %v", err)
	}
	if logFile != nil {
		defer logFile.Close()
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

// setupLogging configures logging to write to both file and stderr
func setupLogging(cfg *config.Config) (*os.File, error) {
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

	// Open log file (append mode)
	logFile, err := os.OpenFile(logFilePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return nil, fmt.Errorf("failed to open log file: %w", err)
	}

	// Write to both file and stderr
	multiWriter := io.MultiWriter(os.Stderr, logFile)
	log.SetOutput(multiWriter)

	// Log rotation: if file is too large, rotate it
	go rotateLogFile(logFilePath, cfg.Logging)

	return logFile, nil
}

// rotateLogFile periodically checks and rotates log files based on configuration
func rotateLogFile(logFilePath string, cfg config.LoggingConfig) {
	ticker := time.NewTicker(1 * time.Hour) // Check every hour
	defer ticker.Stop()

	for range ticker.C {
		fileInfo, err := os.Stat(logFilePath)
		if err != nil {
			continue
		}

		// Check if file size exceeds max size
		maxSizeBytes := int64(cfg.MaxSizeMB) * 1024 * 1024
		if fileInfo.Size() > maxSizeBytes {
			// Rotate: rename current log file with timestamp
			timestamp := time.Now().Format("2006-01-02-15-04-05")
			backupPath := fmt.Sprintf("%s.%s", logFilePath, timestamp)

			if err := os.Rename(logFilePath, backupPath); err != nil {
				log.Printf("Failed to rotate log file: %v", err)
				continue
			}

			// Compress if enabled
			if cfg.Compress {
				go compressLogFile(backupPath)
			}

			// Clean up old backups
			cleanOldLogFiles(filepath.Dir(logFilePath), cfg.MaxBackups, cfg.MaxAgeDays)

			log.Printf("Log file rotated: %s", backupPath)
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
