package telemetry

import (
	"context"
)

// contextKey is a type for context keys to avoid collisions
type contextKey string

const (
	clientContextKey contextKey = "telemetry_client"
)

// WithClient adds a telemetry client to the context
func WithClient(ctx context.Context, client *Client) context.Context {
	return context.WithValue(ctx, clientContextKey, client)
}

// FromContext retrieves the telemetry client from context
func FromContext(ctx context.Context) *Client {
	if client, ok := ctx.Value(clientContextKey).(*Client); ok {
		return client
	}
	return nil
}
