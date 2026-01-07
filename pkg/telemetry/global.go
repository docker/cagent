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
	globalMu                  sync.RWMutex // protects globalTelemetryVersion and globalTelemetryDebugMode
)

// GetGlobalTelemetryClient returns the global telemetry client for adding to context
func GetGlobalTelemetryClient() *Client {
	EnsureGlobalTelemetryInitialized()
	return globalToolTelemetryClient
}

// SetGlobalTelemetryVersion sets the version for automatic telemetry initialization
// This should be called by the root package to provide the correct version
func SetGlobalTelemetryVersion(version string) {
	globalMu.Lock()
	defer globalMu.Unlock()

	// If telemetry is already initialized, update the version
	if globalToolTelemetryClient != nil {
		globalToolTelemetryClient.setVersion(version)
	}
	// Store the version for future automatic initialization
	globalTelemetryVersion = version
}

// SetGlobalTelemetryDebugMode sets the debug mode for automatic telemetry initialization
// This should be called by the root package to pass the --debug flag state
func SetGlobalTelemetryDebugMode(debug bool) {
	globalMu.Lock()
	defer globalMu.Unlock()
	globalTelemetryDebugMode = debug
}

// EnsureGlobalTelemetryInitialized ensures telemetry is initialized exactly once
// This handles all the setup automatically - no explicit initialization needed
func EnsureGlobalTelemetryInitialized() {
	globalTelemetryOnce.Do(func() {
		// Read global settings under the lock
		globalMu.RLock()
		debugMode := globalTelemetryDebugMode
		version := globalTelemetryVersion
		globalMu.RUnlock()

		// Use the global default logger configured by the root command
		logger := slog.Default()

		// Get telemetry enabled setting
		enabled := GetTelemetryEnabled()

		client := newClient(logger, enabled, debugMode, version)

		globalToolTelemetryClient = client

		if debugMode {
			// Use the telemetry logger wrapper for consistency
			telemetryLogger := NewTelemetryLogger(logger)
			telemetryLogger.Info("Auto-initialized telemetry", "enabled", enabled, "debug", debugMode)
		}
	})
}
