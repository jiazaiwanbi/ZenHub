package gemini

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"zenhub/internal/core/canonical"
)

const maxRequestBytes = 8 << 20

var ErrInvalidRequest = errors.New("invalid gemini generate content request")

func ParseGenerateContent(r *http.Request) (canonical.ChatRequest, error) {
	model, stream, err := modelAndStreamFromRequest(r)
	if err != nil {
		return canonical.ChatRequest{}, fmt.Errorf("%w: %v", ErrInvalidRequest, err)
	}

	rawBody, err := io.ReadAll(io.LimitReader(r.Body, maxRequestBytes+1))
	if err != nil {
		return canonical.ChatRequest{}, fmt.Errorf("%w: read body: %v", ErrInvalidRequest, err)
	}
	if len(rawBody) > maxRequestBytes {
		return canonical.ChatRequest{}, fmt.Errorf("%w: body exceeds %d bytes", ErrInvalidRequest, maxRequestBytes)
	}

	var fields map[string]json.RawMessage
	if len(rawBody) > 0 {
		if err := json.Unmarshal(rawBody, &fields); err != nil {
			return canonical.ChatRequest{}, fmt.Errorf("%w: malformed JSON: %v", ErrInvalidRequest, err)
		}
	}
	if fields == nil {
		fields = map[string]json.RawMessage{}
	}

	if model == "" {
		var envelope struct {
			Model string `json:"model"`
		}
		if len(rawBody) > 0 {
			if err := json.Unmarshal(rawBody, &envelope); err == nil {
				model = strings.TrimSpace(envelope.Model)
			}
		}
	}
	if model == "" {
		return canonical.ChatRequest{}, fmt.Errorf("%w: model is required", ErrInvalidRequest)
	}

	return canonical.ChatRequest{
		ID:            fmt.Sprintf("req_%d", time.Now().UnixNano()),
		Protocol:      canonical.ProtocolGeminiGenerateContent,
		Model:         model,
		RawBody:       cloneRaw(rawBody),
		Stream:        stream,
		RawExtensions: cloneRawMap(fields),
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

func modelAndStreamFromRequest(r *http.Request) (string, bool, error) {
	path := strings.TrimSpace(r.URL.Path)
	var prefix string
	switch {
	case strings.HasPrefix(path, "/v1beta/models/"):
		prefix = "/v1beta/models/"
	case strings.HasPrefix(path, "/v1/models/"):
		prefix = "/v1/models/"
	default:
		return "", false, errors.New("unsupported gemini path")
	}

	rest := strings.TrimPrefix(path, prefix)
	suffix := ""
	switch {
	case strings.HasSuffix(rest, ":generateContent"):
		suffix = ":generateContent"
	case strings.HasSuffix(rest, ":streamGenerateContent"):
		suffix = ":streamGenerateContent"
	default:
		return "", false, errors.New("unsupported gemini operation")
	}

	model := strings.TrimSpace(strings.TrimSuffix(rest, suffix))
	model = strings.TrimPrefix(model, "models/")
	if decoded, err := url.PathUnescape(model); err == nil {
		model = decoded
	}
	stream := strings.HasSuffix(suffix, ":streamGenerateContent") || strings.EqualFold(strings.TrimSpace(r.URL.Query().Get("alt")), "sse")
	return model, stream, nil
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
