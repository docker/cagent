// Package upstream provides utilities for propagating HTTP headers
// from incoming API requests to outbound toolset HTTP calls.
package upstream

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"github.com/dop251/goja"
)

type contextKey struct{}

// WithHeaders returns a new context carrying the given HTTP headers.
func WithHeaders(ctx context.Context, h http.Header) context.Context {
	return context.WithValue(ctx, contextKey{}, h)
}

// HeadersFromContext retrieves upstream HTTP headers from the context.
// Returns nil if no headers are present.
func HeadersFromContext(ctx context.Context) http.Header {
	h, _ := ctx.Value(contextKey{}).(http.Header)
	return h
}

// Handler wraps an http.Handler to store the incoming HTTP request
// headers in the request context for downstream toolset forwarding.
func Handler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := WithHeaders(r.Context(), r.Header.Clone())
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// NewHeaderTransport wraps an http.RoundTripper to set custom headers on
// every outbound request. Header values may contain ${headers.NAME}
// placeholders that are resolved at request time from upstream headers
// stored in the request context.
func NewHeaderTransport(base http.RoundTripper, headers map[string]string) http.RoundTripper {
	return &headerTransport{base: base, headers: headers}
}

type headerTransport struct {
	base    http.RoundTripper
	headers map[string]string
}

func (t *headerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	for key, value := range ResolveHeaders(req.Context(), t.headers) {
		req.Header.Set(key, value)
	}
	return t.base.RoundTrip(req)
}

// ResolveHeaders resolves ${headers.NAME} placeholders in header values
// using upstream headers from the context. Header names in the placeholder
// are case-insensitive, matching HTTP header convention.
//
// For example, given the config header:
//
//	Authorization: ${headers.Authorization}
//
// and an upstream request with "Authorization: Bearer token", the resolved
// value will be "Bearer token".
func ResolveHeaders(ctx context.Context, headers map[string]string) map[string]string {
	if len(headers) == 0 {
		return headers
	}

	upstream := HeadersFromContext(ctx)
	if upstream == nil {
		return headers
	}

	vm := goja.New()
	_ = vm.Set("headers", vm.NewDynamicObject(headerAccessor(func(name string) goja.Value {
		return vm.ToValue(upstream.Get(name))
	})))

	resolved := make(map[string]string, len(headers))
	for k, v := range headers {
		resolved[k] = expandTemplate(vm, v)
	}
	return resolved
}

// headerAccessor implements [goja.DynamicObject] for case-insensitive
// HTTP header lookups.
type headerAccessor func(string) goja.Value

func (h headerAccessor) Get(k string) goja.Value   { return h(k) }
func (headerAccessor) Set(string, goja.Value) bool { return false }
func (headerAccessor) Has(string) bool             { return true }
func (headerAccessor) Delete(string) bool          { return false }
func (headerAccessor) Keys() []string              { return nil }

// headerPlaceholderRe matches ${headers.NAME} and captures the header
// name so we can rewrite it to bracket notation for the JS runtime.
var headerPlaceholderRe = regexp.MustCompile(`\$\{\s*headers\.([^}]+)\}`)

// expandTemplate evaluates a string as a JavaScript template literal,
// resolving any ${...} expressions via the goja runtime.
// Before evaluation it rewrites ${headers.NAME} to ${headers["NAME"]}
// so that header names containing hyphens (e.g. X-Request-Id) are
// accessed correctly.
func expandTemplate(vm *goja.Runtime, text string) string {
	if !strings.Contains(text, "${") {
		return text
	}

	// Rewrite dotted header access to bracket notation so names with
	// hyphens work: ${headers.X-Req-Id} → ${headers["X-Req-Id"]}
	text = headerPlaceholderRe.ReplaceAllStringFunc(text, func(m string) string {
		parts := headerPlaceholderRe.FindStringSubmatch(m)
		name := strings.TrimSpace(parts[1])
		return `${headers["` + name + `"]}`
	})

	escaped := strings.ReplaceAll(text, "\\", "\\\\")
	escaped = strings.ReplaceAll(escaped, "`", "\\`")
	script := "`" + escaped + "`"

	v, err := vm.RunString(script)
	if err != nil {
		return text
	}
	if v == nil || v.Export() == nil {
		return ""
	}
	return fmt.Sprintf("%v", v.Export())
}
