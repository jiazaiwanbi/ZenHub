package localhostapi

import (
	"context"
	"errors"
	"net/http"

	"zenhub/internal/core/balancer"
	"zenhub/internal/core/canonical"
	"zenhub/internal/core/executor"
	openaiprotocol "zenhub/internal/core/protocol/openai"
	"zenhub/internal/core/proxy"
	"zenhub/internal/core/router"
)

type ChatService interface {
	Models() []string
	ExecuteChat(context.Context, canonical.ChatRequest) (*canonical.ChatResponse, error)
	StreamChat(context.Context, canonical.ChatRequest, func(canonical.StreamChunk) error) error
}

func New(service ChatService) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/models", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			openaiprotocol.WriteError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		if err := openaiprotocol.WriteModels(w, service.Models()); err != nil {
			openaiprotocol.WriteError(w, http.StatusInternalServerError, err.Error())
		}
	})
	mux.HandleFunc("/v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			openaiprotocol.WriteError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		handleChatCompletions(w, r, service)
	})
	return mux
}

func handleChatCompletions(w http.ResponseWriter, r *http.Request, service ChatService) {
	request, err := openaiprotocol.ParseChatCompletion(r.Body)
	if err != nil {
		writeMappedError(w, err)
		return
	}

	if request.Stream {
		flusher, ok := w.(http.Flusher)
		if !ok {
			openaiprotocol.WriteError(w, http.StatusInternalServerError, "streaming is not supported by this server")
			return
		}

		headersWritten := false
		err = service.StreamChat(r.Context(), request, func(chunk canonical.StreamChunk) error {
			if !headersWritten {
				openaiprotocol.PrepareStream(w)
				headersWritten = true
			}
			if err := openaiprotocol.WriteStreamChunk(w, chunk); err != nil {
				return err
			}
			flusher.Flush()
			return nil
		})
		if err != nil && !headersWritten {
			writeMappedError(w, err)
		}
		return
	}

	response, err := service.ExecuteChat(r.Context(), request)
	if err != nil {
		writeMappedError(w, err)
		return
	}
	if err := openaiprotocol.WriteChatResponse(w, response); err != nil {
		writeMappedError(w, err)
	}
}

func writeMappedError(w http.ResponseWriter, err error) {
	if err == nil {
		return
	}

	var upstreamErr *executor.UpstreamError
	if errors.As(err, &upstreamErr) {
		if len(upstreamErr.Body) == 0 {
			openaiprotocol.WriteError(w, upstreamErr.StatusCode, upstreamErr.Error())
			return
		}
		contentType := upstreamErr.ContentType
		if contentType == "" {
			contentType = "application/json"
		}
		w.Header().Set("Content-Type", contentType)
		w.WriteHeader(upstreamErr.StatusCode)
		_, _ = w.Write(upstreamErr.Body)
		return
	}

	status := http.StatusBadGateway
	switch {
	case errors.Is(err, openaiprotocol.ErrInvalidRequest):
		status = http.StatusBadRequest
	case errors.Is(err, router.ErrNoRoute):
		status = http.StatusNotFound
	case errors.Is(err, executor.ErrRelayNotImplemented):
		status = http.StatusNotImplemented
	case errors.Is(err, balancer.ErrNoHealthyNodes):
		status = http.StatusServiceUnavailable
	case errors.Is(err, context.DeadlineExceeded):
		status = http.StatusGatewayTimeout
	}
	openaiprotocol.WriteError(w, status, err.Error())
}

func StatusForError(err error) int {
	return proxy.HTTPStatus(err)
}
