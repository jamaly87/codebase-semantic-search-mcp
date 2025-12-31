package indexer

import (
	"fmt"
	"log"
	"strings"

	"github.com/google/uuid"
	"github.com/jamaly87/codebase-semantic-search/internal/models"
	sitter "github.com/smacker/go-tree-sitter"
	"github.com/smacker/go-tree-sitter/java"
	"github.com/smacker/go-tree-sitter/javascript"
	"github.com/smacker/go-tree-sitter/typescript/typescript"
)

// ASTChunker extracts semantic chunks using Tree-sitter AST parsing
type ASTChunker struct {
	parsers map[string]*sitter.Parser
}

// NewASTChunker creates a new AST-based chunker with language parsers
func NewASTChunker() (*ASTChunker, error) {
	ac := &ASTChunker{
		parsers: make(map[string]*sitter.Parser),
	}

	// Initialize parsers for supported languages
	ac.initializeParsers()

	return ac, nil
}

// initializeParsers sets up Tree-sitter parsers for each language
func (ac *ASTChunker) initializeParsers() {
	// Java parser
	javaParser := sitter.NewParser()
	javaParser.SetLanguage(java.GetLanguage())
	ac.parsers["java"] = javaParser

	// JavaScript parser
	jsParser := sitter.NewParser()
	jsParser.SetLanguage(javascript.GetLanguage())
	ac.parsers["javascript"] = jsParser

	// TypeScript parser
	tsParser := sitter.NewParser()
	tsParser.SetLanguage(typescript.GetLanguage())
	ac.parsers["typescript"] = tsParser

	log.Println("✓ AST parsers initialized: Java, JavaScript, TypeScript")
}

// ChunkByAST extracts semantic chunks (functions, classes, methods) using AST
func (ac *ASTChunker) ChunkByAST(repoPath, filePath, language, content string) ([]models.CodeChunk, error) {
	parser, err := ac.getParser(language)
	if err != nil {
		return nil, fmt.Errorf("parser not available for %s: %w", language, err)
	}

	// Parse the code
	tree := parser.Parse(nil, []byte(content))
	if tree == nil {
		return nil, fmt.Errorf("failed to parse file")
	}

	// Extract semantic nodes
	chunks := ac.extractSemanticNodes(tree, repoPath, filePath, language, content)

	return chunks, nil
}

// getParser returns a Tree-sitter parser for the given language
func (ac *ASTChunker) getParser(language string) (*sitter.Parser, error) {
	parser, ok := ac.parsers[language]
	if !ok {
		return nil, fmt.Errorf("parser not available for language: %s", language)
	}
	return parser, nil
}

// extractSemanticNodes extracts functions, classes, and methods from the AST
func (ac *ASTChunker) extractSemanticNodes(tree *sitter.Tree, repoPath, filePath, language, content string) []models.CodeChunk {
	var chunks []models.CodeChunk

	root := tree.RootNode()
	if root == nil {
		return chunks
	}

	// Get semantic node types for this language
	nodeTypes := ac.getSemanticNodeTypes(language)

	// Walk the tree and extract semantic nodes
	ac.walkTree(root, content, nodeTypes, func(node *sitter.Node, nodeType string) {
		chunk := ac.createChunkFromNode(node, repoPath, filePath, language, content, nodeType)
		if chunk != nil {
			chunks = append(chunks, *chunk)
		}
	})

	return chunks
}

// getSemanticNodeTypes returns AST node types to extract for each language
func (ac *ASTChunker) getSemanticNodeTypes(language string) map[string]bool {
	nodeTypesMap := map[string][]string{
		"java": {
			"class_declaration",
			"interface_declaration",
			"enum_declaration",
			"method_declaration",
			"constructor_declaration",
		},
		"javascript": {
			"function_declaration",
			"class_declaration",
			"method_definition",
			"arrow_function",
			"function_expression",
		},
		"typescript": {
			"function_declaration",
			"class_declaration",
			"interface_declaration",
			"type_alias_declaration",
			"method_definition",
			"arrow_function",
		},
	}

	types := nodeTypesMap[language]
	if types == nil {
		// Default semantic nodes
		types = []string{
			"function_declaration",
			"class_declaration",
			"method_declaration",
		}
	}

	// Convert to map for O(1) lookup
	typeMap := make(map[string]bool)
	for _, t := range types {
		typeMap[t] = true
	}
	return typeMap
}

// walkTree recursively walks the AST and calls callback for semantic nodes
func (ac *ASTChunker) walkTree(node *sitter.Node, content string, nodeTypes map[string]bool, callback func(*sitter.Node, string)) {
	if node == nil {
		return
	}

	nodeType := node.Type()

	// Check if this is a semantic node we care about
	if nodeTypes[nodeType] {
		callback(node, nodeType)
		// Still recurse into children to find nested functions/classes
	}

	// Recurse into children
	childCount := int(node.ChildCount())
	for i := 0; i < childCount; i++ {
		child := node.Child(i)
		if child != nil {
			ac.walkTree(child, content, nodeTypes, callback)
		}
	}
}

// createChunkFromNode creates a code chunk from an AST node
func (ac *ASTChunker) createChunkFromNode(node *sitter.Node, repoPath, filePath, language, content, nodeType string) *models.CodeChunk {
	if node == nil {
		return nil
	}

	// Get node content
	startByte := node.StartByte()
	endByte := node.EndByte()

	if startByte >= endByte || int(endByte) > len(content) {
		return nil
	}

	chunkContent := content[startByte:endByte]

	// Skip very small chunks (likely incomplete or just declarations)
	if len(strings.TrimSpace(chunkContent)) < 10 {
		return nil
	}

	// Ensure chunk doesn't exceed safe size
	const maxChunkSize = 4000
	if len(chunkContent) > maxChunkSize {
		chunkContent = chunkContent[:maxChunkSize]
	}

	// Get line numbers
	startPoint := node.StartPoint()
	endPoint := node.EndPoint()
	startLine := int(startPoint.Row) + 1
	endLine := int(endPoint.Row) + 1

	// Extract function/class name
	name := ac.extractNodeName(node, content)

	chunk := &models.CodeChunk{
		ID:        uuid.New().String(),
		RepoPath:  repoPath,
		FilePath:  filePath,
		ChunkType: models.ChunkTypeFunction,
		Content:   chunkContent,
		Language:  language,
		StartLine: startLine,
		EndLine:   endLine,
	}

	// Set function or class name based on node type
	switch {
	case contains([]string{"class_declaration", "interface_declaration", "enum_declaration"}, nodeType):
		chunk.ClassName = name
	case contains([]string{"function_declaration", "method_declaration", "method_definition", "constructor_declaration", "arrow_function", "function_expression"}, nodeType):
		chunk.FunctionName = name
	case nodeType == "type_alias_declaration":
		chunk.ClassName = name // Treat type aliases as class-like
	}

	return chunk
}

// extractNodeName tries to extract the name of a function/class from the AST node
func (ac *ASTChunker) extractNodeName(node *sitter.Node, content string) string {
	if node == nil {
		return ""
	}

	// Look for identifier child node
	childCount := int(node.ChildCount())
	for i := 0; i < childCount; i++ {
		child := node.Child(i)
		if child == nil {
			continue
		}

		childType := child.Type()

		// Check for identifier or name node
		if childType == "identifier" || childType == "name" ||
		   childType == "property_identifier" || childType == "type_identifier" {
			start := child.StartByte()
			end := child.EndByte()
			if int(start) < int(end) && int(end) <= len(content) {
				return content[start:end]
			}
		}

		// For arrow functions and function expressions, look deeper
		if childType == "variable_declarator" {
			name := ac.extractNodeName(child, content)
			if name != "" {
				return name
			}
		}
	}

	return ""
}

// contains checks if a slice contains a string
func contains(slice []string, str string) bool {
	for _, s := range slice {
		if s == str {
			return true
		}
	}
	return false
}

// CanParseLanguage checks if AST parsing is available for a language
func (ac *ASTChunker) CanParseLanguage(language string) bool {
	_, ok := ac.parsers[language]
	return ok
}

// Close cleans up resources
func (ac *ASTChunker) Close() {
	// smacker's tree-sitter doesn't require explicit parser cleanup
	ac.parsers = make(map[string]*sitter.Parser)
}

// LogParserStatus logs which languages have AST parsing available
func (ac *ASTChunker) LogParserStatus() {
	languages := []string{"java", "javascript", "typescript", "go", "python", "rust"}

	log.Println("AST Parser Status:")
	for _, lang := range languages {
		available := "✗ Not available (using token-based fallback)"
		if ac.CanParseLanguage(lang) {
			available = "✓ Available"
		}
		log.Printf("  %s: %s", lang, available)
	}
}