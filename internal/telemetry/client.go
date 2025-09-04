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

	endpoint := getTelemetryEndpoint()
	apiKey := getTelemetryAPIKey()
	header := getTelemetryHeader()

	var httpClient *http.Client
	if len(customHttpClient) > 0 && customHttpClient[0] != nil {
		httpClient = customHttpClient[0]
	} else {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}

	client := &Client{
		logger:     telemetryLogger,
		enabled:    enabled,
		debugMode:  debugMode,
		httpClient: httpClient,
		endpoint:   endpoint,
		apiKey:     apiKey,
		header:     header,
		version:    version,
	}

	if debugMode {
		hasEndpoint := endpoint != ""
		hasAPIKey := apiKey != ""
		hasHeader := header != ""
		telemetryLogger.Debug("Telemetry configuration",
			"enabled", enabled,
			"has_endpoint", hasEndpoint,
			"has_api_key", hasAPIKey,
			"has_header", hasHeader,
			"http_enabled", hasEndpoint && hasAPIKey && hasHeader,
		)
	}

	return client, nil
}
