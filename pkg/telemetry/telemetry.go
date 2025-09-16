// Package telemetry provides anonymous usage tracking for cagent.
//
// This package implements a comprehensive telemetry system that collects anonymous
// usage data to help improve the tool. All events are processed asynchronously
// and never block command execution. Telemetry can be disabled at any time.
//
// The system tracks:
// - Command names and success/failure status
// - Agent names and model types
// - Tool names and whether calls succeed or fail
// - Token counts (input/output totals) and estimated costs
// - Session metadata (durations, error counts)
//
// The system does NOT collect:
// - User input or prompts
// - Agent responses or generated content
// - File contents or paths
// - API keys or credentials
// - Personally identifying information
//
// Files in this package:
// - client.go: Core client functionality, lifecycle management
// - events.go: Event tracking methods for sessions, tools, tokens
// - http.go: HTTP request handling and event transmission
// - global.go: Global telemetry functions and initialization
// - utils.go: Utility functions and system information collection
// - types.go: Event types and data structures
package telemetry
