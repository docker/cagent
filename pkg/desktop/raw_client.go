//go:build !no_docker_desktop

package desktop

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"time"
)

var ClientBackend = newRawClient(dialBackend)

type RawClient struct {
	client  func() *http.Client
	timeout time.Duration
}

func newRawClient(dialer func(ctx context.Context) (net.Conn, error)) *RawClient {
	return &RawClient{
		client: func() *http.Client {
			return &http.Client{
				Transport: &http.Transport{
					DialContext: func(ctx context.Context, _, _ string) (conn net.Conn, err error) {
						return dialer(ctx)
					},
				},
			}
		},
		timeout: 10 * time.Second,
	}
}

func (c *RawClient) Get(ctx context.Context, endpoint string, v any) error {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://localhost"+endpoint, http.NoBody)
	if err != nil {
		return err
	}

	response, err := c.client().Do(req)
	if err != nil {
		return err
	}
	defer response.Body.Close()

	buf, err := io.ReadAll(response.Body)
	if err != nil {
		return err
	}

	if err := json.Unmarshal(buf, &v); err != nil {
		return err
	}
	return nil
}
