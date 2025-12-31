package cache

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestFileHashManager(t *testing.T) {
	tmpDir := t.TempDir()
	cacheDir := filepath.Join(tmpDir, "cache")

	manager, err := NewFileHashManager(cacheDir)
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}

	// Create test repository
	repoDir := filepath.Join(tmpDir, "repo")
	if err := os.MkdirAll(repoDir, 0755); err != nil {
		t.Fatalf("Failed to create repo: %v", err)
	}

	testFile := filepath.Join(repoDir, "test.java")
	if err := os.WriteFile(testFile, []byte("original content"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Load cache for repo (should be empty)
	if err := manager.Load(repoDir); err != nil {
		t.Fatalf("Failed to load cache: %v", err)
	}

	// File should need reindexing (not in cache)
	needsReindex, err := manager.NeedsReindex(testFile)
	if err != nil {
		t.Fatalf("NeedsReindex failed: %v", err)
	}

	if !needsReindex {
		t.Error("Expected file to need reindex (not in cache)")
	}

	// Update hash
	if err := manager.Update(testFile, 10); err != nil {
		t.Fatalf("Update failed: %v", err)
	}

	// Save cache
	if err := manager.Save(); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	// Load again
	manager2, err := NewFileHashManager(cacheDir)
	if err != nil {
		t.Fatalf("Failed to create second manager: %v", err)
	}

	if err := manager2.Load(repoDir); err != nil {
		t.Fatalf("Failed to load cache: %v", err)
	}

	// File should NOT need reindexing (unchanged)
	needsReindex, err = manager2.NeedsReindex(testFile)
	if err != nil {
		t.Fatalf("NeedsReindex failed: %v", err)
	}

	if needsReindex {
		t.Error("Expected file to NOT need reindex (unchanged)")
	}

	// Modify file
	time.Sleep(10 * time.Millisecond) // Ensure timestamp changes
	if err := os.WriteFile(testFile, []byte("modified content"), 0644); err != nil {
		t.Fatalf("Failed to modify file: %v", err)
	}

	// Now should need reindexing
	needsReindex, err = manager2.NeedsReindex(testFile)
	if err != nil {
		t.Fatalf("NeedsReindex failed: %v", err)
	}

	if !needsReindex {
		t.Error("Expected file to need reindex (modified)")
	}
}

func TestClearCache(t *testing.T) {
	tmpDir := t.TempDir()
	cacheDir := filepath.Join(tmpDir, "cache")

	manager, err := NewFileHashManager(cacheDir)
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}

	repoDir := filepath.Join(tmpDir, "repo")
	if err := os.MkdirAll(repoDir, 0755); err != nil {
		t.Fatalf("Failed to create repo: %v", err)
	}

	testFile := filepath.Join(repoDir, "test.java")
	if err := os.WriteFile(testFile, []byte("content"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Load and update
	if err := manager.Load(repoDir); err != nil {
		t.Fatalf("Failed to load: %v", err)
	}

	if err := manager.Update(testFile, 5); err != nil {
		t.Fatalf("Failed to update: %v", err)
	}

	if err := manager.Save(); err != nil {
		t.Fatalf("Failed to save: %v", err)
	}

	// Clear cache
	if err := manager.Clear(repoDir); err != nil {
		t.Fatalf("Failed to clear: %v", err)
	}

	// Load again - should be empty
	if err := manager.Load(repoDir); err != nil {
		t.Fatalf("Failed to load after clear: %v", err)
	}

	needsReindex, err := manager.NeedsReindex(testFile)
	if err != nil {
		t.Fatalf("NeedsReindex failed: %v", err)
	}

	if !needsReindex {
		t.Error("Expected file to need reindex after cache clear")
	}
}

func TestGetStats(t *testing.T) {
	tmpDir := t.TempDir()
	cacheDir := filepath.Join(tmpDir, "cache")

	manager, err := NewFileHashManager(cacheDir)
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}

	repoDir := filepath.Join(tmpDir, "repo")
	if err := os.MkdirAll(repoDir, 0755); err != nil {
		t.Fatalf("Failed to create repo: %v", err)
	}

	// Create test files
	files := []string{"a.java", "b.java", "c.java"}
	for _, filename := range files {
		testFile := filepath.Join(repoDir, filename)
		if err := os.WriteFile(testFile, []byte("content"), 0644); err != nil {
			t.Fatalf("Failed to create test file: %v", err)
		}
	}

	if err := manager.Load(repoDir); err != nil {
		t.Fatalf("Failed to load: %v", err)
	}

	// Update all files
	totalChunks := 0
	for i, filename := range files {
		testFile := filepath.Join(repoDir, filename)
		chunks := (i + 1) * 5 // 5, 10, 15 chunks
		totalChunks += chunks

		if err := manager.Update(testFile, chunks); err != nil {
			t.Fatalf("Failed to update: %v", err)
		}
	}

	if err := manager.Save(); err != nil {
		t.Fatalf("Failed to save: %v", err)
	}

	// Get stats
	stats := manager.GetStats()

	if totalFiles, ok := stats["total_files"].(int); ok {
		if totalFiles != len(files) {
			t.Errorf("Expected %d files, got %d", len(files), totalFiles)
		}
	} else {
		t.Error("total_files stat missing")
	}

	if chunks, ok := stats["total_chunks"].(int); ok {
		if chunks != totalChunks {
			t.Errorf("Expected %d chunks, got %d", totalChunks, chunks)
		}
	} else {
		t.Error("total_chunks stat missing")
	}
}

func TestMultipleRepositories(t *testing.T) {
	tmpDir := t.TempDir()
	cacheDir := filepath.Join(tmpDir, "cache")

	manager, err := NewFileHashManager(cacheDir)
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}

	// Create two repos
	repo1 := filepath.Join(tmpDir, "repo1")
	repo2 := filepath.Join(tmpDir, "repo2")

	for _, repo := range []string{repo1, repo2} {
		if err := os.MkdirAll(repo, 0755); err != nil {
			t.Fatalf("Failed to create repo: %v", err)
		}

		testFile := filepath.Join(repo, "test.java")
		if err := os.WriteFile(testFile, []byte("content"), 0644); err != nil {
			t.Fatalf("Failed to create file: %v", err)
		}
	}

	// Update repo1
	if err := manager.Load(repo1); err != nil {
		t.Fatalf("Failed to load repo1: %v", err)
	}

	file1 := filepath.Join(repo1, "test.java")
	if err := manager.Update(file1, 5); err != nil {
		t.Fatalf("Failed to update repo1: %v", err)
	}

	if err := manager.Save(); err != nil {
		t.Fatalf("Failed to save repo1: %v", err)
	}

	// Update repo2
	if err := manager.Load(repo2); err != nil {
		t.Fatalf("Failed to load repo2: %v", err)
	}

	file2 := filepath.Join(repo2, "test.java")
	if err := manager.Update(file2, 10); err != nil {
		t.Fatalf("Failed to update repo2: %v", err)
	}

	if err := manager.Save(); err != nil {
		t.Fatalf("Failed to save repo2: %v", err)
	}

	// Verify both caches are independent
	if err := manager.Load(repo1); err != nil {
		t.Fatalf("Failed to load repo1: %v", err)
	}

	needsReindex, err := manager.NeedsReindex(file1)
	if err != nil {
		t.Fatalf("NeedsReindex failed: %v", err)
	}

	if needsReindex {
		t.Error("Repo1 file should not need reindex")
	}
}

func TestNonExistentFile(t *testing.T) {
	tmpDir := t.TempDir()
	cacheDir := filepath.Join(tmpDir, "cache")

	manager, err := NewFileHashManager(cacheDir)
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}

	repoDir := filepath.Join(tmpDir, "repo")
	if err := os.MkdirAll(repoDir, 0755); err != nil {
		t.Fatalf("Failed to create repo: %v", err)
	}

	if err := manager.Load(repoDir); err != nil {
		t.Fatalf("Failed to load: %v", err)
	}

	// Check non-existent file
	nonExistent := filepath.Join(repoDir, "doesnotexist.java")
	_, err = manager.NeedsReindex(nonExistent)

	// Should return error or true (needs indexing)
	if err == nil {
		// If no error, it should need reindexing
		needsReindex, _ := manager.NeedsReindex(nonExistent)
		if !needsReindex {
			t.Error("Non-existent file should need reindex")
		}
	}
}
