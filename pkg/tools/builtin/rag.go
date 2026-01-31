package builtin

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"slices"

	"github.com/docker/cagent/pkg/rag"
	"github.com/docker/cagent/pkg/tools"
)

// RAGTool provides document querying capabilities for a single RAG source
type RAGTool struct {
	manager  *rag.Manager
	toolName string
}

// Verify interface compliance
var (
	_ tools.ToolSet      = (*RAGTool)(nil)
	_ tools.Instructable = (*RAGTool)(nil)
)

// NewRAGTool creates a new RAG tool for a single RAG manager
// toolName is the name to use for the tool (typically from config or manager name)
func NewRAGTool(manager *rag.Manager, toolName string) *RAGTool {
	return &RAGTool{
		manager:  manager,
		toolName: toolName,
	}
}

type QueryRAGArgs struct {
	Query string `json:"query" jsonschema:"Search query"`
}

type QueryResult struct {
	SourcePath string  `json:"source_path" jsonschema:"Path to the source document"`
	Content    string  `json:"content" jsonschema:"Relevant document chunk content"`
	Similarity float64 `json:"similarity" jsonschema:"Similarity score (0-1)"`
	ChunkIndex int     `json:"chunk_index" jsonschema:"Index of the chunk within the source document"`
}

func (t *RAGTool) Instructions() string {
	if t.manager != nil {
		instruction := t.manager.ToolInstruction()
		if instruction != "" {
			return instruction
		}
	}

	// Default instruction if none provided
	return fmt.Sprintf("Search documents in %s to find relevant code or documentation. "+
		"Provide a clear search query describing what you need.", t.toolName)
}

func (t *RAGTool) Tools(context.Context) ([]tools.Tool, error) {
	var description string
	if t.manager != nil {
		description = t.manager.Description()
	}
	description = cmp.Or(description, fmt.Sprintf("Search project documents from %s to find relevant code or documentation. "+
		"Provide a natural language query describing what you need. "+
		"Returns the most relevant document chunks with file paths.", t.toolName))

	paramsSchema := tools.MustSchemaFor[QueryRAGArgs]()
	outputSchema := tools.MustSchemaFor[[]QueryResult]()

	// Log schemas for debugging
	if paramsJSON, err := json.Marshal(paramsSchema); err == nil {
		slog.Debug("RAG tool parameters schema",
			"tool_name", t.toolName,
			"schema", string(paramsJSON))
	}
	if outputJSON, err := json.Marshal(outputSchema); err == nil {
		slog.Debug("RAG tool output schema",
			"tool_name", t.toolName,
			"schema", string(outputJSON))
	}

	tool := tools.Tool{
		Name:         t.toolName,
		Category:     "knowledge",
		Description:  description,
		Parameters:   paramsSchema,
		OutputSchema: outputSchema,
		Handler:      tools.NewHandler(t.handleQueryRAG),
		Annotations: tools.ToolAnnotations{
			ReadOnlyHint: true,
			Title:        fmt.Sprintf("Query %s", t.toolName),
		},
	}

	slog.Debug("RAG tool registered",
		"tool_name", tool.Name,
		"category", tool.Category,
		"description", description,
		"title", tool.Annotations.Title,
		"read_only", tool.Annotations.ReadOnlyHint)

	return []tools.Tool{tool}, nil
}

func (t *RAGTool) handleQueryRAG(ctx context.Context, args QueryRAGArgs) (*tools.ToolCallResult, error) {
	if args.Query == "" {
		return nil, fmt.Errorf("query cannot be empty")
	}

	results, err := t.manager.Query(ctx, args.Query)
	if err != nil {
		slog.Error("RAG query failed", "rag", t.manager.Name(), "error", err)
		return nil, fmt.Errorf("RAG query failed: %w", err)
	}

	allResults := make([]QueryResult, 0, len(results))
	for _, result := range results {
		allResults = append(allResults, QueryResult{
			SourcePath: result.Document.SourcePath,
			Content:    result.Document.Content,
			Similarity: result.Similarity,
			ChunkIndex: result.Document.ChunkIndex,
		})
	}

	sortResults(allResults)

	maxResults := 10
	if len(allResults) > maxResults {
		allResults = allResults[:maxResults]
	}

	resultJSON, err := json.Marshal(allResults)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal results: %w", err)
	}

	return tools.ResultSuccess(string(resultJSON)), nil
}

// sortResults sorts query results by similarity in descending order
func sortResults(results []QueryResult) {
	slices.SortFunc(results, func(a, b QueryResult) int {
		return cmp.Compare(b.Similarity, a.Similarity) // Descending order
	})
}
