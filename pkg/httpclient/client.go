package httpclient

import (
	"fmt"
	"maps"
	"net/http"
	"runtime"

	"github.com/docker/cagent/pkg/version"
)

type HTTPOptions struct {
	Header http.Header
}

type Opt func(*HTTPOptions)

func NewHTTPClient(opts ...Opt) *http.Client {
	httpOptions := HTTPOptions{
		Header: make(http.Header),
	}

	for _, opt := range opts {
		opt(&httpOptions)
	}

	// Enforce a consistent User-Agent header
	httpOptions.Header.Set("User-Agent", fmt.Sprintf("Cagent/%s (%s; %s)", version.Version, getNormalizedOS(), getNormalizedArchitecture()))

	return &http.Client{
		Transport: &userAgentTransport{
			httpOptions: httpOptions,
			rt:          http.DefaultTransport,
		},
	}
}

func WithHeader(key, value string) Opt {
	return func(o *HTTPOptions) {
		o.Header.Set(key, value)
	}
}

func WithProxiedBaseURL(value string) Opt {
	return func(o *HTTPOptions) {
		o.Header.Set("X-Cagent-Forward", value)

		// Enforce consistent headers (Anthropic client sets similar header already)
		o.Header.Set("X-Cagent-Lang", "go")
		o.Header.Set("X-Cagent-OS", getNormalizedOS())
		o.Header.Set("X-Cagent-Arch", getNormalizedArchitecture())
		o.Header.Set("X-Cagent-Runtime", "cagent")
		o.Header.Set("X-Cagent-Runtime-Version", version.Version)
	}
}

func WithProvider(provider string) Opt {
	return func(o *HTTPOptions) {
		o.Header.Set("X-Cagent-Provider", provider)
	}
}

func WithModel(model string) Opt {
	return func(o *HTTPOptions) {
		o.Header.Set("X-Cagent-Model", model)
	}
}

type userAgentTransport struct {
	httpOptions HTTPOptions
	rt          http.RoundTripper
}

func (u *userAgentTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	r2 := req.Clone(req.Context())
	maps.Copy(r2.Header, u.httpOptions.Header)
	return u.rt.RoundTrip(r2)
}

func getNormalizedOS() string {
	switch runtime.GOOS {
	case "ios":
		return "iOS"
	case "android":
		return "Android"
	case "darwin":
		return "MacOS"
	case "window":
		return "Windows"
	case "freebsd":
		return "FreeBSD"
	case "openbsd":
		return "OpenBSD"
	case "linux":
		return "Linux"
	default:
		return fmt.Sprintf("Other:%s", runtime.GOOS)
	}
}

func getNormalizedArchitecture() string {
	switch runtime.GOARCH {
	case "386":
		return "x32"
	case "amd64":
		return "x64"
	case "arm":
		return "arm"
	case "arm64":
		return "arm64"
	default:
		return fmt.Sprintf("other:%s", runtime.GOARCH)
	}
}
