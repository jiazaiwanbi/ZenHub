package openai

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"zenhub/internal/canonical"
)

const maxChatRequestBytes = 8 << 20

var ErrInvalidRequest = errors.New("invalid OpenAI chat completions request")

type chatRequestEnvelope struct {
	Model       string            `json:"model"`
	Messages    []json.RawMessage `json:"messages"`
	Tools       json.RawMessage   `json:"tools"`
	Temperature *float64          `json:"temperature"`
	TopP        *float64          `json:"top_p"`
	MaxTokens   *int              `json:"max_tokens"`
	Stream      bool              `json:"stream"`
	Metadata    map[string]any    `json:"metadata"`
}

type messageEnvelope struct {
	Role       string          `json:"role"`
	Content    json.RawMessage `json:"content"`
	Name       string          `json:"name"`
	ToolCallID string          `json:"tool_call_id"`
	ToolCalls  json.RawMessage `json:"tool_calls"`
}

func ParseChatCompletion(body io.Reader) (canonical.ChatRequest, error) {
	rawBody, err := io.ReadAll(io.LimitReader(body, maxChatRequestBytes+1))
	if err != nil {
		return canonical.ChatRequest{}, fmt.Errorf("%w: read body: %v", ErrInvalidRequest, err)
	}
	if len(rawBody) > maxChatRequestBytes {
		return canonical.ChatRequest{}, fmt.Errorf("%w: body exceeds %d bytes", ErrInvalidRequest, maxChatRequestBytes)
	}

	var fields map[string]json.RawMessage
	if err := json.Unmarshal(rawBody, &fields); err != nil {
		return canonical.ChatRequest{}, fmt.Errorf("%w: malformed JSON: %v", ErrInvalidRequest, err)
	}

	var envelope chatRequestEnvelope
	if err := json.Unmarshal(rawBody, &envelope); err != nil {
		return canonical.ChatRequest{}, fmt.Errorf("%w: malformed fields: %v", ErrInvalidRequest, err)
	}
	if strings.TrimSpace(envelope.Model) == "" {
		return canonical.ChatRequest{}, fmt.Errorf("%w: model is required", ErrInvalidRequest)
	}
	if len(envelope.Messages) == 0 {
		return canonical.ChatRequest{}, fmt.Errorf("%w: messages are required", ErrInvalidRequest)
	}

	messages := make([]canonical.Message, 0, len(envelope.Messages))
	for i, rawMessage := range envelope.Messages {
		message, err := parseMessage(rawMessage)
		if err != nil {
			return canonical.ChatRequest{}, fmt.Errorf("%w: message %d: %v", ErrInvalidRequest, i, err)
		}
		messages = append(messages, message)
	}

	return canonical.ChatRequest{
		ID:            fmt.Sprintf("req_%d", time.Now().UnixNano()),
		Protocol:      canonical.ProtocolOpenAIChatCompletions,
		Model:         envelope.Model,
		Messages:      messages,
		Tools:         cloneRaw(envelope.Tools),
		Temperature:   envelope.Temperature,
		TopP:          envelope.TopP,
		MaxTokens:     envelope.MaxTokens,
		Stream:        envelope.Stream,
		Metadata:      envelope.Metadata,
		RawExtensions: extensionFields(fields, knownChatRequestFields()),
		RawFields:     cloneRawMap(fields),
	}, nil
}

func WriteChatResponse(w http.ResponseWriter, response *canonical.ChatResponse) error {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, err := w.Write(response.Raw)
	return err
}

func WriteError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"error": map[string]any{
			"message": message,
			"type":    "zenhub_error",
		},
	})
}

func WriteModels(w http.ResponseWriter, modelIDs []string) error {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	return json.NewEncoder(w).Encode(ModelsPayload(modelIDs))
}

func ModelsPayload(modelIDs []string) map[string]any {
	models := append([]string(nil), modelIDs...)
	sort.Strings(models)

	data := make([]map[string]any, 0, len(models))
	for _, model := range models {
		data = append(data, map[string]any{
			"id":       model,
			"object":   "model",
			"created":  0,
			"owned_by": "zenhub",
		})
	}

	return map[string]any{
		"object": "list",
		"data":   data,
	}
}

func PrepareStream(w http.ResponseWriter) (http.Flusher, bool) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		return nil, false
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	return flusher, true
}

func WriteStreamChunk(w io.Writer, chunk canonical.StreamChunk) error {
	if chunk.Done {
		_, err := io.WriteString(w, "data: [DONE]\n\n")
		return err
	}
	data := bytes.TrimSpace(chunk.Data)
	if len(data) == 0 {
		return nil
	}
	_, err := fmt.Fprintf(w, "data: %s\n\n", data)
	return err
}

func parseMessage(raw json.RawMessage) (canonical.Message, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return canonical.Message{}, fmt.Errorf("malformed JSON: %v", err)
	}

	var envelope messageEnvelope
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return canonical.Message{}, fmt.Errorf("malformed fields: %v", err)
	}
	if strings.TrimSpace(envelope.Role) == "" {
		return canonical.Message{}, errors.New("role is required")
	}

	return canonical.Message{
		Role:       envelope.Role,
		Content:    cloneRaw(envelope.Content),
		Name:       envelope.Name,
		ToolCallID: envelope.ToolCallID,
		ToolCalls:  cloneRaw(envelope.ToolCalls),
		RawFields:  cloneRawMap(fields),
	}, nil
}

func extensionFields(fields map[string]json.RawMessage, known map[string]bool) map[string]json.RawMessage {
	extensions := make(map[string]json.RawMessage)
	for key, value := range fields {
		if known[key] {
			continue
		}
		extensions[key] = cloneRaw(value)
	}
	return extensions
}

func knownChatRequestFields() map[string]bool {
	return map[string]bool{
		"model":       true,
		"messages":    true,
		"tools":       true,
		"temperature": true,
		"top_p":       true,
		"max_tokens":  true,
		"stream":      true,
		"metadata":    true,
	}
}

func cloneRaw(raw json.RawMessage) json.RawMessage {
	if raw == nil {
		return nil
	}
	return append(json.RawMessage(nil), raw...)
}

func cloneRawMap(fields map[string]json.RawMessage) map[string]json.RawMessage {
	if fields == nil {
		return nil
	}
	copied := make(map[string]json.RawMessage, len(fields))
	for key, value := range fields {
		copied[key] = cloneRaw(value)
	}
	return copied
}
