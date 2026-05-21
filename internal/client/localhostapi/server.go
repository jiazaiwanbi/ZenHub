package localhostapi

import (
	"context"
	"errors"
	"io"
	"net/http"

	"zenhub/internal/core/balancer"
	"zenhub/internal/core/canonical"
	"zenhub/internal/core/executor"
	anthropicprotocol "zenhub/internal/core/protocol/anthropic"
	geminiprotocol "zenhub/internal/core/protocol/gemini"
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
		handleOpenAIChatCompletions(w, r, service)
	})
	mux.HandleFunc("/v1/messages", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			openaiprotocol.WriteError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		handleAnthropicMessages(w, r, service)
	})
	mux.HandleFunc("/v1beta/models/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			openaiprotocol.WriteError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		handleGeminiGenerateContent(w, r, service)
	})
	mux.HandleFunc("/v1/models/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			openaiprotocol.WriteError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		handleGeminiGenerateContent(w, r, service)
	})
	return mux
}

func handleOpenAIChatCompletions(w http.ResponseWriter, r *http.Request, service ChatService) {
	request, err := openaiprotocol.ParseChatCompletion(r.Body)
	if err != nil {
		writeMappedError(w, err)
		return
	}
	serveChat(w, r, service, request, openaiprotocol.PrepareStream, openaiprotocol.WriteStreamChunk, openaiprotocol.WriteChatResponse)
}

func handleAnthropicMessages(w http.ResponseWriter, r *http.Request, service ChatService) {
	request, err := anthropicprotocol.ParseMessages(r.Body)
	if err != nil {
		writeMappedError(w, err)
		return
	}
	serveChat(w, r, service, request, anthropicprotocol.PrepareStream, anthropicprotocol.WriteStreamChunk, anthropicprotocol.WriteResponse)
}

func handleGeminiGenerateContent(w http.ResponseWriter, r *http.Request, service ChatService) {
	request, err := geminiprotocol.ParseGenerateContent(r)
	if err != nil {
		writeMappedError(w, err)
		return
	}
	serveChat(w, r, service, request, geminiprotocol.PrepareStream, geminiprotocol.WriteStreamChunk, geminiprotocol.WriteResponse)
}

func serveChat(
	w http.ResponseWriter,
	r *http.Request,
	service ChatService,
	request canonical.ChatRequest,
	prepareStream func(http.ResponseWriter) (http.Flusher, bool),
	writeStream func(io.Writer, canonical.StreamChunk) error,
	writeResponse func(http.ResponseWriter, *canonical.ChatResponse) error,
) {
	if request.Stream {
		flusher, ok := prepareStream(w)
		if !ok {
			openaiprotocol.WriteError(w, http.StatusInternalServerError, "streaming is not supported by this server")
			return
		}

		headersWritten := false
		err := service.StreamChat(r.Context(), request, func(chunk canonical.StreamChunk) error {
			if !headersWritten {
				headersWritten = true
			}
			if err := writeStream(w, chunk); err != nil {
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
	if err := writeResponse(w, response); err != nil {
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
	case errors.Is(err, openaiprotocol.ErrInvalidRequest),
		errors.Is(err, anthropicprotocol.ErrInvalidRequest),
		errors.Is(err, geminiprotocol.ErrInvalidRequest):
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
