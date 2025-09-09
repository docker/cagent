package openrouter

import (
	"testing"

	"github.com/docker/cagent/pkg/chat"
)

func TestConvertMessages_Basic(t *testing.T) {
	msgs := []chat.Message{{Role: chat.MessageRoleUser, Content: "hi"}}
	out := convertMessages(msgs)
	if len(out) != 1 {
		t.Fatalf("expected 1 message, got %d", len(out))
	}
	if out[0].Role != "user" || out[0].Content != "hi" {
		t.Fatalf("unexpected conversion: %+v", out[0])
	}
}

func TestConvertMultiContent_ImageURL(t *testing.T) {
	msgs := []chat.Message{{
		Role: chat.MessageRoleUser,
		MultiContent: []chat.MessagePart{
			{Type: chat.MessagePartTypeText, Text: "hello"},
			{Type: chat.MessagePartTypeImageURL, ImageURL: &chat.MessageImageURL{URL: "http://example.com/img.png", Detail: "high"}},
		},
	}} 
	out := convertMessages(msgs)
	if len(out) != 1 {
		t.Fatalf("expected 1, got %d", len(out))
	}
	if len(out[0].MultiContent) != 2 {
		t.Fatalf("expected 2 parts, got %d", len(out[0].MultiContent))
	}
}
