package root

import (
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/docker/cagent/pkg/config"
	"github.com/docker/cagent/pkg/fake"
)

// setupFakeProxy starts a fake proxy if fakeResponses is non-empty.
// streamDelayMs controls simulated streaming: 0 = disabled, >0 = delay in milliseconds between chunks.
// It returns a cleanup function that must be called when done (typically via defer).
func setupFakeProxy(fakeResponses string, streamDelayMs int, runConfig *config.RuntimeConfig) (cleanup func() error, err error) {
	if fakeResponses == "" {
		return func() error { return nil }, nil
	}

	// Normalize path by stripping .yaml suffix (go-vcr adds it automatically)
	fakeResponses = strings.TrimSuffix(fakeResponses, ".yaml")

	var opts []fake.ProxyOption
	if streamDelayMs > 0 {
		opts = append(opts,
			fake.WithSimulateStream(true),
			fake.WithStreamChunkDelay(time.Duration(streamDelayMs)*time.Millisecond),
		)
	}

	proxyURL, cleanupFn, err := fake.StartProxy(fakeResponses, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to start fake proxy: %w", err)
	}

	runConfig.ModelsGateway = proxyURL
	slog.Info("Fake mode enabled", "cassette", fakeResponses, "proxy", proxyURL)

	return cleanupFn, nil
}

// setupRecordingProxy starts a recording proxy if recordPath is non-empty.
// It handles auto-generating a filename when recordPath is "true" (from NoOptDefVal),
// and normalizes the path by stripping any .yaml suffix.
// Returns the cassette path (with .yaml extension) and a cleanup function.
// The cleanup function must be called when done (typically via defer).
func setupRecordingProxy(recordPath string, runConfig *config.RuntimeConfig) (cassettePath string, cleanup func(), err error) {
	if recordPath == "" {
		return "", func() {}, nil
	}

	// Handle auto-generated filename (from NoOptDefVal)
	if recordPath == "true" {
		recordPath = fmt.Sprintf("cagent-recording-%d", time.Now().Unix())
	} else {
		recordPath = strings.TrimSuffix(recordPath, ".yaml")
	}

	proxyURL, cleanupFn, err := fake.StartRecordingProxy(recordPath)
	if err != nil {
		return "", nil, fmt.Errorf("failed to start recording proxy: %w", err)
	}

	runConfig.ModelsGateway = proxyURL
	cassettePath = recordPath + ".yaml"

	slog.Info("Recording mode enabled", "cassette", cassettePath, "proxy", proxyURL)

	return cassettePath, func() {
		if err := cleanupFn(); err != nil {
			slog.Error("Failed to cleanup recording proxy", "error", err)
		}
	}, nil
}
