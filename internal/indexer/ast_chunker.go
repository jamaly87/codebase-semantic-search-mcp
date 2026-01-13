package indexer

import (
	"fmt"
	"log"
	"regexp"
	"strings"
	"sync"

	"github.com/google/uuid"
	"github.com/jamaly87/codebase-semantic-search/internal/models"
	"github.com/jamaly87/codebase-semantic-search/pkg/config"
	sitter "github.com/smacker/go-tree-sitter"
	"github.com/smacker/go-tree-sitter/java"
	"github.com/smacker/go-tree-sitter/javascript"
	"github.com/smacker/go-tree-sitter/typescript/typescript"
)

// Tree-sitter node type constants
// These are the node type strings returned by Tree-sitter parsers.
// They are consistent for a given language parser but are defined by the Tree-sitter grammar,
// not by our code. If Tree-sitter grammar updates, these strings might change.
const (
	// Java node types
	nodeTypeJavaClass         = "class_declaration"
	nodeTypeJavaInterface     = "interface_declaration"
	nodeTypeJavaEnum          = "enum_declaration"
	nodeTypeJavaMethod        = "method_declaration"
	nodeTypeJavaConstructor   = "constructor_declaration"

	// JavaScript/TypeScript node types
	nodeTypeJSFunction        = "function_declaration"
	nodeTypeJSClass           = "class_declaration"
	nodeTypeJSMethod          = "method_definition"
	nodeTypeJSArrowFunction   = "arrow_function"
	nodeTypeJSFunctionExpr    = "function_expression"

	// TypeScript-specific node types
	nodeTypeTSInterface       = "interface_declaration"
	nodeTypeTSTypeAlias       = "type_alias_declaration"

	// Common identifier node types
	nodeTypeIdentifier        = "identifier"
	nodeTypeName              = "name"
	nodeTypePropertyID        = "property_identifier"
	nodeTypeTypeID            = "type_identifier"
	nodeTypeVariableDecl      = "variable_declarator"
)

// Chunking constants
const (
	// minChunkSizeBytes is the minimum size for a valid chunk (to skip incomplete declarations)
	minChunkSizeBytes = 10
	// defaultMaxChunkSizeBytes is the default maximum chunk size if not configured
	defaultMaxChunkSizeBytes = 4000
	// classSignatureMaxLines is the maximum number of lines to include in class signature
	classSignatureMaxLines = 50
	// classSummaryMaxMethods is the maximum number of method signatures to include in class summary
	classSummaryMaxMethods = 20
	// methodSignatureMaxLength is the maximum length for a method signature in class summary
	methodSignatureMaxLength = 100
	// overlapLinesRatio is the ratio of lines to use for overlap when splitting large chunks (1/10 = 10%)
	overlapLinesRatio = 10
	// maxOverlapLines is the maximum number of overlap lines when splitting chunks
	maxOverlapLines = 10
	// minOverlapLines is the minimum number of overlap lines when splitting chunks
	minOverlapLines = 1
)

// ASTChunker extracts semantic chunks using Tree-sitter AST parsing
// Tree-sitter parsers are NOT thread-safe, so all operations must be protected by a mutex
type ASTChunker struct {
	parsers map[string]*sitter.Parser
	mux     sync.Mutex // Protects parser access (Tree-sitter parsers are not thread-safe)
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
// Supports hierarchical chunking for large classes/interfaces
// Thread-safe: uses mutex to protect Tree-sitter parser access
func (ac *ASTChunker) ChunkByAST(repoPath, filePath, language, content string, cfg *config.ChunkingConfig) ([]models.CodeChunk, error) {
	ac.mux.Lock()
	parser, err := ac.getParser(language)
	if err != nil {
		ac.mux.Unlock()
		return nil, fmt.Errorf("parser not available for %s: %w", language, err)
	}

	// Parse the code (Tree-sitter parsers are NOT thread-safe)
	// We can release the lock after parsing since we're working with the tree, not the parser
	tree := parser.Parse(nil, []byte(content))
	ac.mux.Unlock()

	if tree == nil {
		return nil, fmt.Errorf("failed to parse file")
	}

	// Extract semantic nodes with hierarchical chunking if enabled
	// Tree operations are safe to do without the lock
	chunks := ac.extractSemanticNodes(tree, repoPath, filePath, language, content, cfg)

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
// Supports hierarchical chunking: splits large classes into summary + method chunks
func (ac *ASTChunker) extractSemanticNodes(tree *sitter.Tree, repoPath, filePath, language, content string, cfg *config.ChunkingConfig) []models.CodeChunk {
	var chunks []models.CodeChunk

	root := tree.RootNode()
	if root == nil {
		return chunks
	}

	// Get semantic node types for this language
	nodeTypes := ac.getSemanticNodeTypes(language)
	maxChunkSize := cfg.MaxChunkSizeBytes
	if maxChunkSize == 0 {
		maxChunkSize = defaultMaxChunkSizeBytes
	}

	// Walk the tree and extract semantic nodes
	ac.walkTree(root, content, nodeTypes, func(node *sitter.Node, nodeType string) {
		// Check if this is a large class/interface that should be split hierarchically
		if cfg.EnableHierarchicalChunking && ac.isLargeClassOrInterface(node, nodeType, content, maxChunkSize) {
			hierarchicalChunks := ac.createHierarchicalChunks(node, repoPath, filePath, language, content, nodeType, maxChunkSize)
			chunks = append(chunks, hierarchicalChunks...)
		} else {
			// Regular chunking for smaller nodes
			chunk := ac.createChunkFromNode(node, repoPath, filePath, language, content, nodeType)
			if chunk != nil {
				// If chunk is still too large, split it intelligently
				if len(chunk.Content) > maxChunkSize {
					splitChunks := ac.splitLargeChunk(chunk, content, maxChunkSize)
					chunks = append(chunks, splitChunks...)
				} else {
					chunks = append(chunks, *chunk)
				}
			}
		}
	})

	return chunks
}

// getSemanticNodeTypes returns AST node types to extract for each language
// These node type strings are defined by Tree-sitter grammars and are consistent
// for each language parser. They are NOT Go constants but grammar-defined strings.
func (ac *ASTChunker) getSemanticNodeTypes(language string) map[string]bool {
	nodeTypesMap := map[string][]string{
		"java": {
			nodeTypeJavaClass,
			nodeTypeJavaInterface,
			nodeTypeJavaEnum,
			nodeTypeJavaMethod,
			nodeTypeJavaConstructor,
		},
		"javascript": {
			nodeTypeJSFunction,
			nodeTypeJSClass,
			nodeTypeJSMethod,
			nodeTypeJSArrowFunction,
			nodeTypeJSFunctionExpr,
		},
		"typescript": {
			nodeTypeJSFunction,
			nodeTypeJSClass,
			nodeTypeTSInterface,
			nodeTypeTSTypeAlias,
			nodeTypeJSMethod,
			nodeTypeJSArrowFunction,
		},
	}

	types := nodeTypesMap[language]
	if types == nil {
		// Default semantic nodes (fallback for unknown languages)
		types = []string{
			nodeTypeJSFunction,
			nodeTypeJSClass,
			nodeTypeJavaMethod,
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
// nodeType is a string returned by Tree-sitter's node.Type() method.
//
// Guarantee: Tree-sitter node types are consistent for a given language parser.
// For example, in Java:
//   - Classes are always "class_declaration"
//   - Interfaces are always "interface_declaration"
//   - Methods are always "method_declaration"
//
// These strings are defined by the Tree-sitter grammar for each language and are
// stable within a parser version. However, they are NOT Go constants - they're
// runtime strings. If Tree-sitter grammar updates, these strings might change.
//
// We use constants (nodeTypeJavaClass, etc.) to document expected values and
// make the code more maintainable, but the actual guarantee comes from Tree-sitter.
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
	if len(strings.TrimSpace(chunkContent)) < minChunkSizeBytes {
		return nil
	}

	// Ensure chunk doesn't exceed safe size
	if len(chunkContent) > defaultMaxChunkSizeBytes {
		chunkContent = chunkContent[:defaultMaxChunkSizeBytes]
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
	// nodeType comes from Tree-sitter's node.Type() - these are grammar-defined strings
	classNodeTypes := []string{
		nodeTypeJavaClass,
		nodeTypeJavaInterface,
		nodeTypeJavaEnum,
		nodeTypeJSClass,
		nodeTypeTSInterface,
	}

	functionNodeTypes := []string{
		nodeTypeJSFunction,
		nodeTypeJavaMethod,
		nodeTypeJSMethod,
		nodeTypeJavaConstructor,
		nodeTypeJSArrowFunction,
		nodeTypeJSFunctionExpr,
	}

	switch {
	case contains(classNodeTypes, nodeType):
		chunk.ClassName = name
	case contains(functionNodeTypes, nodeType):
		chunk.FunctionName = name
	case nodeType == nodeTypeTSTypeAlias:
		chunk.ClassName = name // Treat type aliases as class-like
	default:
		// Log unexpected node types for debugging (but don't fail)
		// This helps identify if Tree-sitter grammar changes
		log.Printf("Unexpected node type in createChunkFromNode: %q (file: %s)", nodeType, filePath)
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
		// These node types are consistent across Tree-sitter grammars
		if childType == nodeTypeIdentifier || childType == nodeTypeName ||
		   childType == nodeTypePropertyID || childType == nodeTypeTypeID {
			start := child.StartByte()
			end := child.EndByte()
			if int(start) < int(end) && int(end) <= len(content) {
				return content[start:end]
			}
		}

		// For arrow functions and function expressions, look deeper
		if childType == nodeTypeVariableDecl {
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

// isLargeClassOrInterface checks if a node is a large class/interface that should be split hierarchically
// nodeType is a string returned by Tree-sitter's node.Type() method, which is defined by the grammar.
// For Java: classes are "class_declaration", interfaces are "interface_declaration", enums are "enum_declaration"
// For JavaScript/TypeScript: classes are "class_declaration", interfaces are "interface_declaration"
func (ac *ASTChunker) isLargeClassOrInterface(node *sitter.Node, nodeType string, content string, maxSize int) bool {
	// Only split classes and interfaces
	// These node types are defined by Tree-sitter grammars and are consistent for each language
	classNodeTypes := []string{
		nodeTypeJavaClass,
		nodeTypeJavaInterface,
		nodeTypeJavaEnum,
		nodeTypeJSClass,
		nodeTypeTSInterface,
	}

	if !contains(classNodeTypes, nodeType) {
		return false
	}

	// Check if node content exceeds max size
	startByte := node.StartByte()
	endByte := node.EndByte()
	if int(endByte-startByte) > maxSize {
		return true
	}

	return false
}

// createHierarchicalChunks creates a class summary chunk + individual method chunks
// This allows better search granularity for large classes
func (ac *ASTChunker) createHierarchicalChunks(node *sitter.Node, repoPath, filePath, language, content, nodeType string, maxSize int) []models.CodeChunk {
	var chunks []models.CodeChunk

	// Extract class name and create summary chunk
	className := ac.extractNodeName(node, content)
	startPoint := node.StartPoint()
	endPoint := node.EndPoint()
	startLine := int(startPoint.Row) + 1
	endLine := int(endPoint.Row) + 1

	// Create class summary chunk (signature + fields + brief method list)
	summaryContent := ac.createClassSummary(node, content, language)
	summaryChunk := &models.CodeChunk{
		ID:        uuid.New().String(),
		RepoPath:  repoPath,
		FilePath:  filePath,
		ChunkType: models.ChunkTypeClass,
		Content:   summaryContent,
		Language:  language,
		StartLine: startLine,
		EndLine:   endLine,
		ClassName: className,
	}

	// Ensure summary doesn't exceed max size
	if len(summaryChunk.Content) > maxSize {
		summaryChunk.Content = summaryChunk.Content[:maxSize]
	}

	chunks = append(chunks, *summaryChunk)
	summaryChunkID := summaryChunk.ID

	// Extract methods and create individual method chunks
	methodNodes := ac.extractMethodNodes(node, language)
	for _, methodNode := range methodNodes {
		methodChunk := ac.createChunkFromNode(methodNode, repoPath, filePath, language, content, "method_declaration")
		if methodChunk != nil {
			methodChunk.ParentChunkID = summaryChunkID
			methodChunk.ChunkType = models.ChunkTypeMethod
			methodChunk.ClassName = className // Preserve class context

			// If method is still too large, split it
			if len(methodChunk.Content) > maxSize {
				splitChunks := ac.splitLargeChunk(methodChunk, content, maxSize)
				chunks = append(chunks, splitChunks...)
			} else {
				chunks = append(chunks, *methodChunk)
			}
		}
	}

	return chunks
}

// createClassSummary creates a summary chunk for a class/interface
// Includes: class signature, fields, and method signatures
func (ac *ASTChunker) createClassSummary(node *sitter.Node, content string, language string) string {
	var summary strings.Builder

	// Get class declaration (first few lines)
	startByte := node.StartByte()
	endByte := node.EndByte()
	if int(endByte) > len(content) {
		endByte = uint32(len(content))
	}

	// Extract class signature (first ~20 lines or until first method)
	classContent := content[startByte:endByte]
	lines := strings.Split(classContent, "\n")

	// Find first method/constructor to determine where signature ends
	signatureEnd := len(lines)
	for i, line := range lines {
		if i > classSignatureMaxLines {
			signatureEnd = i
			break
		}
		// Check if this line starts a method (language-specific patterns)
		trimmed := strings.TrimSpace(line)
		if ac.isMethodStart(trimmed, language) {
			signatureEnd = i
			break
		}
	}

	// Write class signature
	for i := 0; i < signatureEnd && i < len(lines); i++ {
		summary.WriteString(lines[i])
		summary.WriteString("\n")
	}

	// Add method list if there are methods
	methodNodes := ac.extractMethodNodes(node, language)
	if len(methodNodes) > 0 {
		summary.WriteString("\n// Methods:\n")
		for i, methodNode := range methodNodes {
			if i >= classSummaryMaxMethods {
				summary.WriteString(fmt.Sprintf("// ... and %d more methods\n", len(methodNodes)-classSummaryMaxMethods))
				break
			}
			methodName := ac.extractNodeName(methodNode, content)
			if methodName != "" {
				// Extract method signature (first line)
				methodStart := methodNode.StartByte()
				methodEnd := methodNode.EndByte()
				if int(methodEnd) <= len(content) {
					methodLines := strings.Split(content[methodStart:methodEnd], "\n")
					if len(methodLines) > 0 {
						// Get first line (signature) and truncate if too long
						sig := strings.TrimSpace(methodLines[0])
						if len(sig) > methodSignatureMaxLength {
							sig = sig[:methodSignatureMaxLength] + "..."
						}
						summary.WriteString(fmt.Sprintf("// - %s\n", sig))
					}
				}
			}
		}
	}

	return summary.String()
}

// extractMethodNodes extracts method/function nodes from a class/interface
func (ac *ASTChunker) extractMethodNodes(classNode *sitter.Node, language string) []*sitter.Node {
	var methods []*sitter.Node

	// Language-specific method node types
	// These are Tree-sitter grammar-defined strings, consistent for each language parser
	methodTypes := map[string][]string{
		"java":       {nodeTypeJavaMethod, nodeTypeJavaConstructor},
		"javascript": {nodeTypeJSMethod, nodeTypeJSFunction},
		"typescript": {nodeTypeJSMethod, nodeTypeJSFunction},
	}

	types := methodTypes[language]
	if types == nil {
		// Default fallback - use Java method type as generic default
		types = []string{nodeTypeJavaMethod}
	}

	typeMap := make(map[string]bool)
	for _, t := range types {
		typeMap[t] = true
	}

	// Walk class body to find methods
	ac.walkTree(classNode, "", typeMap, func(node *sitter.Node, nodeType string) {
		// Only include direct children (methods of the class, not nested classes)
		if node.Parent() == classNode || (node.Parent() != nil && node.Parent().Parent() == classNode) {
			methods = append(methods, node)
		}
	})

	return methods
}

// isMethodStart checks if a line starts a method declaration
func (ac *ASTChunker) isMethodStart(line string, language string) bool {
	patterns := map[string][]string{
		"java": {
			`^\s*(public|private|protected)\s+.*\s+\w+\s*\(`,
			`^\s*@\w+.*\s+\w+\s*\(`,
		},
		"javascript": {
			`^\s*\w+\s*\(`,
			`^\s*async\s+\w+\s*\(`,
		},
		"typescript": {
			`^\s*(public|private|protected)?\s*\w+\s*\(`,
			`^\s*async\s+\w+\s*\(`,
		},
	}

	langPatterns := patterns[language]
	if langPatterns == nil {
		return false
	}

	for _, pattern := range langPatterns {
		matched, _ := regexp.MatchString(pattern, line)
		if matched {
			return true
		}
	}

	return false
}

// splitLargeChunk splits a large chunk at natural boundaries (statements, blocks)
func (ac *ASTChunker) splitLargeChunk(chunk *models.CodeChunk, fullContent string, maxSize int) []models.CodeChunk {
	var splitChunks []models.CodeChunk

	// For now, use simple line-based splitting with overlap
	// In a more sophisticated implementation, we'd parse the chunk's AST
	// and split at statement boundaries
	lines := strings.Split(chunk.Content, "\n")
	currentChunk := strings.Builder{}
	currentStartLine := chunk.StartLine

	// Determine overlap lines proportionally to the chunk size:
	// use ~10% of total lines, with at least 1 and at most 10 lines of overlap.
	overlapLines := len(lines) / overlapLinesRatio
	if overlapLines < minOverlapLines {
		overlapLines = minOverlapLines
	} else if overlapLines > maxOverlapLines {
		overlapLines = maxOverlapLines
	}
	for i, line := range lines {
		currentChunk.WriteString(line)
		currentChunk.WriteString("\n")

		// If we've accumulated enough content, create a chunk
		if currentChunk.Len() > maxSize && i < len(lines)-1 {
			chunkContent := currentChunk.String()
			if len(chunkContent) > maxSize {
				chunkContent = chunkContent[:maxSize]
			}

			newChunk := &models.CodeChunk{
				ID:           uuid.New().String(),
				RepoPath:     chunk.RepoPath,
				FilePath:     chunk.FilePath,
				ChunkType:    chunk.ChunkType,
				Content:      chunkContent,
				Language:     chunk.Language,
				StartLine:    currentStartLine,
				EndLine:      chunk.StartLine + i,
				FunctionName: chunk.FunctionName,
				ClassName:    chunk.ClassName,
				ParentChunkID: chunk.ParentChunkID,
			}

			splitChunks = append(splitChunks, *newChunk)

			// Start next chunk with overlap
			currentChunk.Reset()
			overlapStart := i - overlapLines
			if overlapStart < 0 {
				overlapStart = 0
			}
			for j := overlapStart; j <= i; j++ {
				currentChunk.WriteString(lines[j])
				currentChunk.WriteString("\n")
			}
			currentStartLine = chunk.StartLine + overlapStart
		}
	}

	// Add remaining content as final chunk
	if currentChunk.Len() > 0 {
		chunkContent := currentChunk.String()
		if len(chunkContent) > maxSize {
			chunkContent = chunkContent[:maxSize]
		}

		finalChunk := &models.CodeChunk{
			ID:           uuid.New().String(),
			RepoPath:     chunk.RepoPath,
			FilePath:     chunk.FilePath,
			ChunkType:    chunk.ChunkType,
			Content:      chunkContent,
			Language:     chunk.Language,
			StartLine:    currentStartLine,
			EndLine:      chunk.EndLine,
			FunctionName: chunk.FunctionName,
			ClassName:    chunk.ClassName,
			ParentChunkID: chunk.ParentChunkID,
		}

		splitChunks = append(splitChunks, *finalChunk)
	}

	return splitChunks
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