package api

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
)

type MessageCursor struct {
	// Timestamp of the message (RFC3339 format)
	Timestamp string `json:"t"`
	// Index is the position in the flattened message array (handles duplicate timestamps)
	Index int `json:"i"`
}

func EncodeCursor(cursor MessageCursor) (string, error) {
	jsonBytes, err := json.Marshal(cursor)
	if err != nil {
		return "", fmt.Errorf("failed to marshal cursor: %w", err)
	}
	return base64.URLEncoding.EncodeToString(jsonBytes), nil
}

func DecodeCursor(encoded string) (MessageCursor, error) {
	var cursor MessageCursor

	if encoded == "" {
		return cursor, nil
	}

	jsonBytes, err := base64.URLEncoding.DecodeString(encoded)
	if err != nil {
		return cursor, fmt.Errorf("failed to decode cursor: %w", err)
	}

	if err := json.Unmarshal(jsonBytes, &cursor); err != nil {
		return cursor, fmt.Errorf("failed to unmarshal cursor: %w", err)
	}

	return cursor, nil
}
