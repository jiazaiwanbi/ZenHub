package localhostapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAnthropicMessagesHandlerTranslatesViaOpenAIUpstream(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Fatalf("unexpected upstream path %q", r.URL.Path)
		}

		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode upstream payload: %v", err)
		}
		if got := payload["model"]; got != "upstream-model" {
			t.Fatalf("upstream model = %#v, want upstream-model", got)
		}
		if _, ok := payload["messages"].([]any); !ok {
			t.Fatalf("messages type = %T, want []any", payload["messages"])
		}

		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"id":"resp_1","model":"upstream-model","choices":[{"message":{"role":"assistant","content":"hello from openai"},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":4,"total_tokens":7}}`)
	}))
	defer upstream.Close()

	handler := buildHandler(t, upstream.URL)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{
		"model":"alias-model",
		"max_tokens":256,
		"messages":[{"role":"user","content":[{"type":"text","text":"hi"}]}]
	}`))

	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 body=%s", recorder.Code, recorder.Body.String())
	}
	body := recorder.Body.String()
	if !strings.Contains(body, `"type":"message"`) {
		t.Fatalf("expected anthropic message payload, got %s", body)
	}
	if !strings.Contains(body, `"text":"hello from openai"`) {
		t.Fatalf("expected translated assistant text, got %s", body)
	}
}

func TestGeminiGenerateContentHandlerTranslatesViaOpenAIUpstream(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Fatalf("unexpected upstream path %q", r.URL.Path)
		}

		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode upstream payload: %v", err)
		}
		if got := payload["model"]; got != "upstream-model" {
			t.Fatalf("upstream model = %#v, want upstream-model", got)
		}
		if _, ok := payload["messages"].([]any); !ok {
			t.Fatalf("messages type = %T, want []any", payload["messages"])
		}

		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"id":"resp_2","model":"upstream-model","choices":[{"message":{"role":"assistant","content":"hello from openai"},"finish_reason":"stop"}],"usage":{"prompt_tokens":5,"completion_tokens":6,"total_tokens":11}}`)
	}))
	defer upstream.Close()

	handler := buildHandler(t, upstream.URL)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/v1beta/models/alias-model:generateContent", strings.NewReader(`{
		"contents":[{"role":"user","parts":[{"text":"hi"}]}]
	}`))

	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 body=%s", recorder.Code, recorder.Body.String())
	}
	body := recorder.Body.String()
	if !strings.Contains(body, `"candidates"`) {
		t.Fatalf("expected gemini response payload, got %s", body)
	}
	if !strings.Contains(body, `"text":"hello from openai"`) {
		t.Fatalf("expected translated assistant text, got %s", body)
	}
}
