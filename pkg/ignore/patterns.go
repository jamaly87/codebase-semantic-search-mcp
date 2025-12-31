package ignore

import (
	"path/filepath"
	"strings"
)

// Matcher matches file paths against ignore patterns
type Matcher struct {
	patterns []string
}

// NewMatcher creates a new pattern matcher
func NewMatcher(patterns []string) *Matcher {
	return &Matcher{
		patterns: patterns,
	}
}

// ShouldIgnore returns true if the path matches any ignore pattern
func (m *Matcher) ShouldIgnore(path string) bool {
	// Normalize path separators
	path = filepath.ToSlash(path)

	for _, pattern := range m.patterns {
		if m.matchPattern(path, pattern) {
			return true
		}
	}

	return false
}

// matchPattern checks if a path matches a pattern
func (m *Matcher) matchPattern(path, pattern string) bool {
	// Normalize pattern
	pattern = filepath.ToSlash(pattern)

	// Handle ** for recursive matching
	if strings.Contains(pattern, "**") {
		// Convert ** to * for filepath.Match
		parts := strings.Split(pattern, "**")

		// If pattern is like "node_modules/**", match if path starts with "node_modules/"
		if len(parts) > 0 && parts[0] != "" {
			prefix := strings.TrimSuffix(parts[0], "/")
			if strings.HasPrefix(path, prefix+"/") || path == prefix {
				return true
			}
		}

		// If pattern is like "**/target/**", match if path contains "/target/"
		for _, part := range parts {
			if part != "" && part != "/" {
				part = strings.Trim(part, "/")
				if strings.Contains(path, "/"+part+"/") || strings.HasPrefix(path, part+"/") || strings.HasSuffix(path, "/"+part) {
					return true
				}
			}
		}
	}

	// Try exact match first
	matched, err := filepath.Match(pattern, path)
	if err == nil && matched {
		return true
	}

	// Try matching just the filename
	filename := filepath.Base(path)
	matched, err = filepath.Match(pattern, filename)
	if err == nil && matched {
		return true
	}

	// Check if any parent directory matches
	dir := filepath.Dir(path)
	for dir != "." && dir != "/" {
		if filepath.Base(dir) == strings.TrimSuffix(pattern, "/**") {
			return true
		}
		dir = filepath.Dir(dir)
	}

	return false
}

// DefaultPatterns returns the default ignore patterns
func DefaultPatterns() []string {
	return []string{
		// Build outputs
		"target/**",
		"build/**",
		"dist/**",
		"out/**",

		// Dependencies
		"node_modules/**",
		".pnp/**",

		// Generated code
		"**/*.min.js",
		"**/*.bundle.js",

		// Version control
		".git/**",

		// IDE
		".idea/**",
		".vscode/**",
		"*.iml",
	}
}
