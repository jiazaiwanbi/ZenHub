package openai

import (
	"strings"
	"testing"

	"zenhub/internal/canonical"
)

func TestParseChatCompletionCanonicalizesRequest(t *testing.T) {
	request, err := ParseChatCompletion(strings.NewReader(`{
		"model": "gpt-local",
		"messages": [
			{
				"role": "user",
				"content": "hello",
				"name": "tester"
			}
		],
		"temperature": 0.2,
		"stream": true,
		"metadata": {"team": "core"},
		"extra_field": {"debug": true}
	}`))
	if err != nil {
		t.Fatalf("ParseChatCompletion() error = %v", err)
	}

	if request.Protocol != canonical.ProtocolOpenAIChatCompletions {
		t.Fatalf("Protocol = %q, want %q", request.Protocol, canonical.ProtocolOpenAIChatCompletions)
	}
	if request.Model != "gpt-local" {
		t.Fatalf("Model = %q, want gpt-local", request.Model)
	}
	if !request.Stream {
		t.Fatal("Stream = false, want true")
	}
	if len(request.Messages) != 1 {
		t.Fatalf("Messages len = %d, want 1", len(request.Messages))
	}
	if request.Messages[0].Role != "user" {
		t.Fatalf("Message role = %q, want user", request.Messages[0].Role)
	}
	if request.Messages[0].Name != "tester" {
		t.Fatalf("Message name = %q, want tester", request.Messages[0].Name)
	}
	if _, ok := request.RawExtensions["extra_field"]; !ok {
		t.Fatal("RawExtensions missing extra_field")
	}
	if _, ok := request.RawFields["messages"]; !ok {
		t.Fatal("RawFields missing messages")
	}
}

func TestParseChatCompletionRejectsMissingModel(t *testing.T) {
	_, err := ParseChatCompletion(strings.NewReader(`{"messages":[{"role":"user","content":"hello"}]}`))
	if err == nil {
		t.Fatal("ParseChatCompletion() error = nil, want error")
	}
}
