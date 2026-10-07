//go:build darwin || linux || windows

package desktop

import (
	"bytes"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	secretsengine "github.com/docker/secrets-engine/client"
	"github.com/docker/secrets-engine/x/secrets"
	"github.com/stretchr/testify/assert"
)

func TestSecretsEngineFailureLogging(t *testing.T) {
	t.Run("a missing engine is only logged at debug level", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			logs := captureLogs(t)
			installFakeBackend(t, &fakeBackend{token: makeToken(t, time.Now().Add(time.Hour))})
			installFakeEngine(t, &fakeEngine{err: fmt.Errorf("%w: dial unix engine.sock: connect: no such file or directory",
				secretsengine.ErrSecretsEngineNotAvailable)})

			GetToken(t.Context())
			assert.Equal(t, 0, countFailureLogs(logs, "WARN"))
			assert.Equal(t, 1, countFailureLogs(logs, "DEBUG"))
		})
	})

	t.Run("other failures are warned about once per run", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			logs := captureLogs(t)
			installFakeBackend(t, &fakeBackend{token: makeToken(t, time.Now().Add(time.Hour))})
			engine := &fakeEngine{err: secrets.ErrAccessDenied}
			installFakeEngine(t, engine)

			lookUp := func() {
				endSecretsEngineCooldown()
				expireCache()
				GetToken(t.Context())
			}

			lookUp()
			lookUp()
			assert.Equal(t, 1, countFailureLogs(logs, "WARN"), "a failure that persists is warned about once")
			assert.Equal(t, 1, countFailureLogs(logs, "DEBUG"))

			// The engine answers again, then fails: that's a new run.
			engine.setErr(nil)
			lookUp()
			engine.setErr(secrets.ErrAccessDenied)
			lookUp()
			assert.Equal(t, 2, countFailureLogs(logs, "WARN"))
		})
	})
}

// captureLogs records everything logged for the rest of the test.
func captureLogs(t *testing.T) *bytes.Buffer {
	t.Helper()

	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(previous) })
	return &logs
}

// countFailureLogs counts the engine failures logged at level.
func countFailureLogs(logs *bytes.Buffer, level string) int {
	n := 0
	for line := range strings.Lines(logs.String()) {
		if strings.Contains(line, "level="+level) && strings.Contains(line, `msg="`+secretsEngineFailureMsg+`"`) {
			n++
		}
	}
	return n
}
