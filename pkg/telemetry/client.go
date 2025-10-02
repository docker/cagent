package telemetry

import (
	"context"
	"log/slog"
	"net/http"
	"time"
)

// telemetryLogger wraps slog.Logger to automatically prepend "[Telemetry]" to all messages
type telemetryLogger struct {
	logger *slog.Logger
}

// NewTelemetryLogger creates a new telemetry logger that automatically prepends "[Telemetry]" to all messages
func NewTelemetryLogger(logger *slog.Logger) *telemetryLogger {
	return &telemetryLogger{logger: logger}
}

// Debug logs a debug message with "[Telemetry]" prefix
func (tl *telemetryLogger) Debug(msg string, args ...any) {
	tl.logger.Debug("🔎 [Telemetry] "+msg, args...)
}

// Info logs an info message with "[Telemetry]" prefix
func (tl *telemetryLogger) Info(msg string, args ...any) {
	tl.logger.Info("💬 [Telemetry] "+msg, args...)
}

// Warn logs a warning message with "[Telemetry]" prefix
func (tl *telemetryLogger) Warn(msg string, args ...any) {
	tl.logger.Warn("⚠️ [Telemetry] "+msg, args...)
}

// Error logs an error message with "[Telemetry]" prefix
func (tl *telemetryLogger) Error(msg string, args ...any) {
	tl.logger.Error("❌ [Telemetry] "+msg, args...)
}

// Enabled returns whether the logger is enabled for the given level
func (tl *telemetryLogger) Enabled(ctx context.Context, level slog.Level) bool {
	return tl.logger.Enabled(ctx, level)
}

func NewClient(logger *slog.Logger, enabled, debugMode bool, version string, customHttpClient ...*http.Client) (*Client, error) {
	telemetryLogger := NewTelemetryLogger(logger)

	if !enabled {
		return &Client{
			logger:  telemetryLogger,
			enabled: false,
			version: version,
		}, nil
	}

	header := "x-api-key"

	endpoint := "https://api.docker.com/events/v1/track"
	apiKey := "Gxw1IjiDEP29dWm9DanuE2XhIKKzqDEY4iGlW1P0"

	// Use staging configuration in debug mode
	if debugMode {
		endpoint = "https://api-stage.docker.com/events/v1/track"
		apiKey = "z4sTQ8eDid2nJ53md8ptCaZlVxvIlhvf4AGR7oi5"
	}

	var httpClient *http.Client
	if len(customHttpClient) > 0 && customHttpClient[0] != nil {
		httpClient = customHttpClient[0]
	} else {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}

	client := &Client{
		logger:     telemetryLogger,
		userUUID:   getUserUUID(),
		enabled:    enabled,
		debugMode:  debugMode,
		httpClient: httpClient,
		endpoint:   endpoint,
		apiKey:     apiKey,
		header:     header,
		version:    version,
	}

	if debugMode {
		telemetryLogger.Debug("Enabled:", enabled)
	}

	return client, nil
}
