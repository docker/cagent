package oauth

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync/atomic"
	"time"
)

// CallbackServer handles OAuth callback redirects for CLI/TUI mode
type CallbackServer struct {
	server     *http.Server
	port       int
	callbackCh chan CallbackResult
	running    atomic.Bool
}

// CallbackResult contains the result of an OAuth callback
type CallbackResult struct {
	Code  string
	State string
	Error string
}

// NewCallbackServer creates a new OAuth callback server
func NewCallbackServer(port int) *CallbackServer {
	return &CallbackServer{
		port:       port,
		callbackCh: make(chan CallbackResult, 1),
	}
}

// Start starts the OAuth callback server
func (s *CallbackServer) Start(ctx context.Context) error {
	if alreadyRunning := s.running.Swap(true); alreadyRunning {
		return nil
	}

	var lc net.ListenConfig
	listener, err := lc.Listen(ctx, "tcp", fmt.Sprintf("localhost:%d", s.port))
	if err != nil {
		return fmt.Errorf("failed to listen on port %d: %w", s.port, err)
	}
	go func() {
		<-ctx.Done()
		listener.Close()
	}()

	mux := http.NewServeMux()
	mux.HandleFunc("/oauth-callback", s.handleCallback)

	s.server = &http.Server{
		Handler:      mux,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
	}

	slog.Debug("Starting OAuth callback server", "port", s.port)

	go func() {
		if err := s.server.Serve(listener); err != nil && err != http.ErrServerClosed {
			slog.Error("OAuth callback server error", "error", err)
		}
	}()

	return nil
}

// Stop stops the OAuth callback server
func (s *CallbackServer) Stop(ctx context.Context) error {
	if wasRunning := s.running.Swap(false); !wasRunning {
		return nil
	}

	if s.server == nil {
		return nil
	}

	slog.Debug("Stopping OAuth callback server", "port", s.port)

	shutdownCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	if err := s.server.Shutdown(shutdownCtx); err != nil {
		slog.Warn("Failed to gracefully shutdown OAuth callback server", "error", err)
		return s.server.Close()
	}

	return nil
}

// GetRedirectURI returns the redirect URI for this callback server
func (s *CallbackServer) GetRedirectURI() string {
	return fmt.Sprintf("http://localhost:%d/oauth-callback", s.port)
}

// WaitForCallback waits for an OAuth callback with timeout
func (s *CallbackServer) WaitForCallback(ctx context.Context) (CallbackResult, error) {
	select {
	case result := <-s.callbackCh:
		return result, nil
	case <-ctx.Done():
		return CallbackResult{}, ctx.Err()
	}
}

// handleCallback handles the OAuth callback request
func (s *CallbackServer) handleCallback(w http.ResponseWriter, r *http.Request) {
	slog.Debug("Received OAuth callback", "url", r.URL.String())

	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	query := r.URL.Query()
	code := query.Get("code")
	state := query.Get("state")
	errorParam := query.Get("error")

	result := CallbackResult{
		Code:  code,
		State: state,
		Error: errorParam,
	}

	// Send result to waiting channel (non-blocking)
	select {
	case s.callbackCh <- result:
		slog.Debug("OAuth callback result sent", "code_present", code != "", "state", state, "error", errorParam)
	default:
		slog.Warn("OAuth callback channel full, dropping result")
	}

	// Send success response to browser
	w.Header().Set("Content-Type", "text/html")
	w.WriteHeader(http.StatusOK)

	if errorParam != "" {
		_, _ = fmt.Fprintf(w, `
<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Authorization Error</title>
    <style>
        * {
            margin: 0;
            padding: 0;
            box-sizing: border-box;
        }

        body {
            font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, 'Helvetica Neue', Arial, sans-serif;
            background: linear-gradient(135deg, #667eea 0%%, #764ba2 100%%);
            min-height: 100vh;
            display: flex;
            align-items: center;
            justify-content: center;
            padding: 20px;
        }

        .container {
            background: white;
            border-radius: 16px;
            box-shadow: 0 20px 40px rgba(0,0,0,0.1);
            padding: 48px 40px;
            text-align: center;
            max-width: 480px;
            width: 100%%;
        }

        .error-icon {
            width: 80px;
            height: 80px;
            background: #fee2e2;
            border-radius: 50%%;
            display: flex;
            align-items: center;
            justify-content: center;
            margin: 0 auto 24px;
            font-size: 32px;
        }

        h1 {
            font-size: 28px;
            font-weight: 600;
            color: #1f2937;
            margin-bottom: 16px;
        }

        p {
            font-size: 16px;
            color: #6b7280;
            line-height: 1.5;
            margin-bottom: 12px;
        }

        .error-detail {
            background: #fef2f2;
            border: 1px solid #fecaca;
            border-radius: 8px;
            padding: 12px 16px;
            color: #dc2626;
            font-family: 'Monaco', 'Menlo', 'Ubuntu Mono', monospace;
            font-size: 14px;
            margin: 20px 0;
        }
    </style>
</head>
<body>
    <div class="container">
        <div class="error-icon">❌</div>
        <h1>Authorization Failed</h1>
        <p>The authorization process could not be completed.</p>
        <div class="error-detail">%s</div>
        <p>You can close this window and return to cagent.</p>
    </div>
</body>
</html>`, html.EscapeString(errorParam))
	} else {
		_, _ = fmt.Fprint(w, `
<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Authorization Successful</title>
    <style>
        * {
            margin: 0;
            padding: 0;
            box-sizing: border-box;
        }

        body {
            font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, 'Helvetica Neue', Arial, sans-serif;
            background: linear-gradient(135deg, #667eea 0%, #764ba2 100%);
            min-height: 100vh;
            display: flex;
            align-items: center;
            justify-content: center;
            padding: 20px;
            animation: fadeIn 0.6s ease-out;
        }

        @keyframes fadeIn {
            from { opacity: 0; transform: translateY(20px); }
            to { opacity: 1; transform: translateY(0); }
        }

        .container {
            background: white;
            border-radius: 16px;
            box-shadow: 0 20px 40px rgba(0,0,0,0.1);
            padding: 48px 40px;
            text-align: center;
            max-width: 480px;
            width: 100%;
            animation: slideUp 0.8s ease-out;
        }

        @keyframes slideUp {
            from { opacity: 0; transform: translateY(30px); }
            to { opacity: 1; transform: translateY(0); }
        }

        .success-icon {
            width: 80px;
            height: 80px;
            background: linear-gradient(135deg, #10b981, #059669);
            border-radius: 50%;
            display: flex;
            align-items: center;
            justify-content: center;
            margin: 0 auto 24px;
            font-size: 32px;
            color: white;
            animation: pulse 2s infinite;
        }

        @keyframes pulse {
            0%, 100% { transform: scale(1); }
            50% { transform: scale(1.05); }
        }

        h1 {
            font-size: 28px;
            font-weight: 600;
            color: #1f2937;
            margin-bottom: 16px;
            letter-spacing: -0.025em;
        }

        p {
            font-size: 16px;
            color: #6b7280;
            line-height: 1.6;
            margin-bottom: 32px;
        }

        .terminal-hint {
            background: #f3f4f6;
            border: 2px dashed #d1d5db;
            border-radius: 12px;
            padding: 20px;
            margin: 24px 0;
        }

        .terminal-hint .icon {
            font-size: 24px;
            margin-bottom: 8px;
        }

        .terminal-hint .text {
            font-size: 14px;
            color: #4b5563;
            font-weight: 500;
        }

        .auto-close {
            font-size: 14px;
            color: #9ca3af;
            margin-top: 24px;
            font-style: italic;
        }

        .loading-bar {
            width: 100%;
            height: 4px;
            background: #e5e7eb;
            border-radius: 2px;
            margin-top: 16px;
            overflow: hidden;
        }

        .loading-progress {
            height: 100%;
            background: linear-gradient(90deg, #10b981, #059669);
            border-radius: 2px;
            animation: loading 3s linear;
        }

        @keyframes loading {
            from { width: 0%; }
            to { width: 100%; }
        }
    </style>
</head>
<body>
    <div class="container">
        <div class="success-icon">✓</div>
        <h1>Authorization Successful</h1>
        <p>You can close this window and return to cagent</p>

        <div class="terminal-hint">
            <div class="icon">💻</div>
            <div class="text">Return to your terminal to continue</div>
        </div>

        <div class="auto-close">
            This window will close automatically in a few seconds
            <div class="loading-bar">
                <div class="loading-progress"></div>
            </div>
        </div>
    </div>

    <script>
        setTimeout(function() {
            window.close();
        }, 3000);

        // Add a subtle animation when the page loads
        document.addEventListener('DOMContentLoaded', function() {
            const container = document.querySelector('.container');
            container.style.transform = 'translateY(0)';
        });
    </script>
</body>
</html>`)
	}
}

// html is a simple HTML escaping utility
var html = struct {
	EscapeString func(s string) string
}{
	EscapeString: func(s string) string {
		s = strings.ReplaceAll(s, "&", "&amp;")
		s = strings.ReplaceAll(s, "<", "&lt;")
		s = strings.ReplaceAll(s, ">", "&gt;")
		s = strings.ReplaceAll(s, `"`, "&#34;")
		s = strings.ReplaceAll(s, "'", "&#39;")
		return s
	},
}

// Global callback server for CLI/TUI mode
var globalCallbackServer atomic.Pointer[CallbackServer]

// SetGlobalCallbackServer sets the global callback server instance
func SetGlobalCallbackServer(server *CallbackServer) {
	globalCallbackServer.Store(server)
}

// GetGlobalCallbackServer returns the global callback server instance
func GetGlobalCallbackServer() *CallbackServer {
	return globalCallbackServer.Load()
}
