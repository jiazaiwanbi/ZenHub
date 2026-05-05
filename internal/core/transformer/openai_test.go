package transformer

import (
	"encoding/json"
	"testing"

	"zenhub/internal/core/canonical"
)

func TestOpenAIChatRequestBuildsFromCanonicalFields(t *testing.T) {
	stream := false
	temperature := 0.7

	payload, err := OpenAIChatRequest(canonical.ChatRequest{
		Model: "local-model",
		Messages: []canonical.Message{
			{
				Role:      "user",
				Content:   json.RawMessage(`"new prompt"`),
				RawFields: map[string]json.RawMessage{"content": json.RawMessage(`"old prompt"`), "custom": json.RawMessage(`true`)},
			},
		},
		Temperature:   &temperature,
		Stream:        true,
		RawExtensions: map[string]json.RawMessage{"extra_flag": json.RawMessage(`true`)},
		RawFields: map[string]json.RawMessage{
			"model":       json.RawMessage(`"stale-model"`),
			"messages":    json.RawMessage(`[{"role":"user","content":"stale prompt"}]`),
			"stream":      json.RawMessage(`true`),
			"temperature": json.RawMessage(`0.1`),
			"metadata":    json.RawMessage(`{"team":"stale"}`),
		},
	}, "upstream-model", &stream)
	if err != nil {
		t.Fatalf("OpenAIChatRequest() error = %v", err)
	}

	var fields map[string]json.RawMessage
	if err := json.Unmarshal(payload, &fields); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}

	assertJSONFieldEquals(t, fields["model"], `"upstream-model"`)
	assertJSONFieldEquals(t, fields["messages"], `[{"content":"new prompt","custom":true,"role":"user"}]`)
	assertJSONFieldEquals(t, fields["temperature"], `0.7`)
	assertJSONFieldEquals(t, fields["stream"], `false`)
	assertJSONFieldEquals(t, fields["extra_flag"], `true`)
}

func TestOpenAIChatRequestSupportsCanonicalOnlyInput(t *testing.T) {
	payload, err := OpenAIChatRequest(canonical.ChatRequest{
		Model: "local-model",
		Messages: []canonical.Message{
			{
				Role:    "user",
				Content: json.RawMessage(`"hello"`),
			},
		},
	}, "", nil)
	if err != nil {
		t.Fatalf("OpenAIChatRequest() error = %v", err)
	}

	var fields map[string]json.RawMessage
	if err := json.Unmarshal(payload, &fields); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}

	assertJSONFieldEquals(t, fields["model"], `"local-model"`)
	assertJSONFieldEquals(t, fields["messages"], `[{"content":"hello","role":"user"}]`)
	assertJSONFieldEquals(t, fields["stream"], `false`)
}

func assertJSONFieldEquals(t *testing.T, got json.RawMessage, want string) {
	t.Helper()

	if string(got) != want {
		t.Fatalf("json field = %s, want %s", string(got), want)
	}
}
