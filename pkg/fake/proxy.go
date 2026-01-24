// Package fake provides a VCR-based proxy for replaying recorded AI API responses.
// This is useful for E2E testing without making real API calls.
package fake

import (
	"bytes"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strings"

	"github.com/labstack/echo/v4"
	"gopkg.in/dnaeon/go-vcr.v4/pkg/cassette"
	"gopkg.in/dnaeon/go-vcr.v4/pkg/recorder"
)

// StartProxy starts an internal HTTP proxy that replays cassette responses.
// It returns the proxy URL and a cleanup function that should be called when done.
func StartProxy(cassettePath string) (string, func() error, error) {
	return StartProxyWithOptions(cassettePath, recorder.ModeReplayOnly, nil, nil)
}

// StartRecordingProxy starts a proxy that records AI API interactions to a cassette file.
// It injects API keys from environment variables for the actual API calls.
// The recorded cassette can later be replayed using StartProxy.
func StartRecordingProxy(cassettePath string) (string, func() error, error) {
	return StartProxyWithOptions(cassettePath, recorder.ModeRecordOnce, nil, APIKeyHeaderUpdater)
}

// APIKeyHeaderUpdater injects API keys from environment variables into request headers.
// This is used when recording API interactions to ensure real API calls succeed.
func APIKeyHeaderUpdater(host string, req *http.Request) {
	switch host {
	case "https://api.openai.com/v1":
		req.Header.Set("Authorization", "Bearer "+os.Getenv("OPENAI_API_KEY"))
	case "https://api.anthropic.com":
		req.Header.Del("Authorization")
		req.Header.Set("X-Api-Key", os.Getenv("ANTHROPIC_API_KEY"))
	case "https://generativelanguage.googleapis.com":
		req.Header.Del("Authorization")
		req.Header.Set("X-Goog-Api-Key", os.Getenv("GOOGLE_API_KEY"))
	case "https://api.mistral.ai/v1":
		req.Header.Set("Authorization", "Bearer "+os.Getenv("MISTRAL_API_KEY"))
	}
}

// StartProxyWithOptions starts an internal HTTP proxy with configurable options.
// - mode: recorder mode (ModeReplayOnly, ModeRecordOnce, etc.)
// - matcher: custom matcher function (nil uses DefaultMatcher)
// - headerUpdater: optional function to update request headers (for recording with real API keys)
func StartProxyWithOptions(
	cassettePath string,
	mode recorder.Mode,
	matcher recorder.MatcherFunc,
	headerUpdater func(host string, req *http.Request),
) (string, func() error, error) {
	hasMatcher := matcher != nil
	if !hasMatcher {
		matcher = DefaultMatcher(nil)
	}

	transport, err := recorder.New(cassettePath,
		recorder.WithMode(mode),
		recorder.WithMatcher(matcher),
		recorder.WithSkipRequestLatency(true),
		recorder.WithHook(RemoveHeadersHook, recorder.AfterCaptureHook),
		recorder.WithReplayableInteractions(false),
	)
	if err != nil {
		return "", nil, fmt.Errorf("failed to create VCR recorder: %w", err)
	}

	e := echo.New()
	e.HideBanner = true
	e.HidePort = true
	e.Any("/*", Handle(transport, headerUpdater))

	httpServer := httptest.NewServer(e)

	cleanup := func() error {
		httpServer.Close()
		return transport.Stop()
	}

	return httpServer.URL, cleanup, nil
}

// RemoveHeadersHook strips headers from recorded interactions for security.
func RemoveHeadersHook(i *cassette.Interaction) error {
	i.Request.Headers = map[string][]string{}
	i.Response.Headers = map[string][]string{}
	return nil
}

// DefaultMatcher creates a matcher that normalizes tool call IDs for consistent matching.
// The onError callback is called if reading the request body fails (nil logs and returns false).
func DefaultMatcher(onError func(err error)) recorder.MatcherFunc {
	callIDRegex := regexp.MustCompile(`call_[a-z0-9\-]+`)

	return func(r *http.Request, i cassette.Request) bool {
		if r.Body == nil || r.Body == http.NoBody {
			return cassette.DefaultMatcher(r, i)
		}
		if r.Method != i.Method {
			return false
		}
		if r.URL.String() != i.URL {
			return false
		}

		reqBody, err := io.ReadAll(r.Body)
		if err != nil {
			if onError != nil {
				onError(err)
			} else {
				slog.Error("Failed to read request body for matching", "error", err)
			}
			return false
		}
		r.Body.Close()
		r.Body = io.NopCloser(bytes.NewBuffer(reqBody))

		// Normalize tool call IDs for matching
		return callIDRegex.ReplaceAllString(string(reqBody), "call_ID") == callIDRegex.ReplaceAllString(i.Body, "call_ID")
	}
}

// TargetURLForHost returns the target URL builder for a given forwarding host.
// Returns nil if the host is not recognized.
func TargetURLForHost(host string) func(req *http.Request) string {
	switch host {
	case "https://api.openai.com/v1":
		return func(req *http.Request) string {
			return "https://api.openai.com" + req.URL.Redacted()
		}
	case "https://api.anthropic.com":
		return func(req *http.Request) string {
			return "https://api.anthropic.com" + req.URL.Redacted()
		}
	case "https://generativelanguage.googleapis.com":
		return func(req *http.Request) string {
			return "https://generativelanguage.googleapis.com" + req.URL.Redacted()
		}
	case "https://api.mistral.ai/v1":
		return func(req *http.Request) string {
			return "https://api.mistral.ai" + req.URL.Redacted()
		}
	default:
		return nil
	}
}

// Handle creates an echo handler that proxies requests through the VCR transport.
// The headerUpdater is called with the host and request to update headers (e.g., for adding API keys).
func Handle(transport http.RoundTripper, headerUpdater func(host string, req *http.Request)) echo.HandlerFunc {
	return func(c echo.Context) error {
		ctx := c.Request().Context()

		host := c.Request().Header.Get("X-Cagent-Forward")
		host = strings.TrimSuffix(host, "/")

		toTargetURL := TargetURLForHost(host)
		if toTargetURL == nil {
			return echo.NewHTTPError(http.StatusBadRequest, "unknown service host "+host)
		}

		targetURL := toTargetURL(c.Request())

		req, err := http.NewRequestWithContext(ctx, c.Request().Method, targetURL, c.Request().Body)
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "Failed to create new request")
		}

		maps.Copy(req.Header, c.Request().Header)

		if headerUpdater != nil {
			headerUpdater(host, req)
		}

		client := &http.Client{
			Timeout:   0, // no timeout, let ctx control it
			Transport: transport,
		}

		resp, err := client.Do(req)
		if err != nil {
			slog.Error("VCR proxy request failed", "url", targetURL, "error", err)
			return echo.NewHTTPError(http.StatusInternalServerError, "Failed to run request: "+err.Error())
		}
		defer resp.Body.Close()

		maps.Copy(c.Response().Header(), resp.Header)

		c.Response().WriteHeader(resp.StatusCode)

		if IsStreamResponse(resp) {
			return StreamCopy(c, resp)
		}

		_, err = io.Copy(c.Response().Writer, resp.Body)
		return err
	}
}

// StreamCopy copies a streaming response to the client.
func StreamCopy(c echo.Context, resp *http.Response) error {
	ctx := c.Request().Context()

	w := c.Response().Writer

	rf, ok := w.(io.ReaderFrom)
	if !ok {
		// fallback seguro
		_, err := io.Copy(w, resp.Body)
		return err
	}

	for {
		select {
		case <-ctx.Done():
			slog.WarnContext(ctx, "client disconnected, stop streaming")
			return nil
		default:
			n, err := rf.ReadFrom(io.LimitReader(resp.Body, 256))
			if n > 0 {
				if flusher, ok := w.(http.Flusher); ok {
					flusher.Flush()
				}
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

// IsStreamResponse checks if the response should be streamed.
func IsStreamResponse(resp *http.Response) bool {
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
