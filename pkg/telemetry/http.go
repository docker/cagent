package telemetry

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"
)

// createEvent creates an EventPayload struct from eventType and properties
func (tc *Client) createEvent(eventName string, properties map[string]any) EventPayload {
	osInfo, _, osLanguage := getSystemInfo()

	// Create a new properties map that includes both user properties and system metadata
	allProperties := make(map[string]any)

	// Copy user-provided properties first
	for k, v := range properties {
		allProperties[k] = v
	}

	// Add system metadata to properties
	allProperties["user_uuid"] = userUUID
	allProperties["version"] = tc.version
	allProperties["os"] = osInfo
	allProperties["os_language"] = osLanguage

	event := EventPayload{
		Event:          EventType(eventName),
		EventTimestamp: time.Now().UnixMilli(),
		Source:         "cagent",
		Properties:     allProperties,
	}

	return event
}

// printEvent prints event in debug mode
func (tc *Client) printEvent(event *EventPayload) {
	output, err := json.MarshalIndent(event, "", "  ")
	if err != nil {
		tc.logger.Error("Failed to marshal telemetry event", "error", err)
		return
	}
	tc.logger.Info("event", "event", string(output))
}

// sendEvent sends a single event to Docker events API and handles logging
func (tc *Client) sendEvent(event *EventPayload) {
	// Send to Docker events API if conditions are met
	if tc.apiKey != "" && tc.endpoint != "" && tc.enabled {
		tc.logger.Debug("Sending telemetry event via HTTP", "event_type", event.Event, "endpoint", tc.endpoint)

		// Perform HTTP request inline
		if err := tc.performHTTPRequest(event); err != nil {
			tc.logger.Debug("Failed to send telemetry event to Docker API", "error", err, "event_type", event.Event)
		} else {
			tc.logger.Debug("Successfully sent telemetry event via HTTP", "event_type", event.Event)
		}
	} else {
		tc.logger.Debug("Skipping HTTP telemetry event - missing endpoint or API key or disabled",
			"event_type", event.Event,
			"has_endpoint", tc.endpoint != "",
			"has_api_key", tc.apiKey != "",
			"enabled", tc.enabled)
	}

	// Log the event
	logArgs := []any{
		"event", event.Event,
		"event_timestamp", event.EventTimestamp,
		"source", event.Source,
	}

	// Add key properties to log
	if userUUID, ok := event.Properties["user_uuid"].(string); ok {
		logArgs = append(logArgs, "user_uuid", userUUID)
	}
	if sessionID, ok := event.Properties["session_id"].(string); ok {
		logArgs = append(logArgs, "session_id", sessionID)
	}

	tc.logger.Debug("Event recorded", logArgs...)

	// Enhanced debug logging with full event structure
	if tc.logger.Enabled(context.Background(), slog.LevelDebug) {
		if jsonData, err := json.Marshal(event); err == nil {
			tc.logger.Debug("Full telemetry event JSON", "json", string(jsonData))
		}
	}
}

// performHTTPRequest handles the actual HTTP request to the telemetry API
func (tc *Client) performHTTPRequest(event *EventPayload) error {
	// Wrap event in records array to match MarlinRequest format
	requestBody := map[string]any{
		"records": []any{event},
	}

	// Serialize request to JSON
	jsonData, err := json.Marshal(requestBody)
	if err != nil {
		return fmt.Errorf("failed to marshal request to JSON: %w", err)
	}

	// Create HTTP request
	req, err := http.NewRequest(http.MethodPost, tc.endpoint, bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("failed to create HTTP request: %w", err)
	}

	// Set headers
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", fmt.Sprintf("cagent/%s", tc.version))
	if tc.apiKey != "" && tc.header != "" {
		req.Header.Set(tc.header, tc.apiKey)
	}

	// Debug: log request details
	tc.logger.Debug("HTTP request details",
		"method", req.Method,
		"url", req.URL.String(),
		"content_type", req.Header.Get("Content-Type"),
		"user_agent", req.Header.Get("User-Agent"),
		"has_header", req.Header.Get(tc.header) != "",
		"header_length", len(req.Header.Get(tc.header)),
		"payload_size", len(jsonData),
		"payload", string(jsonData),
	)

	// Send request with timeout context
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req = req.WithContext(ctx)

	resp, err := tc.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	// Check response status
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body := make([]byte, 1024) // Read up to 1KB of error response
		n, _ := resp.Body.Read(body)

		// Enhanced error logging with response details
		tc.logger.Debug("HTTP error response details",
			"status_code", resp.StatusCode,
			"status_text", resp.Status,
			"content_type", resp.Header.Get("Content-Type"),
			"content_length", resp.Header.Get("Content-Length"),
			"response_body", string(body[:n]),
		)

		return fmt.Errorf("HTTP request failed with status %d: %s", resp.StatusCode, string(body[:n]))
	}

	return nil
}
