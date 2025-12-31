package indexer

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/jamaly87/codebase-semantic-search/pkg/config"
	"github.com/jamaly87/codebase-semantic-search/pkg/ignore"
)

// Scanner scans directories for source files
type Scanner struct {
	config          *config.IndexingConfig
	ignoreMatcher   *ignore.Matcher
	langDetector    *LanguageDetector
	maxFileSizeBytes int64
}

// NewScanner creates a new file scanner
func NewScanner(cfg *config.IndexingConfig, ignorePatterns []string) *Scanner {
	return &Scanner{
		config:           cfg,
		ignoreMatcher:    ignore.NewMatcher(ignorePatterns),
		langDetector:     NewLanguageDetector(),
		maxFileSizeBytes: int64(cfg.MaxFileSizeMB) * 1024 * 1024,
	}
}

// ScanResult contains the results of a directory scan
type ScanResult struct {
	Files      []string          // List of file paths to index
	TotalFiles int               // Total files found
	SkippedFiles int             // Files skipped (too large, ignored, etc.)
	Languages  map[string]int    // Count of files per language
	Errors     []error           // Errors encountered during scan
}

// Scan scans a repository directory for indexable files
func (s *Scanner) Scan(repoPath string) (*ScanResult, error) {
	// Verify directory exists
	info, err := os.Stat(repoPath)
	if err != nil {
		return nil, fmt.Errorf("failed to stat repo path: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("repo path is not a directory: %s", repoPath)
	}

	result := &ScanResult{
		Files:     make([]string, 0),
		Languages: make(map[string]int),
		Errors:    make([]error, 0),
	}

	// Walk the directory tree
	err = filepath.WalkDir(repoPath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			result.Errors = append(result.Errors, fmt.Errorf("error accessing %s: %w", path, err))
			return nil // Continue walking
		}

		// Get relative path for pattern matching
		relPath, err := filepath.Rel(repoPath, path)
		if err != nil {
			relPath = path
		}

		// Skip directories that match ignore patterns
		if d.IsDir() {
			if s.shouldIgnoreDir(relPath, d.Name()) {
				return fs.SkipDir
			}
			return nil
		}

		// Skip files that match ignore patterns
		if s.ignoreMatcher.ShouldIgnore(relPath) {
			result.SkippedFiles++
			return nil
		}

		result.TotalFiles++

		// Check if file is supported language
		if !s.langDetector.IsSupported(path) {
			result.SkippedFiles++
			return nil
		}

		// Check file size
		fileInfo, err := d.Info()
		if err != nil {
			result.Errors = append(result.Errors, fmt.Errorf("failed to get file info for %s: %w", path, err))
			result.SkippedFiles++
			return nil
		}

		if fileInfo.Size() > s.maxFileSizeBytes {
			result.SkippedFiles++
			return nil
		}

		// Add to results
		result.Files = append(result.Files, path)

		// Track language stats
		if lang, ok := s.langDetector.Detect(path); ok {
			result.Languages[lang.Name]++
		}

		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to walk directory: %w", err)
	}

	return result, nil
}

// shouldIgnoreDir returns true if a directory should be ignored
func (s *Scanner) shouldIgnoreDir(relPath, dirName string) bool {
	// Always skip hidden directories
	if strings.HasPrefix(dirName, ".") && dirName != "." {
		return true
	}

	// Check against ignore patterns
	return s.ignoreMatcher.ShouldIgnore(relPath)
}

// IsSupported returns true if the file is a supported language
func (s *Scanner) IsSupported(filePath string) bool {
	return s.langDetector.IsSupported(filePath)
}
