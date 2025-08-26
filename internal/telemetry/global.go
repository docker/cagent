package telemetry

import (
	"context"
	"log/slog"
	"os"
	"sync"
)

// TrackCommand records a command event using automatic telemetry initialization
func TrackCommand(action string, args []string) {
	// Automatically initialize telemetry if not already done
	ensureGlobalTelemetryInitialized()

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

// SetGlobalToolTelemetryClient sets the global client for tool telemetry
// This allows other packages to record tool events without context passing
// This is now optional - if not called, automatic initialization will happen
func SetGlobalToolTelemetryClient(client *Client, logger *slog.Logger) {
	globalToolTelemetryClient = client
	// Logger is now handled internally by automatic initialization
}

// GetGlobalTelemetryClient returns the global telemetry client for adding to context
func GetGlobalTelemetryClient() *Client {
	ensureGlobalTelemetryInitialized()
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

// ensureGlobalTelemetryInitialized ensures telemetry is initialized exactly once
// This handles all the setup automatically - no explicit initialization needed
func ensureGlobalTelemetryInitialized() {
	globalTelemetryOnce.Do(func() {
		// Use the debug mode set by the root package via --debug flag
		debugMode := globalTelemetryDebugMode

		// Create logger with appropriate level based on debug mode
		logLevel := slog.LevelInfo
		if debugMode {
			logLevel = slog.LevelDebug
		}
		logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
			Level: logLevel,
		}))

		// Get telemetry enabled setting
		enabled := GetTelemetryEnabled()

		// Use the version set by SetGlobalTelemetryVersion or default
		version := globalTelemetryVersion

		// Try to initialize telemetry
		client, err := NewClient(logger, enabled, debugMode, version)
		if err != nil {
			// If initialization fails, create a disabled client
			client, _ = NewClient(logger, false, debugMode, version)
		}

		globalToolTelemetryClient = client

		if debugMode {
			logger.Info("Auto-initialized telemetry", "enabled", enabled, "debug", debugMode)
		}
	})
}

// EnsureGlobalTelemetryInitialized makes the private initialization function public
func EnsureGlobalTelemetryInitialized() {
	ensureGlobalTelemetryInitialized()
}
