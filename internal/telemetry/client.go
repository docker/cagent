package telemetry

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"sync/atomic"
	"time"
)

// NewClient creates a new simplified telemetry client with explicit debug control
func NewClient(logger *slog.Logger, enabled, debugMode bool, version string) (*Client, error) {
	return NewClientWithHTTPClient(logger, enabled, debugMode, version, &http.Client{Timeout: 30 * time.Second})
}

// NewClientWithHTTPClient creates a new telemetry client with a custom HTTP client (useful for testing)
func NewClientWithHTTPClient(logger *slog.Logger, enabled, debugMode bool, version string, httpClient HTTPClient) (*Client, error) {
	// Debug mode only affects output destination, not whether telemetry is enabled
	// Respect the user's enabled setting

	if !enabled {
		return &Client{
			logger:  logger,
			enabled: false,
			version: version,
		}, nil
	}

	endpoint := getTelemetryEndpoint()
	apiKey := getTelemetryAPIKey()
	header := getTelemetryHeader()

	client := &Client{
		logger:     logger,
		enabled:    enabled,
		debugMode:  debugMode,
		httpClient: httpClient,
		endpoint:   endpoint,
		apiKey:     apiKey,
		header:     header,
		version:    version,
		eventChan:  make(chan EventWithContext, 1000), // Buffer for 1000 events
		stopChan:   make(chan struct{}),
		done:       make(chan struct{}),
	}

	if debugMode {
		hasEndpoint := endpoint != ""
		hasAPIKey := apiKey != ""
		hasHeader := header != ""
		logger.Debug("Telemetry configuration",
			"enabled", enabled,
			"has_endpoint", hasEndpoint,
			"has_api_key", hasAPIKey,
			"has_header", hasHeader,
			"http_enabled", hasEndpoint && hasAPIKey && hasHeader,
		)
	}

	// Start background event processor
	go client.processEvents()

	return client, nil
}

// IsEnabled returns whether telemetry is enabled
func (tc *Client) IsEnabled() bool {
	tc.mu.RLock()
	defer tc.mu.RUnlock()
	return tc.enabled
}

// Shutdown gracefully shuts down the telemetry client
func (tc *Client) Shutdown(ctx context.Context) error {
	tc.RecordSessionEnd(ctx)

	if !tc.enabled {
		return nil
	}

	// Signal shutdown to background goroutine
	close(tc.stopChan)

	// Wait for background processing to complete with timeout
	select {
	case <-tc.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(5 * time.Second):
		return fmt.Errorf("timeout waiting for telemetry shutdown")
	}
}

// processEvents runs in a background goroutine to process telemetry events
func (tc *Client) processEvents() {
	defer close(tc.done)

	if tc.debugMode {
		tc.logger.Debug("🔄 Background event processor started")
	}

	for {
		select {
		case event := <-tc.eventChan:
			if tc.debugMode {
				tc.logger.Debug("🔄 Processing event from channel", "event_type", event.eventName)
			}
			tc.processEvent(event)
		case <-tc.stopChan:
			if tc.debugMode {
				tc.logger.Debug("🛑 Background processor received stop signal")
			}
			// Drain remaining events before shutting down
			for {
				select {
				case event := <-tc.eventChan:
					if tc.debugMode {
						tc.logger.Debug("🔄 Draining event during shutdown", "event_type", event.eventName)
					}
					tc.processEvent(event)
				default:
					if tc.debugMode {
						tc.logger.Debug("🛑 Background processor shutting down")
					}
					return
				}
			}
		}
	}
}

// processEvent handles individual events in the background goroutine
func (tc *Client) processEvent(eventCtx EventWithContext) {
	// Track that we're processing this event
	atomic.AddInt64(&tc.requestCount, 1)
	defer atomic.AddInt64(&tc.requestCount, -1)

	event := tc.createEvent(eventCtx.eventName, eventCtx.properties)

	if tc.debugMode {
		tc.printEvent(&event)
	}

	// Always send the event (regardless of debug mode)
	tc.sendEvent(&event)
}
