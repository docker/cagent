package telemetry

import (
	"context"
	"time"
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

func RecordError(ctx context.Context, err string) {
	if client := FromContext(ctx); client != nil {
		client.RecordError(ctx, err)
	}
}

func RecordToolCall(ctx context.Context, toolName, sessionID, agentName string, duration time.Duration, err error) {
	if client := FromContext(ctx); client != nil {
		client.RecordToolCall(ctx, toolName, sessionID, agentName, duration, err)
	}
}

func RecordSessionEnd(ctx context.Context) {
	if client := FromContext(ctx); client != nil {
		client.RecordSessionEnd(ctx)
	}
}

func RecordSessionStart(ctx context.Context, sessionID, agentName string) {
	if client := FromContext(ctx); client != nil {
		client.RecordSessionStart(ctx, sessionID, agentName)
	}
}

func RecordTokenUsage(ctx context.Context, model string, inputTokens, outputTokens int64, cost float64) {
	if client := FromContext(ctx); client != nil {
		client.RecordTokenUsage(ctx, model, inputTokens, outputTokens, cost)
	}
}
