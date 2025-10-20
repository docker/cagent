package mcp

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"
)

// CallbackServer handles OAuth callback requests
type CallbackServer struct {
	server   *http.Server
	listener net.Listener
	mu       sync.Mutex

	// Channels for communicating the authorization code and state
	codeCh  chan string
	stateCh chan string
	errCh   chan error

	// Expected state parameter for CSRF protection
	expectedState string
}

// NewCallbackServer creates a new OAuth callback server
func NewCallbackServer() (*CallbackServer, error) {
	// Find an available port
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("failed to find available port: %w", err)
	}

	cs := &CallbackServer{
		listener: listener,
		codeCh:   make(chan string, 1),
		stateCh:  make(chan string, 1),
		errCh:    make(chan error, 1),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/callback", cs.handleCallback)

	cs.server = &http.Server{
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	return cs, nil
}

func (cs *CallbackServer) Start() error {
	go func() {
		if err := cs.server.Serve(cs.listener); err != nil && err != http.ErrServerClosed {
			slog.Error("Callback server error", "error", err)
		}
	}()

	slog.Info("OAuth callback server started", "address", cs.GetRedirectURI())
	return nil
}

func (cs *CallbackServer) GetRedirectURI() string {
	addr := cs.listener.Addr().String()
	return fmt.Sprintf("http://%s/callback", addr)
}

func (cs *CallbackServer) SetExpectedState(state string) {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	cs.expectedState = state
}

func (cs *CallbackServer) handleCallback(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()

	if errMsg := query.Get("error"); errMsg != "" {
		errDesc := query.Get("error_description")
		if errDesc != "" {
			errMsg = fmt.Sprintf("%s: %s", errMsg, errDesc)
		}

		cs.errCh <- fmt.Errorf("OAuth error: %s", errMsg)

		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprintf(w, `<!DOCTYPE html>
<html>
<head>
    <title>Authorization Failed</title>
    <style>
        body { font-family: Arial, sans-serif; padding: 50px; text-align: center; }
        .error { color: #d32f2f; }
    </style>
</head>
<body>
    <h1 class="error">Authorization Failed</h1>
    <p>%s</p>
    <p>You can close this window.</p>
</body>
</html>`, errMsg)
		return
	}

	code := query.Get("code")
	state := query.Get("state")

	if code == "" {
		cs.errCh <- fmt.Errorf("no authorization code received")
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprint(w, "No authorization code received")
		return
	}

	// Verify state parameter for CSRF protection
	cs.mu.Lock()
	expectedState := cs.expectedState
	cs.mu.Unlock()

	if expectedState != "" && state != expectedState {
		cs.errCh <- fmt.Errorf("state mismatch: expected %s, got %s", expectedState, state)
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprint(w, "Invalid state parameter")
		return
	}

	cs.codeCh <- code
	cs.stateCh <- state

	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, `<!DOCTYPE html>
<html>
<head>
    <title>Authorization Successful</title>
    <style>
        body { font-family: Arial, sans-serif; padding: 50px; text-align: center; }
        .success { color: #388e3c; }
    </style>
</head>
<body>
    <h1 class="success">Authorization Successful!</h1>
    <p>You have successfully authorized the application.</p>
    <p>You can close this window and return to the application.</p>
</body>
</html>`)
}

func (cs *CallbackServer) WaitForCallback(ctx context.Context) (code, state string, err error) {
	select {
	case code = <-cs.codeCh:
		select {
		case state = <-cs.stateCh:
			return code, state, nil
		case <-ctx.Done():
			return "", "", ctx.Err()
		}
	case err = <-cs.errCh:
		return "", "", err
	case <-ctx.Done():
		return "", "", ctx.Err()
	}
}

func (cs *CallbackServer) Shutdown(ctx context.Context) error {
	if cs.server != nil {
		return cs.server.Shutdown(ctx)
	}
	return nil
}
