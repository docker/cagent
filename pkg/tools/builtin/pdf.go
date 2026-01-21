package builtin

import (
	"bytes"
	"fmt"
	"io"
	"strings"

	"github.com/ledongthuc/pdf"
)

// readPDFText extracts plain text content from a PDF file.
func readPDFText(path string) (string, error) {
	f, r, err := pdf.Open(path)
	if err != nil {
		return "", fmt.Errorf("failed to open PDF: %w", err)
	}
	defer f.Close()

	var buf bytes.Buffer
	b, err := r.GetPlainText()
	if err != nil {
		return "", fmt.Errorf("failed to extract text from PDF: %w", err)
	}

	if _, err := io.Copy(&buf, b); err != nil {
		return "", fmt.Errorf("failed to read PDF text: %w", err)
	}

	return buf.String(), nil
}

// isPDFFile checks if a file path has a PDF extension.
func isPDFFile(path string) bool {
	return strings.HasSuffix(strings.ToLower(path), ".pdf")
}
