package indexer

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/jamaly87/codebase-semantic-search/pkg/config"
	"github.com/jamaly87/codebase-semantic-search/pkg/ignore"
)

func TestScanRepository(t *testing.T) {
	// Create temporary directory structure
	tmpDir := t.TempDir()

	// Create test files
	files := map[string]string{
		"test.java":     "public class Test {}",
		"src/main.java": "public class Main {}",
		"test.txt":      "not a code file",
		"README.md":     "# README",
	}

	for path, content := range files {
		fullPath := filepath.Join(tmpDir, path)
		dir := filepath.Dir(fullPath)

		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatalf("Failed to create directory: %v", err)
		}

		if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
			t.Fatalf("Failed to create file: %v", err)
		}
	}

	cfg := &config.IndexingConfig{
		MaxFileSizeMB: 1, // 1MB
	}

	patterns := []string{}
	scanner := NewScanner(cfg, patterns)

	result, err := scanner.Scan(tmpDir)
	if err != nil {
		t.Fatalf("Scan failed: %v", err)
	}

	// Should find Java files
	if len(result.Files) < 2 {
		t.Errorf("Expected at least 2 files, got %d", len(result.Files))
	}

	// Verify files are Java files
	for _, file := range result.Files {
		if filepath.Ext(file) != ".java" {
			t.Errorf("Non-Java file found: %s", file)
		}
	}
}

func TestIgnorePatterns(t *testing.T) {
	tmpDir := t.TempDir()

	// Create files including ones that should be ignored
	files := map[string]string{
		"src/main.java":           "public class Main {}",
		"node_modules/lib.js":     "ignored",
		"build/output.java":       "ignored",
		".git/config":             "ignored",
		"test/test.java":          "public class Test {}",
		"vendor/external.ts":      "ignored",
		"dist/bundle.js":          "ignored",
		"target/compiled.class":   "ignored",
	}

	for path, content := range files {
		fullPath := filepath.Join(tmpDir, path)
		dir := filepath.Dir(fullPath)

		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatalf("Failed to create directory: %v", err)
		}

		if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
			t.Fatalf("Failed to create file: %v", err)
		}
	}

	cfg := &config.IndexingConfig{
		MaxFileSizeMB: 1,
	}

	// Use common ignore patterns
	patterns := []string{
		"node_modules/**",
		"build/**",
		".git/**",
		"vendor/**",
		"dist/**",
		"target/**",
	}

	scanner := NewScanner(cfg, patterns)

	result, err := scanner.Scan(tmpDir)
	if err != nil {
		t.Fatalf("Scan failed: %v", err)
	}

	// Should only find 2 Java files (not the ones in ignored directories)
	if len(result.Files) != 2 {
		t.Errorf("Expected 2 files, got %d", len(result.Files))
		for _, f := range result.Files {
			t.Logf("Found: %s", f)
		}
	}

	// Verify ignored paths are not included
	for _, file := range result.Files {
		if contains(file, "node_modules") ||
			contains(file, "build") ||
			contains(file, ".git") ||
			contains(file, "vendor") ||
			contains(file, "dist") ||
			contains(file, "target") {
			t.Errorf("Ignored file found: %s", file)
		}
	}
}

func TestFileSizeLimit(t *testing.T) {
	tmpDir := t.TempDir()

	// Create files of different sizes
	smallFile := filepath.Join(tmpDir, "small.java")
	largeFile := filepath.Join(tmpDir, "large.java")

	// Small file (100 bytes)
	if err := os.WriteFile(smallFile, []byte(string(make([]byte, 100))), 0644); err != nil {
		t.Fatalf("Failed to create small file: %v", err)
	}

	// Large file (2MB)
	if err := os.WriteFile(largeFile, []byte(string(make([]byte, 2*1024*1024))), 0644); err != nil {
		t.Fatalf("Failed to create large file: %v", err)
	}

	cfg := &config.IndexingConfig{
		MaxFileSizeMB: 1, // 1MB limit
	}

	scanner := NewScanner(cfg, []string{})

	result, err := scanner.Scan(tmpDir)
	if err != nil {
		t.Fatalf("Scan failed: %v", err)
	}

	// Should only find small file
	if len(result.Files) != 1 {
		t.Errorf("Expected 1 file, got %d", len(result.Files))
	}

	if len(result.Files) > 0 {
		if result.Files[0] != smallFile {
			t.Errorf("Expected %s, got %s", smallFile, result.Files[0])
		}
	}
}

func TestSupportedExtensions(t *testing.T) {
	tmpDir := t.TempDir()

	// Create files with different extensions
	files := map[string]bool{
		"test.java":  true,  // Supported
		"test.ts":    true,  // Supported
		"test.tsx":   true,  // Supported
		"test.js":    true,  // Supported
		"test.jsx":   true,  // Supported
		"test.mjs":   true,  // Supported
		"test.go":    true,  // Supported (added)
		"test.py":    false, // Not supported (yet)
		"test.txt":   false, // Not supported
		"test.md":    false, // Not supported
		"test":       false, // No extension
	}

	for filename, _ := range files {
		fullPath := filepath.Join(tmpDir, filename)
		if err := os.WriteFile(fullPath, []byte("content"), 0644); err != nil {
			t.Fatalf("Failed to create file: %v", err)
		}
	}

	cfg := &config.IndexingConfig{
		MaxFileSizeMB: 1,
	}

	scanner := NewScanner(cfg, []string{})

	result, err := scanner.Scan(tmpDir)
	if err != nil {
		t.Fatalf("Scan failed: %v", err)
	}

	// Verify only supported files are included
	for _, file := range result.Files {
		filename := filepath.Base(file)
		expected, exists := files[filename]

		if !exists {
			t.Errorf("Unexpected file found: %s", filename)
			continue
		}

		if !expected {
			t.Errorf("Unsupported file found: %s", filename)
		}
	}
}

func TestEmptyRepository(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := &config.IndexingConfig{
		MaxFileSizeMB: 1,
	}

	scanner := NewScanner(cfg, []string{})

	result, err := scanner.Scan(tmpDir)
	if err != nil {
		t.Fatalf("Scan failed: %v", err)
	}

	if len(result.Files) != 0 {
		t.Errorf("Expected 0 files in empty directory, got %d", len(result.Files))
	}
}

func TestNestedDirectories(t *testing.T) {
	tmpDir := t.TempDir()

	// Create nested structure
	files := []string{
		"a/b/c/deep.java",
		"x/y/z/file.ts",
		"root.java",
	}

	for _, path := range files {
		fullPath := filepath.Join(tmpDir, path)
		dir := filepath.Dir(fullPath)

		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatalf("Failed to create directory: %v", err)
		}

		if err := os.WriteFile(fullPath, []byte("content"), 0644); err != nil {
			t.Fatalf("Failed to create file: %v", err)
		}
	}

	cfg := &config.IndexingConfig{
		MaxFileSizeMB: 1,
	}

	scanner := NewScanner(cfg, []string{})

	result, err := scanner.Scan(tmpDir)
	if err != nil {
		t.Fatalf("Scan failed: %v", err)
	}

	// Should find all 3 files
	if len(result.Files) != 3 {
		t.Errorf("Expected 3 files, got %d", len(result.Files))
	}
}

func TestIgnoreMatcher(t *testing.T) {
	patterns := []string{
		"node_modules/**",
		"*.log",
		"build/**",
	}

	matcher := ignore.NewMatcher(patterns)

	tests := []struct {
		path          string
		shouldIgnore bool
	}{
		{"node_modules/package.json", true},
		{"src/main.java", false},
		{"debug.log", true},
		{"build/output.js", true},
		{"test.java", false},
		{"src/node_modules/lib.js", true}, // Nested
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			result := matcher.ShouldIgnore(tt.path)
			if result != tt.shouldIgnore {
				t.Errorf("Path %s: expected ignore=%v, got %v",
					tt.path, tt.shouldIgnore, result)
			}
		})
	}
}

// Helper function
func contains(s, substr string) bool {
	return filepath.Base(filepath.Dir(s)) == substr ||
		filepath.Base(s) == substr ||
		len(filepath.SplitList(s)) > 0 && filepath.SplitList(s)[0] == substr
}
