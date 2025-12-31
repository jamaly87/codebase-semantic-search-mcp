package indexer

import (
	"path/filepath"
	"strings"

	"github.com/jamaly87/codebase-semantic-search/internal/models"
)

// LanguageDetector detects programming languages from file paths
type LanguageDetector struct {
	languages map[string]*models.Language
	extMap    map[string]string // extension -> language name
}

// NewLanguageDetector creates a new language detector
func NewLanguageDetector() *LanguageDetector {
	languages := map[string]*models.Language{
		"java": {
			Name:       "java",
			Extensions: []string{".java"},
			Parser:     "tree-sitter-java",
		},
		"typescript": {
			Name:       "typescript",
			Extensions: []string{".ts", ".tsx"},
			Parser:     "tree-sitter-typescript",
		},
		"javascript": {
			Name:       "javascript",
			Extensions: []string{".js", ".jsx", ".mjs", ".cjs"},
			Parser:     "tree-sitter-javascript",
		},
		"go": {
			Name:       "go",
			Extensions: []string{".go"},
			Parser:     "tree-sitter-go",
		},
	}

	// Build extension map
	extMap := make(map[string]string)
	for name, lang := range languages {
		for _, ext := range lang.Extensions {
			extMap[ext] = name
		}
	}

	return &LanguageDetector{
		languages: languages,
		extMap:    extMap,
	}
}

// Detect detects the language from a file path
func (ld *LanguageDetector) Detect(filePath string) (*models.Language, bool) {
	ext := strings.ToLower(filepath.Ext(filePath))
	if ext == "" {
		return nil, false
	}

	langName, ok := ld.extMap[ext]
	if !ok {
		return nil, false
	}

	lang, ok := ld.languages[langName]
	return lang, ok
}

// IsSupported returns true if the file extension is supported
func (ld *LanguageDetector) IsSupported(filePath string) bool {
	_, ok := ld.Detect(filePath)
	return ok
}

// GetLanguage returns a language by name
func (ld *LanguageDetector) GetLanguage(name string) (*models.Language, bool) {
	lang, ok := ld.languages[name]
	return lang, ok
}

// GetAllLanguages returns all supported languages
func (ld *LanguageDetector) GetAllLanguages() []*models.Language {
	langs := make([]*models.Language, 0, len(ld.languages))
	for _, lang := range ld.languages {
		langs = append(langs, lang)
	}
	return langs
}
