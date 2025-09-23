package telemetry

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/docker/cagent/pkg/paths"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

// Build-time telemetry configuration (set via -ldflags)
var (
	TelemetryEnabled  = "true" // Default enabled
	TelemetryEndpoint = ""     // Set at build time
	TelemetryAPIKey   = ""     // Set at build time
	TelemetryHeader   = ""     // Set at build time
)

// getSystemInfo collects system information for events
func getSystemInfo() (osName, osVersion, osLanguage string) {
	osInfo := runtime.GOOS
	osLang := os.Getenv("LANG")
	if osLang == "" {
		osLang = "en-US"
	}
	return osInfo, "", osLang
}

func GetTelemetryEnabled() bool {
	if env := os.Getenv("TELEMETRY_ENABLED"); env != "" {
		// Only disable if explicitly set to "false"
		return env != "false"
	}
	// Default to true (telemetry enabled)
	return true
}

func getTelemetryEndpoint() string {
	if env := os.Getenv("TELEMETRY_ENDPOINT"); env != "" {
		return env
	}
	return TelemetryEndpoint
}

func getTelemetryAPIKey() string {
	if env := os.Getenv("TELEMETRY_API_KEY"); env != "" {
		return env
	}
	return TelemetryAPIKey
}

func getTelemetryHeader() string {
	if env := os.Getenv("TELEMETRY_HEADER"); env != "" {
		return env
	}
	return TelemetryHeader
}

// getUserUUIDFilePath returns the path to the user UUID file
func getUserUUIDFilePath() string {
	configDir := paths.GetConfigDir()
	return filepath.Join(configDir, "user-uuid")
}

// getUserUUID gets or creates a persistent user UUID
func getUserUUID() string {
	uuidFile := getUserUUIDFilePath()

	// Try to read existing UUID
	if data, err := os.ReadFile(uuidFile); err == nil {
		existingUUID := strings.TrimSpace(string(data))
		if existingUUID != "" {
			return existingUUID
		}
		// UUID file exists but is empty/invalid - will generate new one
	} else {
		// UUID file cannot be read (likely first run) - will generate new one
	}

	// Generate new UUID and save it
	newUUID := uuid.New().String()
	if err := saveUserUUID(newUUID); err != nil {
		// If we can't save, still return a UUID for this session
		// but it won't persist across runs
		return newUUID
	}

	return newUUID
}

// saveUserUUID saves the UUID to disk
func saveUserUUID(newUUID string) error {
	uuidFile := getUserUUIDFilePath()

	// Ensure directory exists
	dir := filepath.Dir(uuidFile)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	// Write UUID to file (readable only by user)
	return os.WriteFile(uuidFile, []byte(newUUID), 0o600)
}

// structToMap converts a struct to map[string]any using JSON marshaling
// This automatically handles all fields and respects JSON tags (including omitempty)
func structToMap(v any) (map[string]any, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal struct: %w", err)
	}

	var result map[string]any
	err = json.Unmarshal(data, &result)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal to map: %w", err)
	}

	return result, nil
}

// CommandInfo represents the parsed command information
type CommandInfo struct {
	Action string
	Args   []string
	Flags  []string
}

// BuildCommandInfo extracts detailed command information for telemetry
func BuildCommandInfo(cmd *cobra.Command, args []string, baseName string) CommandInfo {
	info := CommandInfo{
		Action: baseName,
		Args:   []string{},
		Flags:  []string{},
	}

	// Only capture arguments for specific commands where they provide valuable context
	shouldCaptureArgs := baseName == "run" || baseName == "pull" || baseName == "catalog"

	if shouldCaptureArgs {
		// Add subcommands from args (first non-flag arguments)
		for _, arg := range args {
			if !strings.HasPrefix(arg, "-") {
				info.Args = append(info.Args, arg)
			} else {
				// Stop at first flag
				break
			}
		}
	}

	// Add important flags that provide context
	if cmd.Flags() != nil {
		// Check for help flag
		if help, _ := cmd.Flags().GetBool("help"); help {
			info.Flags = append(info.Flags, "--help")
		}

		// Check for version flag (if it exists)
		if cmd.Flags().Lookup("version") != nil {
			if version, _ := cmd.Flags().GetBool("version"); version {
				info.Flags = append(info.Flags, "--version")
			}
		}

		// Check for other commonly used flags (more relevant for run/pull commands)
		if shouldCaptureArgs {
			flagsToCheck := []string{"config", "agent", "model", "output", "format", "yolo"}
			for _, flagName := range flagsToCheck {
				if flag := cmd.Flags().Lookup(flagName); flag != nil && flag.Changed {
					info.Flags = append(info.Flags, "--"+flagName)
				}
			}
		}
	}

	return info
}
