package oauthflow

import (
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCallbackServer_Port(t *testing.T) {
	t.Parallel()

	cs, err := NewCallbackServer(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cs.Shutdown(t.Context()) }()

	port := cs.Port()
	if port <= 0 || port > 65535 {
		t.Fatalf("Port() = %d, want a valid TCP port", port)
	}

	// Port() should agree with what's embedded in GetRedirectURI().
	if !strings.Contains(cs.GetRedirectURI(), ":"+strconv.Itoa(port)+"/callback") {
		t.Errorf("GetRedirectURI() = %q does not contain port %d", cs.GetRedirectURI(), port)
	}
}

func TestCallbackServerReservesPortBeforeStart(t *testing.T) {
	t.Parallel()

	cs, err := NewCallbackServer(t.Context())
	require.NoError(t, err)
	t.Cleanup(func() { _ = cs.listener.Close() })

	var lc net.ListenConfig
	listener, err := lc.Listen(t.Context(), "tcp", cs.listener.Addr().String())
	if listener != nil {
		t.Cleanup(func() { _ = listener.Close() })
	}
	require.Error(t, err, "the advertised port must already be reserved")

	other, err := NewCallbackServerOnPort(t.Context(), cs.Port())
	if other != nil {
		t.Cleanup(func() { _ = other.listener.Close() })
	}
	require.Error(t, err, "an occupied explicit port must fail during construction")
	assert.Nil(t, other)

	redirectURI := cs.GetRedirectURI()
	require.NoError(t, cs.Start())
	t.Cleanup(func() { _ = cs.Shutdown(t.Context()) })
	assert.Equal(t, redirectURI, cs.GetRedirectURI())
}

// TestBuildRedirectURI tests the pure string-substitution logic without
// needing to open a listener.
func TestBuildRedirectURI(t *testing.T) {
	t.Parallel()

	const fallback = "http://127.0.0.1:12345/callback"
	const port = 54321

	tests := []struct {
		name     string
		override string
		want     string
	}{
		{
			name:     "empty override falls back",
			override: "",
			want:     fallback,
		},
		{
			name:     "no placeholder returns override verbatim",
			override: "https://oauth.example.com/callback",
			want:     "https://oauth.example.com/callback",
		},
		{
			name:     "single placeholder is substituted",
			override: "https://oauth.example.com/redirect?port=${callbackPort}",
			want:     fmt.Sprintf("https://oauth.example.com/redirect?port=%d", port),
		},
		{
			name:     "multiple placeholders all substituted",
			override: "https://host:${callbackPort}/x/${callbackPort}",
			want:     fmt.Sprintf("https://host:%d/x/%d", port, port),
		},
		{
			name:     "unrelated dollar sequences are left alone",
			override: "https://x.example/cb?s=$other&p=${callbackPort}",
			want:     fmt.Sprintf("https://x.example/cb?s=$other&p=%d", port),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildRedirectURI(tt.override, fallback, port)
			if got != tt.want {
				t.Errorf("buildRedirectURI(%q, %q, %d) = %q, want %q", tt.override, fallback, port, got, tt.want)
			}
		})
	}
}

// TestCallbackServer_DuplicateCallbacksDoNotBlock guards against a regression
// where extra callbacks (stale browser tabs, page refreshes, or any local
// process probing the loopback port) blocked the HTTP handler goroutine on
// a full result channel. Sends are now non-blocking; the first callback
// wins and later ones must be dropped without wedging the server.
func TestCallbackServer_DuplicateCallbacksDoNotBlock(t *testing.T) {
	t.Parallel()

	cs, err := NewCallbackServer(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if err := cs.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cs.Shutdown(t.Context()) }()

	cs.SetExpectedState("expected-state")
	callbackURL := cs.GetRedirectURI() + "?code=authcode&state=expected-state"

	client := &http.Client{Timeout: 2 * time.Second, Transport: newTestTransport(t)}

	// Fire several callbacks back-to-back. Each one must complete (so the
	// handler goroutine isn't stuck) regardless of whether anyone is
	// reading from resultCh yet. The first one wins with HTTP 200; the
	// rest must report HTTP 409 (Conflict) rather than misleadingly
	// claiming success.
	for i := range 5 {
		req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, callbackURL, http.NoBody)
		if err != nil {
			t.Fatal(err)
		}
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("callback %d: %v", i, err)
		}
		resp.Body.Close()
		wantStatus := http.StatusOK
		if i > 0 {
			wantStatus = http.StatusConflict
		}
		if resp.StatusCode != wantStatus {
			t.Fatalf("callback %d: status = %d, want %d", i, resp.StatusCode, wantStatus)
		}
	}

	// The first callback must still be deliverable to the waiter.
	code, state, err := cs.WaitForCallback(t.Context())
	if err != nil {
		t.Fatalf("WaitForCallback: %v", err)
	}
	if code != "authcode" || state != "expected-state" {
		t.Errorf("got code=%q state=%q, want code=authcode state=expected-state", code, state)
	}
}

// TestCallbackServer_ResolveRedirectURI exercises the method wrapper end-to-end
// to make sure it stitches GetRedirectURI() and Port() together correctly.
func TestCallbackServer_ResolveRedirectURI(t *testing.T) {
	t.Parallel()

	cs, err := NewCallbackServer(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cs.Shutdown(t.Context()) }()

	if got := cs.ResolveRedirectURI(""); got != cs.GetRedirectURI() {
		t.Errorf("ResolveRedirectURI(\"\") = %q, want %q", got, cs.GetRedirectURI())
	}

	want := fmt.Sprintf("https://host.example/cb?port=%d", cs.Port())
	if got := cs.ResolveRedirectURI("https://host.example/cb?port=${callbackPort}"); got != want {
		t.Errorf("ResolveRedirectURI with placeholder = %q, want %q", got, want)
	}
}

// newTestTransport returns a transport private to the test. Parallel tests
// must not share http.DefaultTransport: httptest.Server.Close calls
// http.DefaultTransport.CloseIdleConnections, which can break another
// test's in-flight request ("transport connection broken").
func newTestTransport(t *testing.T) *http.Transport {
	t.Helper()
	tr := http.DefaultTransport.(*http.Transport).Clone()
	t.Cleanup(tr.CloseIdleConnections)
	return tr
}
