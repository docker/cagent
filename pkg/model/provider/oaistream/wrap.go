package oaistream

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	openaisdk "github.com/openai/openai-go/v3"

	"github.com/docker/docker-agent/pkg/modelerrors"
)

// WrapOpenAIError preserves HTTP metadata and classifies provider failures privately.
// Non-OpenAI errors pass through unchanged; displayed errors retain the SDK's safe summary.
func WrapOpenAIError(err error) error {
	if err == nil {
		return nil
	}
	apiErr, ok := errors.AsType[*openaisdk.Error](err)
	if !ok || apiErr == nil || apiErr.StatusCode < 400 {
		return err
	}
	wrapped := modelerrors.WrapHTTPError(apiErr.StatusCode, apiErr.Response, err)
	message := openAIErrorMessage(apiErr)
	if kind := openAIOverflowKind(apiErr, message); kind != "" {
		return &modelerrors.ContextOverflowError{Underlying: wrapped, Kind: kind}
	}
	// Classify private text once; retain only the retry decision in the returned error.
	if statusErr, ok := errors.AsType[*modelerrors.StatusError](wrapped); ok {
		statusErr.Retryable, _, _ = modelerrors.ClassifyModelError(&modelerrors.StatusError{
			StatusCode: apiErr.StatusCode,
			Err:        errors.New(message),
		})
	}
	return wrapped
}

func openAIOverflowKind(apiErr *openaisdk.Error, message string) modelerrors.OverflowKind {
	// Match modelerrors' structured-signal precedence before inspecting prose.
	switch {
	case apiErr.Type == "request_too_large":
		return modelerrors.OverflowKindWire
	case apiErr.Code == "context_length_exceeded":
		return modelerrors.OverflowKindTokens
	case apiErr.StatusCode == http.StatusRequestEntityTooLarge:
		return modelerrors.OverflowKindWire
	}

	return modelerrors.OverflowKindOf(errors.New(message))
}

func openAIErrorMessage(apiErr *openaisdk.Error) string {
	messages := []string{apiErr.Message, apiErr.Type, apiErr.Code}
	for _, key := range []string{"error", "detail"} {
		if field, ok := apiErr.JSON.ExtraFields[key]; ok {
			var message string
			if json.Unmarshal([]byte(field.Raw()), &message) == nil {
				messages = append(messages, message)
			}
		}
	}
	// These are prose fields: avoid embedded-JSON parsing of unbounded diagnostics.
	// Never return or log this text, and do not truncate classification signals.
	return strings.ReplaceAll(strings.Join(messages, "\n"), "{", "(")
}
