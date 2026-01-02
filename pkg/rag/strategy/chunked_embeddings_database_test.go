package strategy

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/docker/cagent/pkg/rag/database"
)

func TestChunkedVectorDB_ForeignKeyCascadeDelete(t *testing.T) {
	// This test verifies that foreign key constraints are enabled and
	// ON DELETE CASCADE works correctly. Without PRAGMA foreign_keys = ON,
	// SQLite silently ignores foreign key constraints.
	tmpFile := t.TempDir() + "/test_fk.db"
	defer os.Remove(tmpFile)

	db, err := newChunkedVectorDB(tmpFile, 3, "test")
	require.NoError(t, err)
	defer db.Close()

	ctx := t.Context()

	// Add a document with embedding
	doc := database.Document{
		ID:         "test_doc_0",
		SourcePath: "/test/file.go",
		ChunkIndex: 0,
		Content:    "test content",
		FileHash:   "abc123",
	}
	embedding := []float64{0.1, 0.2, 0.3}

	err = db.AddDocumentWithEmbedding(ctx, doc, embedding, "")
	require.NoError(t, err)

	// Verify the chunk exists
	results, err := db.SearchSimilarVectors(ctx, embedding, 10)
	require.NoError(t, err)
	assert.Len(t, results, 1, "Should have one chunk")

	// Delete the file metadata (parent row)
	// With foreign keys enabled, this should cascade delete the chunks
	err = db.DeleteFileMetadata(ctx, "/test/file.go")
	require.NoError(t, err)

	// Verify the chunk was also deleted due to CASCADE
	results, err = db.SearchSimilarVectors(ctx, embedding, 10)
	require.NoError(t, err)
	assert.Empty(t, results, "Chunks should be cascade deleted when file metadata is deleted")
}

func TestChunkedVectorDB_ForeignKeyConstraintEnforced(t *testing.T) {
	// This test verifies that foreign key constraints prevent inserting
	// orphan records (child rows without parent).
	tmpFile := t.TempDir() + "/test_fk_constraint.db"
	defer os.Remove(tmpFile)

	db, err := newChunkedVectorDB(tmpFile, 3, "test")
	require.NoError(t, err)
	defer db.Close()

	ctx := t.Context()

	// Try to insert a chunk directly without a parent file entry
	// This should fail if foreign keys are enabled
	embJSON := []byte(`[0.1, 0.2, 0.3]`)
	_, err = db.db.ExecContext(ctx,
		`INSERT INTO test_chunks (source_path, chunk_index, content, embedding) VALUES (?, ?, ?, ?)`,
		"/nonexistent/file.go", 0, "orphan content", embJSON)

	// With foreign keys enabled, this insert should fail
	assert.Error(t, err, "Insert should fail due to foreign key constraint violation")
}
