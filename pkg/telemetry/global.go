package telemetry

import (
	"context"
	"log/slog"
	"sync"
)

// TrackCommand records a command event using automatic telemetry initialization
func TrackCommand(action string, args []string) {
	// Automatically initialize telemetry if not already done
	EnsureGlobalTelemetryInitialized()

	if globalToolTelemetryClient != nil {
		ctx := context.Background()
		commandEvent := CommandEvent{
			Action:  action,
			Args:    args,
			Success: true, // We're tracking user intent, not outcome
		}
		globalToolTelemetryClient.Track(ctx, &commandEvent)
	}
}

// Global variables for simple tool telemetry
var (
	globalToolTelemetryClient *Client
	globalTelemetryOnce       sync.Once
	globalTelemetryVersion    = "unknown"
	globalTelemetryDebugMode  = false
)

// GetGlobalTelemetryClient returns the global telemetry client for adding to context
func GetGlobalTelemetryClient() *Client {
	EnsureGlobalTelemetryInitialized()
	return globalToolTelemetryClient
}

// SetGlobalTelemetryVersion sets the version for automatic telemetry initialization
// This should be called by the root package to provide the correct version
func SetGlobalTelemetryVersion(version string) {
	// If telemetry is already initialized, update the version
	if globalToolTelemetryClient != nil {
		globalToolTelemetryClient.version = version
	}
	// Store the version for future automatic initialization
	globalTelemetryVersion = version
}

// SetGlobalTelemetryDebugMode sets the debug mode for automatic telemetry initialization
// This should be called by the root package to pass the --debug flag state
func SetGlobalTelemetryDebugMode(debug bool) {
	globalTelemetryDebugMode = debug
}

// EnsureGlobalTelemetryInitialized ensures telemetry is initialized exactly once
// This handles all the setup automatically - no explicit initialization needed
func EnsureGlobalTelemetryInitialized() {
	globalTelemetryOnce.Do(func() {
		// Use the debug mode set by the root package via --debug flag
		debugMode := globalTelemetryDebugMode

		// Use the global default logger configured by the root command
		logger := slog.Default()

		// Get telemetry enabled setting
		enabled := GetTelemetryEnabled()

		// Use the version set by SetGlobalTelemetryVersion or default
		version := globalTelemetryVersion

		client := newClient(logger, enabled, debugMode, version)

		globalToolTelemetryClient = client

		if debugMode {
			// Use the telemetry logger wrapper for consistency
			telemetryLogger := NewTelemetryLogger(logger)
			telemetryLogger.Info("Auto-initialized telemetry", "enabled", enabled, "debug", debugMode)
		}
	})
}
