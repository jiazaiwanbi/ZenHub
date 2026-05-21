package anthropic

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"zenhub/internal/core/canonical"
)

const maxRequestBytes = 8 << 20

var ErrInvalidRequest = errors.New("invalid anthropic messages request")

type messagesRequestEnvelope struct {
	Model       string            `json:"model"`
	Messages    []json.RawMessage `json:"messages"`
	System      json.RawMessage   `json:"system"`
	Tools       json.RawMessage   `json:"tools"`
	Temperature *float64          `json:"temperature"`
	TopP        *float64          `json:"top_p"`
	MaxTokens   *int              `json:"max_tokens"`
	Stream      bool              `json:"stream"`
	Metadata    map[string]any    `json:"metadata"`
}

type messageEnvelope struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

func ParseMessages(body io.Reader) (canonical.ChatRequest, error) {
	rawBody, err := io.ReadAll(io.LimitReader(body, maxRequestBytes+1))
	if err != nil {
		return canonical.ChatRequest{}, fmt.Errorf("%w: read body: %v", ErrInvalidRequest, err)
	}
	if len(rawBody) > maxRequestBytes {
		return canonical.ChatRequest{}, fmt.Errorf("%w: body exceeds %d bytes", ErrInvalidRequest, maxRequestBytes)
	}

	var fields map[string]json.RawMessage
	if err := json.Unmarshal(rawBody, &fields); err != nil {
		return canonical.ChatRequest{}, fmt.Errorf("%w: malformed JSON: %v", ErrInvalidRequest, err)
	}

	var envelope messagesRequestEnvelope
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
		Protocol:      canonical.ProtocolAnthropicMessages,
		Model:         envelope.Model,
		RawBody:       cloneRaw(rawBody),
		Messages:      messages,
		Tools:         cloneRaw(envelope.Tools),
		Temperature:   envelope.Temperature,
		TopP:          envelope.TopP,
		MaxTokens:     envelope.MaxTokens,
		Stream:        envelope.Stream,
		Metadata:      envelope.Metadata,
		RawExtensions: extensionFields(fields, knownRequestFields()),
		RawFields:     cloneRawMap(fields),
	}, nil
}

func WriteResponse(w http.ResponseWriter, response *canonical.ChatResponse) error {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, err := w.Write(response.Raw)
	return err
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
	if len(chunk.Data) == 0 {
		return nil
	}
	_, err := w.Write(chunk.Data)
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
		Role:      envelope.Role,
		Content:   cloneRaw(envelope.Content),
		RawFields: cloneRawMap(fields),
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

func knownRequestFields() map[string]bool {
	return map[string]bool{
		"model":       true,
		"messages":    true,
		"system":      true,
		"tools":       true,
		"temperature": true,
		"top_p":       true,
		"max_tokens":  true,
		"stream":      true,
		"metadata":    true,
	}
}

func cloneRaw(raw []byte) json.RawMessage {
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
