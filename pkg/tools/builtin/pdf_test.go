package builtin

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsPDFFile(t *testing.T) {
	t.Parallel()
	tests := []struct {
		path     string
		expected bool
	}{
		{"document.pdf", true},
		{"document.PDF", true},
		{"document.Pdf", true},
		{"path/to/document.pdf", true},
		{"/absolute/path/to/document.pdf", true},
		{"document.txt", false},
		{"document.pdf.txt", false},
		{"document", false},
		{"", false},
		{"pdf", false},
		{".pdf", true},
	}

	for _, tc := range tests {
		t.Run(tc.path, func(t *testing.T) {
			t.Parallel()
			result := isPDFFile(tc.path)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestReadPDFText_NonexistentFile(t *testing.T) {
	t.Parallel()
	_, err := readPDFText("/nonexistent/path/to/file.pdf")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to open PDF")
}

func TestReadPDFText_InvalidFile(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	invalidPDF := filepath.Join(tmpDir, "invalid.pdf")
	require.NoError(t, os.WriteFile(invalidPDF, []byte("not a valid pdf content"), 0o644))

	_, err := readPDFText(invalidPDF)
	require.Error(t, err)
}

func TestFilesystemTool_ReadFile_PDF(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	tool := NewFilesystemTool(tmpDir)

	invalidPDF := "test.pdf"
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, invalidPDF), []byte("not a valid pdf"), 0o644))

	result, err := tool.handleReadFile(t.Context(), ReadFileArgs{
		Path: invalidPDF,
	})
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Output, "failed to open PDF")
}

func TestFilesystemTool_ReadFile_PDFNotFound(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	tool := NewFilesystemTool(tmpDir)

	result, err := tool.handleReadFile(t.Context(), ReadFileArgs{
		Path: "nonexistent.pdf",
	})
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Output, "failed to open PDF")
}

func TestFilesystemTool_ReadMultipleFiles_WithPDF(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	tool := NewFilesystemTool(tmpDir)

	textFile := "test.txt"
	pdfFile := "test.pdf"
	textContent := "Hello, World!"

	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, textFile), []byte(textContent), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, pdfFile), []byte("invalid pdf"), 0o644))

	result, err := tool.handleReadMultipleFiles(t.Context(), ReadMultipleFilesArgs{
		Paths: []string{textFile, pdfFile},
	})
	require.NoError(t, err)
	assert.Contains(t, result.Output, textContent)
	assert.Contains(t, result.Output, "failed to open PDF")
}
