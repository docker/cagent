package strategy

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"golang.org/x/sync/errgroup"

	"github.com/docker/cagent/pkg/fsx"
	"github.com/docker/cagent/pkg/modelsdev"
	"github.com/docker/cagent/pkg/rag/chunk"
	"github.com/docker/cagent/pkg/rag/database"
	"github.com/docker/cagent/pkg/rag/embed"
	"github.com/docker/cagent/pkg/rag/treesitter"
	"github.com/docker/cagent/pkg/rag/types"
)

// vectorStoreDB is the internal database interface used by VectorStore.
type vectorStoreDB interface {
	AddDocumentWithEmbedding(ctx context.Context, doc database.Document, embedding []float64, embeddingInput string) error
	SearchSimilarVectors(ctx context.Context, queryEmbedding []float64, limit int) ([]VectorSearchResultData, error)
	DeleteDocumentsByPath(ctx context.Context, sourcePath string) error
	GetFileMetadata(ctx context.Context, sourcePath string) (*database.FileMetadata, error)
	SetFileMetadata(ctx context.Context, metadata database.FileMetadata) error
	GetAllFileMetadata(ctx context.Context) ([]database.FileMetadata, error)
	DeleteFileMetadata(ctx context.Context, sourcePath string) error
	Close() error
}

// VectorSearchResultData is the internal search result type used by VectorStore.
// It contains base document data plus similarity score.
type VectorSearchResultData struct {
	database.Document
	Embedding      []float64
	EmbeddingInput string // Only populated for semantic-embeddings
	Similarity     float64
}

// VectorStore provides shared embedding-based indexing and retrieval infrastructure.
// This is NOT a standalone strategy - it's infrastructure that strategies use.
type VectorStore struct {
	name         string
	db           vectorStoreDB
	embedder     *embed.Embedder
	docProcessor chunk.DocumentProcessor
	fileHashes   map[string]string
	fileHashesMu sync.Mutex // Protects fileHashes map for concurrent access
	watcher      *fsnotify.Watcher
	watcherMu    sync.Mutex
	events       chan<- types.Event
	shouldIgnore func(path string) bool // Optional filter for gitignore support

	similarityMetric string

	indexingTokens int64 // Track tokens used during indexing
	indexingCost   float64

	modelID     string // Full model ID (e.g., "openai/text-embedding-3-small") for pricing lookup
	modelsStore modelStore

	// embeddingInputBuilder controls how raw chunks are transformed into the
	// text that is actually embedded. By default it returns the raw chunk
	// content, but strategies can override this to include additional context
	// such as file paths or other metadata.
	embeddingInputBuilder EmbeddingInputBuilder

	// embeddingConcurrency controls how many embedding input builds run in parallel.
	// This speeds up strategies that call an LLM per chunk (semantic-embeddings).
	// For simple builders (chunked-embeddings), parallelism has no overhead.
	embeddingConcurrency int

	// fileIndexConcurrency controls how many files are indexed in parallel
	// during initialization. Higher values speed up indexing but use more
	// resources (CPU, GPU, memory, API rate limits).
	fileIndexConcurrency int

	// reindexMu prevents concurrent reindexing operations from the file watcher
	// to avoid overwhelming the system and causing event channel saturation.
	reindexMu sync.Mutex
}

type modelStore interface {
	GetModel(modelID string) (*modelsdev.Model, error)
}

// EmbeddingInputBuilder builds the string that will be sent to the embedding model
// for a given chunk. This allows strategies to customize the embedded text
// without changing the stored document content.
type EmbeddingInputBuilder interface {
	BuildEmbeddingInput(ctx context.Context, sourcePath string, ch chunk.Chunk) (string, error)
}

// DefaultEmbeddingInputBuilder returns the raw chunk content unchanged.
// This is the default used by chunked-embeddings strategy.
type DefaultEmbeddingInputBuilder struct{}

// BuildEmbeddingInput returns the raw chunk content.
func (d DefaultEmbeddingInputBuilder) BuildEmbeddingInput(_ context.Context, _ string, ch chunk.Chunk) (string, error) {
	return ch.Content, nil
}

// VectorStoreConfig holds configuration for creating a VectorStore.
type VectorStoreConfig struct {
	Name                 string
	Database             vectorStoreDB
	Embedder             *embed.Embedder
	Events               chan<- types.Event
	SimilarityMetric     string
	ModelID              string
	ModelsStore          modelStore
	EmbeddingConcurrency int
	FileIndexConcurrency int
	Chunking             ChunkingConfig
	ShouldIgnore         func(path string) bool // Optional filter for gitignore support
}

// NewVectorStore creates a new vector store with the given configuration.
func NewVectorStore(cfg VectorStoreConfig) *VectorStore {
	// Create the appropriate document processor based on config
	var dp chunk.DocumentProcessor
	if cfg.Chunking.CodeAware {
		dp = treesitter.NewDocumentProcessor(cfg.Chunking.Size, cfg.Chunking.Overlap, cfg.Chunking.RespectWordBoundaries)
	} else {
		dp = chunk.NewTextDocumentProcessor(cfg.Chunking.Size, cfg.Chunking.Overlap, cfg.Chunking.RespectWordBoundaries)
	}

	s := &VectorStore{
		name:                  cfg.Name,
		db:                    cfg.Database,
		embedder:              cfg.Embedder,
		docProcessor:          dp,
		fileHashes:            make(map[string]string),
		events:                cfg.Events,
		shouldIgnore:          cfg.ShouldIgnore,
		similarityMetric:      cfg.SimilarityMetric,
		modelID:               cfg.ModelID,
		modelsStore:           cfg.ModelsStore,
		embeddingInputBuilder: DefaultEmbeddingInputBuilder{},
		embeddingConcurrency:  cfg.EmbeddingConcurrency,
		fileIndexConcurrency:  cfg.FileIndexConcurrency,
	}

	// Set usage handler to calculate cost from models.dev and emit events with CUMULATIVE totals
	// This matches how chat completions calculate cost in runtime.go
	cfg.Embedder.SetUsageHandler(func(tokens int64, _ float64) {
		cost := s.calculateCost(tokens)
		s.recordUsage(tokens, cost)
	})

	return s
}

// SetEmbeddingInputBuilder allows callers to override how text is prepared
// before being sent to the embedding model. Passing nil resets to the default
// behavior (raw chunk content).
func (s *VectorStore) SetEmbeddingInputBuilder(builder EmbeddingInputBuilder) {
	if builder == nil {
		s.embeddingInputBuilder = DefaultEmbeddingInputBuilder{}
		return
	}
	s.embeddingInputBuilder = builder
}

// calculateCost calculates embedding cost using models.dev pricing
func (s *VectorStore) calculateCost(tokens int64) float64 {
	if s.modelsStore == nil || strings.HasPrefix(s.modelID, "dmr/") {
		return 0
	}

	model, err := s.modelsStore.GetModel(s.modelID)
	if err != nil {
		slog.Debug("Failed to get model pricing from models.dev, cost will be 0",
			"model_id", s.modelID,
			"error", err)
		return 0
	}

	if model.Cost == nil {
		return 0
	}

	// Embeddings only have input tokens, no output
	// Cost is per 1M tokens, so divide by 1e6
	return (float64(tokens) * model.Cost.Input) / 1e6
}

// RecordUsage records usage and emits a usage event with cumulative totals.
// This is exported so strategies can track additional usage (e.g., semantic LLM calls).
func (s *VectorStore) RecordUsage(tokens int64, cost float64) {
	s.recordUsage(tokens, cost)
}

func (s *VectorStore) recordUsage(tokens int64, cost float64) {
	if tokens == 0 && cost == 0 {
		return
	}

	s.indexingTokens += tokens
	s.indexingCost += cost

	// Emit usage event with CUMULATIVE totals for TUI
	s.emitEvent(types.Event{
		Type:        types.EventTypeUsage,
		TotalTokens: s.indexingTokens,
		Cost:        s.indexingCost,
	})
}

// Initialize indexes all documents
func (s *VectorStore) Initialize(ctx context.Context, docPaths []string, chunking ChunkingConfig) error {
	slog.Info("Starting vector store initialization",
		"name", s.name,
		"doc_paths", docPaths,
		"chunk_size", chunking.Size,
		"chunk_overlap", chunking.Overlap,
		"respect_word_boundaries", chunking.RespectWordBoundaries,
		"code_aware", chunking.CodeAware)

	// Load existing file hashes from metadata
	slog.Debug("Loading existing file hashes", "strategy", s.name)
	if err := s.loadExistingHashes(ctx); err != nil {
		slog.Warn("Failed to load existing file hashes", "strategy", s.name, "error", err)
	}

	// Collect all files
	slog.Debug("Collecting files", "strategy", s.name, "paths", docPaths)
	files, err := fsx.CollectFiles(docPaths, s.shouldIgnore)
	if err != nil {
		s.emitEvent(types.Event{Type: types.EventTypeError, Error: err})
		return fmt.Errorf("failed to collect files: %w", err)
	}

	// Track seen files for cleanup
	seenFilesForCleanup := make(map[string]bool)
	for _, f := range files {
		seenFilesForCleanup[f] = true
	}

	// Clean up orphaned documents
	if err := s.cleanupOrphanedDocuments(ctx, seenFilesForCleanup); err != nil {
		slog.Error("Failed to cleanup orphaned documents during initialization", "error", err)
	}

	if len(files) == 0 {
		slog.Warn("No files found for vector store", "name", s.name, "paths", docPaths)
		return nil
	}

	slog.Debug("Collected files for indexing check",
		"strategy", s.name,
		"file_count", len(files))

	// Determine which files need indexing
	type fileStatus struct {
		path          string
		needsIndexing bool
	}

	var fileStatuses []fileStatus
	seenFiles := make(map[string]bool)
	filesToIndex := 0

	for _, filePath := range files {
		seenFiles[filePath] = true

		needsIndexing, err := s.needsIndexing(ctx, filePath)
		if err != nil {
			slog.Error("Failed to check if file needs indexing",
				"path", filePath, "error", err)
			fileStatuses = append(fileStatuses, fileStatus{path: filePath, needsIndexing: false})
			continue
		}

		fileStatuses = append(fileStatuses, fileStatus{path: filePath, needsIndexing: needsIndexing})
		if needsIndexing {
			filesToIndex++
		}
	}

	if filesToIndex == 0 {
		slog.Info("All files up to date, no indexing needed",
			"name", s.name,
			"total_files", len(files))
		return nil
	}

	s.emitEvent(types.Event{Type: types.EventTypeIndexingStarted})

	// Index files that need it in parallel
	var indexed int
	var indexedMu sync.Mutex

	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(s.fileIndexConcurrency)

	for _, status := range fileStatuses {
		if !status.needsIndexing {
			slog.Debug("File unchanged, skipping", "path", status.path)
			continue
		}

		g.Go(func() error {
			// Check for context cancellation
			select {
			case <-gctx.Done():
				return gctx.Err()
			default:
			}

			// Index the file
			if err := s.indexFile(gctx, status.path); err != nil {
				slog.Error("Failed to index file", "path", status.path, "error", err)
				// Don't return error - continue indexing other files
				return nil
			}

			// Update progress counter with mutex protection
			indexedMu.Lock()
			indexed++
			current := indexed
			indexedMu.Unlock()

			// Emit progress event
			s.emitEvent(types.Event{
				Type: types.EventTypeIndexingProgress,
				Progress: &types.Progress{
					Current: current,
					Total:   filesToIndex,
				},
			})

			return nil
		})
	}

	// Wait for all files to be indexed
	if err := g.Wait(); err != nil {
		return err
	}

	if err := s.cleanupOrphanedDocuments(ctx, seenFiles); err != nil {
		slog.Error("Failed to cleanup orphaned documents", "error", err)
	}

	s.emitEvent(types.Event{Type: types.EventTypeIndexingComplete})

	slog.Info("Vector store initialization completed",
		"name", s.name,
		"total_files", len(files),
		"indexed", indexed,
		"total_tokens", s.indexingTokens,
		"total_cost", s.indexingCost)

	return nil
}

// Query searches for relevant documents using vector similarity
func (s *VectorStore) Query(ctx context.Context, query string, numResults int, threshold float64) ([]database.SearchResult, error) {
	queryEmbedding, err := s.embedder.Embed(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to generate query embedding: %w", err)
	}

	results, err := s.db.SearchSimilarVectors(ctx, queryEmbedding, numResults)
	if err != nil {
		return nil, fmt.Errorf("failed to search: %w", err)
	}

	// Convert internal result type to public SearchResult type
	var filtered []database.SearchResult
	for _, result := range results {
		if result.Similarity >= threshold {
			filtered = append(filtered, database.SearchResult{
				Document:   result.Document,
				Similarity: result.Similarity,
			})
		}
	}

	return filtered, nil
}

// CheckAndReindexChangedFiles checks for file changes and re-indexes if needed
func (s *VectorStore) CheckAndReindexChangedFiles(ctx context.Context, docPaths []string, chunking ChunkingConfig) error {
	files, err := fsx.CollectFiles(docPaths, s.shouldIgnore)
	if err != nil {
		return fmt.Errorf("failed to collect files: %w", err)
	}

	seenFiles := make(map[string]bool)

	for _, filePath := range files {
		seenFiles[filePath] = true

		needsIndexing, err := s.needsIndexing(ctx, filePath)
		if err != nil {
			slog.Error("Failed to check if file needs indexing", "path", filePath, "error", err)
			continue
		}

		if needsIndexing {
			slog.Info("File changed, re-indexing", "path", filePath)
			if err := s.indexFile(ctx, filePath); err != nil {
				slog.Error("Failed to re-index file", "path", filePath, "error", err)
			}
		}
	}

	if err := s.cleanupOrphanedDocuments(ctx, seenFiles); err != nil {
		slog.Error("Failed to cleanup orphaned documents during file watch", "error", err)
	}

	return nil
}

// StartFileWatcher starts monitoring files for changes
func (s *VectorStore) StartFileWatcher(ctx context.Context, docPaths []string, chunking ChunkingConfig) error {
	s.watcherMu.Lock()
	defer s.watcherMu.Unlock()

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("failed to create file watcher: %w", err)
	}
	s.watcher = watcher

	for _, docPath := range docPaths {
		if err := s.addPathToWatcher(docPath); err != nil {
			slog.Warn("Failed to watch path", "strategy", s.name, "path", docPath, "error", err)
			continue
		}
		slog.Debug("Watching path for changes", "strategy", s.name, "path", docPath)
	}

	go s.watchLoop(ctx, docPaths)

	slog.Info("File watcher started", "strategy", s.name, "paths", docPaths)
	return nil
}

// Close releases resources
func (s *VectorStore) Close() error {
	s.watcherMu.Lock()
	defer s.watcherMu.Unlock()

	var firstErr error

	// Close file watcher
	if s.watcher != nil {
		if err := s.watcher.Close(); err != nil {
			slog.Warn("Failed to close file watcher", "strategy", s.name, "error", err)
			firstErr = err
		}
		s.watcher = nil
	}

	// Close database connection
	if s.db != nil {
		if err := s.db.Close(); err != nil {
			slog.Error("Failed to close database", "strategy", s.name, "error", err)
			if firstErr == nil {
				firstErr = err
			}
		}
	}

	return firstErr
}

// GetIndexingUsage returns usage statistics from indexing
func (s *VectorStore) GetIndexingUsage() (tokens int64, cost float64) {
	return s.indexingTokens, s.indexingCost
}

// Helper methods

func (s *VectorStore) loadExistingHashes(ctx context.Context) error {
	metadata, err := s.db.GetAllFileMetadata(ctx)
	if err != nil {
		return fmt.Errorf("failed to get file metadata: %w", err)
	}

	s.fileHashesMu.Lock()
	defer s.fileHashesMu.Unlock()

	for _, meta := range metadata {
		s.fileHashes[meta.SourcePath] = meta.FileHash
		slog.Debug("Loaded file hash from metadata",
			"path", meta.SourcePath,
			"hash", meta.FileHash)
	}

	slog.Debug("Loaded existing file hashes from metadata",
		"strategy", s.name,
		"count", len(s.fileHashes))

	return nil
}

func (s *VectorStore) needsIndexing(_ context.Context, filePath string) (bool, error) {
	currentHash, err := chunk.FileHash(filePath)
	if err != nil {
		return false, fmt.Errorf("failed to hash file: %w", err)
	}

	// Read from fileHashes map with mutex protection
	s.fileHashesMu.Lock()
	storedHash, exists := s.fileHashes[filePath]
	s.fileHashesMu.Unlock()

	if !exists {
		slog.Debug("File not in metadata, needs indexing", "path", filePath)
		return true, nil
	}

	needsIndexing := storedHash != currentHash
	if needsIndexing {
		slog.Debug("File hash changed, needs re-indexing", "path", filePath)
	}
	return needsIndexing, nil
}

func (s *VectorStore) indexFile(ctx context.Context, filePath string) error {
	fileHash, err := chunk.FileHash(filePath)
	if err != nil {
		return fmt.Errorf("failed to hash file: %w", err)
	}

	if err := s.db.DeleteDocumentsByPath(ctx, filePath); err != nil {
		return fmt.Errorf("failed to delete old documents: %w", err)
	}

	chunks, err := chunk.ProcessFile(s.docProcessor, filePath)
	if err != nil {
		return fmt.Errorf("failed to process file: %w", err)
	}

	// Filter out empty chunks
	var validChunks []chunk.Chunk
	for _, ch := range chunks {
		if ch.Content == "" {
			continue
		}
		validChunks = append(validChunks, ch)
	}

	if len(validChunks) == 0 {
		slog.Debug("No valid chunks in file", "path", filePath)
		return nil
	}

	// Build embedding inputs (possibly in parallel for strategies that benefit)
	chunkContents, err := s.buildEmbeddingInputs(ctx, filePath, validChunks)
	if err != nil {
		return fmt.Errorf("failed to build embedding inputs: %w", err)
	}

	// Generate embeddings for all chunks in batch
	slog.Debug("Generating embeddings for file",
		"path", filePath,
		"chunk_count", len(validChunks))

	embeddings, err := s.embedder.EmbedBatch(ctx, chunkContents)
	if err != nil {
		return fmt.Errorf("failed to generate embeddings: %w", err)
	}

	if len(embeddings) != len(validChunks) {
		return fmt.Errorf("embedding count mismatch: got %d embeddings for %d chunks", len(embeddings), len(validChunks))
	}

	// Store all documents
	storedChunks := 0
	for i, ch := range validChunks {
		doc := database.Document{
			ID:         fmt.Sprintf("%s_%d_%d", filePath, ch.Index, time.Now().UnixNano()),
			SourcePath: filePath,
			ChunkIndex: ch.Index,
			Content:    ch.Content,
			FileHash:   fileHash,
		}

		// Pass embedding and embedding input separately - the database implementation
		// decides what to store based on strategy type (chunked vs semantic)
		if err := s.db.AddDocumentWithEmbedding(ctx, doc, embeddings[i], chunkContents[i]); err != nil {
			return fmt.Errorf("failed to add document: %w", err)
		}

		storedChunks++
	}

	metadata := database.FileMetadata{
		SourcePath: filePath,
		FileHash:   fileHash,
		ChunkCount: storedChunks,
	}
	if err := s.db.SetFileMetadata(ctx, metadata); err != nil {
		return fmt.Errorf("failed to update file metadata: %w", err)
	}

	// Update fileHashes map with mutex protection for concurrent indexing
	s.fileHashesMu.Lock()
	s.fileHashes[filePath] = fileHash
	s.fileHashesMu.Unlock()

	slog.Debug("Indexed file", "path", filePath, "chunks", storedChunks)
	return nil
}

// buildEmbeddingInputs transforms chunks into embedding input text using the
// configured builder. When embeddingConcurrency > 1, runs in parallel.
func (s *VectorStore) buildEmbeddingInputs(ctx context.Context, filePath string, chunks []chunk.Chunk) ([]string, error) {
	chunkContents := make([]string, len(chunks))

	if s.embeddingConcurrency > 1 {
		g, gctx := errgroup.WithContext(ctx)
		g.SetLimit(s.embeddingConcurrency)

		for i, ch := range chunks {
			g.Go(func() error {
				text, berr := s.embeddingInputBuilder.BuildEmbeddingInput(gctx, filePath, ch)
				if berr != nil || strings.TrimSpace(text) == "" {
					slog.Warn("Embedding input builder failed; falling back to raw chunk content",
						"strategy", s.name,
						"path", filePath,
						"chunk_index", ch.Index,
						"error", berr)
					text = ch.Content
				}
				chunkContents[i] = text
				return nil
			})
		}

		if err := g.Wait(); err != nil {
			return nil, err
		}
	} else {
		for i, ch := range chunks {
			text, berr := s.embeddingInputBuilder.BuildEmbeddingInput(ctx, filePath, ch)
			if berr != nil || strings.TrimSpace(text) == "" {
				slog.Warn("Embedding input builder failed; falling back to raw chunk content",
					"strategy", s.name,
					"path", filePath,
					"chunk_index", ch.Index,
					"error", berr)
				text = ch.Content
			}
			chunkContents[i] = text
		}
	}

	return chunkContents, nil
}

func (s *VectorStore) cleanupOrphanedDocuments(ctx context.Context, seenFiles map[string]bool) error {
	metadata, err := s.db.GetAllFileMetadata(ctx)
	if err != nil {
		return fmt.Errorf("failed to get file metadata: %w", err)
	}

	deletedCount := 0
	for _, meta := range metadata {
		if seenFiles[meta.SourcePath] {
			continue
		}

		slog.Info("Removing embeddings of orphaned documents", "path", meta.SourcePath)

		if err := s.db.DeleteDocumentsByPath(ctx, meta.SourcePath); err != nil {
			slog.Error("Failed to delete orphaned documents",
				"path", meta.SourcePath, "error", err)
			continue
		}

		if err := s.db.DeleteFileMetadata(ctx, meta.SourcePath); err != nil {
			slog.Error("Failed to delete orphaned metadata",
				"path", meta.SourcePath, "error", err)
			continue
		}

		s.fileHashesMu.Lock()
		delete(s.fileHashes, meta.SourcePath)
		s.fileHashesMu.Unlock()

		deletedCount++
	}

	if deletedCount > 0 {
		slog.Info("Cleaned up orphaned documents", "strategy", s.name, "count", deletedCount)
	}

	return nil
}

func (s *VectorStore) addPathToWatcher(path string) error {
	// Resolve path(s) using Processor (handles globs, directories, files)
	files, err := fsx.CollectFiles([]string{path}, s.shouldIgnore)
	if err != nil {
		return fmt.Errorf("failed to collect files for watching: %w", err)
	}

	if len(files) == 0 {
		slog.Debug("No files found to watch", "path", path)
		return nil
	}

	// Add directories of all found files to watcher
	watchedDirs := make(map[string]bool)
	for _, file := range files {
		dir := filepath.Dir(file)
		if !watchedDirs[dir] {
			if err := s.watcher.Add(dir); err != nil {
				slog.Warn("Failed to watch directory", "dir", dir, "error", err)
			} else {
				watchedDirs[dir] = true
				slog.Debug("Added directory to watcher", "dir", dir)
			}
		}
	}

	// If path is explicitly a directory, watch it recursively too
	info, err := os.Stat(path)
	if err == nil && info.IsDir() {
		if err := s.watcher.Add(path); err == nil {
			watchedDirs[path] = true
		}

		// Recursively add subdirectories
		_ = filepath.Walk(path, func(p string, info os.FileInfo, err error) error {
			if err == nil && info.IsDir() {
				if !watchedDirs[p] {
					if err := s.watcher.Add(p); err == nil {
						watchedDirs[p] = true
					}
				}
			}
			return nil
		})
	}

	return nil
}

func (s *VectorStore) watchLoop(ctx context.Context, docPaths []string) {
	var debounceTimer *time.Timer
	debounceDuration := 2 * time.Second
	pendingChanges := make(map[string]bool)
	var pendingMu sync.Mutex

	processChanges := func() {
		// Prevent concurrent reindexing operations
		if !s.reindexMu.TryLock() {
			slog.Debug("Skipping file change processing - reindexing already in progress", "strategy", s.name)
			return
		}
		defer s.reindexMu.Unlock()

		pendingMu.Lock()
		changedFiles := make([]string, 0, len(pendingChanges))
		for file := range pendingChanges {
			changedFiles = append(changedFiles, file)
		}
		pendingChanges = make(map[string]bool)
		pendingMu.Unlock()

		if len(changedFiles) == 0 {
			return
		}

		slog.Info("Processing file changes", "strategy", s.name, "count", len(changedFiles))

		filesToReindex := make([]string, 0)
		for _, file := range changedFiles {
			// Check if the file matches any of the configured document paths/patterns
			matches, matchErr := fsx.Matches(file, docPaths)
			if matchErr != nil {
				slog.Error("Failed to match path", "file", file, "error", matchErr)
				continue
			}
			if !matches {
				slog.Debug("File changed but does not match configured docs, ignoring", "path", file)
				continue
			}
			// Check if the file should be ignored (e.g., gitignore)
			if s.shouldIgnore != nil && s.shouldIgnore(file) {
				slog.Debug("File changed but is ignored by filter, skipping", "path", file)
				continue
			}

			needsIndexing, err := s.needsIndexing(ctx, file)
			if err != nil {
				slog.Debug("File no longer exists or inaccessible", "path", file, "error", err)
				continue
			}
			if needsIndexing {
				filesToReindex = append(filesToReindex, file)
			}
		}

		if len(filesToReindex) > 0 {
			s.emitEvent(types.Event{
				Type:    "indexing_started",
				Message: fmt.Sprintf("Re-indexing %d changed file(s)", len(filesToReindex)),
			})

			for i, file := range filesToReindex {
				s.emitEvent(types.Event{
					Type:    "indexing_progress",
					Message: fmt.Sprintf("Re-indexing: %s", filepath.Base(file)),
					Progress: &types.Progress{
						Current: i + 1,
						Total:   len(filesToReindex),
					},
				})

				if err := s.indexFile(ctx, file); err != nil {
					slog.Error("Failed to re-index file", "path", file, "error", err)
					s.emitEvent(types.Event{
						Type:    "error",
						Message: fmt.Sprintf("Failed to re-index: %s", filepath.Base(file)),
						Error:   err,
					})
				}
			}

			if err := s.cleanupOrphanedDocumentsFromDisk(ctx, docPaths); err != nil {
				slog.Error("Failed to cleanup orphaned documents", "error", err)
			}

			s.emitEvent(types.Event{
				Type:    "indexing_completed",
				Message: fmt.Sprintf("Re-indexed %d file(s)", len(filesToReindex)),
			})
		}
	}

	for {
		select {
		case <-ctx.Done():
			if debounceTimer != nil {
				debounceTimer.Stop()
			}
			slog.Info("File watcher stopped", "strategy", s.name)
			return

		case event, ok := <-s.watcher.Events:
			if !ok {
				return
			}

			if event.Op&(fsnotify.Write|fsnotify.Create|fsnotify.Remove|fsnotify.Rename) == 0 {
				continue
			}

			if event.Op&fsnotify.Create != 0 {
				s.watcherMu.Lock()
				if err := s.addPathToWatcher(event.Name); err != nil {
					slog.Debug("Could not watch new path", "path", event.Name, "error", err)
				}
				s.watcherMu.Unlock()
			}

			// Early filter: only track changes for files that match configured doc patterns
			matches, err := fsx.Matches(event.Name, docPaths)
			if err != nil {
				slog.Debug("Could not match path against doc patterns", "path", event.Name, "error", err)
				continue
			}
			if !matches {
				continue
			}
			// Skip files that should be ignored (e.g., gitignore)
			if s.shouldIgnore != nil && s.shouldIgnore(event.Name) {
				continue
			}

			slog.Debug("File system event detected",
				"strategy", s.name,
				"event", event.Op.String(),
				"path", event.Name)

			pendingMu.Lock()
			pendingChanges[event.Name] = true
			pendingMu.Unlock()

			if debounceTimer != nil {
				debounceTimer.Stop()
			}
			debounceTimer = time.AfterFunc(debounceDuration, processChanges)

		case err, ok := <-s.watcher.Errors:
			if !ok {
				return
			}
			slog.Error("File watcher error", "strategy", s.name, "error", err)
		}
	}
}

func (s *VectorStore) cleanupOrphanedDocumentsFromDisk(ctx context.Context, docPaths []string) error {
	files, err := fsx.CollectFiles(docPaths, s.shouldIgnore)
	if err != nil {
		return fmt.Errorf("failed to collect files: %w", err)
	}

	seenFiles := make(map[string]bool)
	for _, file := range files {
		seenFiles[file] = true
	}

	return s.cleanupOrphanedDocuments(ctx, seenFiles)
}

func (s *VectorStore) emitEvent(event types.Event) {
	EmitEvent(s.events, event, s.name)
}
