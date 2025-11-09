package e2e_test

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/require"
	"gopkg.in/dnaeon/go-vcr.v4/pkg/cassette"
	"gopkg.in/dnaeon/go-vcr.v4/pkg/recorder"

	"github.com/docker/cagent/pkg/config"
	"github.com/docker/cagent/pkg/environment"
)

func removeHeadersHook(i *cassette.Interaction) error {
	i.Request.Headers = map[string][]string{}
	i.Response.Headers = map[string][]string{}
	return nil
}

func customMatcher(t *testing.T) recorder.MatcherFunc {
	t.Helper()

	return func(r *http.Request, i cassette.Request) bool {
		if r.Body == nil || r.Body == http.NoBody {
			return cassette.DefaultMatcher(r, i)
		}

		reqBody, err := io.ReadAll(r.Body)
		require.NoError(t, err)

		r.Body.Close()
		r.Body = io.NopCloser(bytes.NewBuffer(reqBody))
		return r.Method == i.Method && r.URL.String() == i.URL && string(reqBody) == i.Body
	}
}

func startRecordingAIProxy(t *testing.T) (*httptest.Server, config.RuntimeConfig) {
	t.Helper()

	transport, err := recorder.New(filepath.Join("testdata", "cassettes", t.Name()),
		recorder.WithMode(recorder.ModeRecordOnce),
		recorder.WithMatcher(customMatcher(t)),
		recorder.WithSkipRequestLatency(true),
		recorder.WithHook(removeHeadersHook, recorder.AfterCaptureHook),
	)
	require.NoError(t, err)

	t.Cleanup(func() { require.NoError(t, transport.Stop()) })

	e := echo.New()
	e.Any("/*", handle(transport))

	httpServer := httptest.NewServer(e)
	t.Cleanup(httpServer.Close)

	return httpServer, config.RuntimeConfig{
		ModelsGateway: httpServer.URL,
		DefaultEnvProvider: &testEnvProvider{
			environment.DockerDesktopTokenEnv: "DUMMY",
		},
	}
}

func handle(transport http.RoundTripper) echo.HandlerFunc {
	return func(c echo.Context) error {
		ctx := c.Request().Context()

		host := c.Request().Header.Get("X-Cagent-Forward")
		host = strings.TrimSuffix(host, "/")

		var toTargetURL func(req *http.Request) string
		var updateHeaders func(req *http.Request)
		switch host {
		case "https://api.openai.com/v1":
			toTargetURL = func(req *http.Request) string {
				return "https://api.openai.com" + req.URL.Redacted()
			}
			updateHeaders = func(req *http.Request) {
				req.Header.Set("Authorization", "Bearer "+os.Getenv("OPENAI_API_KEY"))
			}
		case "https://api.anthropic.com":
			toTargetURL = func(req *http.Request) string {
				return "https://api.anthropic.com" + req.URL.Redacted()
			}
			updateHeaders = func(req *http.Request) {
				req.Header.Del("Authorization")
				req.Header.Set("X-Api-Key", os.Getenv("ANTHROPIC_API_KEY"))
			}
		case "https://generativelanguage.googleapis.com":
			toTargetURL = func(req *http.Request) string {
				return "https://generativelanguage.googleapis.com" + req.URL.Redacted()
			}
			updateHeaders = func(req *http.Request) {
				req.Header.Del("Authorization")
				req.Header.Set("X-Goog-Api-Key", os.Getenv("GOOGLE_API_KEY"))
			}
		case "https://api.mistral.ai/v1":
			toTargetURL = func(req *http.Request) string {
				return "https://api.mistral.ai" + req.URL.Redacted()
			}
			updateHeaders = func(req *http.Request) {
				req.Header.Set("Authorization", "Bearer "+os.Getenv("MISTRAL_API_KEY"))
			}
		default:
			return echo.NewHTTPError(http.StatusBadRequest, "unknown service host "+host)
		}

		targetURL := toTargetURL(c.Request())

		req, err := http.NewRequestWithContext(ctx, c.Request().Method, targetURL, c.Request().Body)
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "Failed to create new request")
		}

		maps.Copy(req.Header, c.Request().Header)
		updateHeaders(req)

		client := &http.Client{
			Timeout:   0, // no timeout, let ctx control it
			Transport: transport,
		}

		resp, err := client.Do(req)
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "Failed to run request"+err.Error())
		}
		defer resp.Body.Close()

		maps.Copy(c.Response().Header(), resp.Header)

		c.Response().WriteHeader(resp.StatusCode)

		if isStreamResponse(resp) {
			return streamCopy(c, resp)
		}

		_, err = io.Copy(c.Response().Writer, resp.Body)
		return err
	}
}

func streamCopy(c echo.Context, resp *http.Response) error {
	ctx := c.Request().Context()

	writer := c.Response().Writer.(io.ReaderFrom)

	for {
		select {
		case <-ctx.Done():
			slog.WarnContext(ctx, "client disconnected, stop streaming")
			return nil
		default:
			n, err := writer.ReadFrom(io.LimitReader(resp.Body, 256))
			if n > 0 {
				c.Response().Flush() // keep flushing to client
			}
			if err != nil {
				if err == io.EOF || ctx.Err() != nil {
					return nil
				}
				slog.ErrorContext(ctx, "stream read error", "error", err)
				return err
			}
		}
	}
}

func isStreamResponse(resp *http.Response) bool {
	ct := strings.ToLower(resp.Header.Get("Content-Type"))
	if strings.Contains(ct, "text/event-stream") {
		return true
	}

	te := strings.ToLower(resp.Header.Get("Transfer-Encoding"))
	if strings.Contains(te, "chunked") && !strings.Contains(ct, "application/json") {
		return true
	}

	return strings.Contains(ct, "application/octet-stream") ||
		strings.Contains(ct, "application/x-ndjson") ||
		strings.Contains(ct, "application/stream+json")
}

type testEnvProvider map[string]string

func (p *testEnvProvider) Get(_ context.Context, name string) string {
	return (*p)[name]
}
