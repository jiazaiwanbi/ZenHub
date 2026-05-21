package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"zenhub/internal/core/balancer"
	"zenhub/internal/core/canonical"
	"zenhub/internal/core/executor"
	anthropicprotocol "zenhub/internal/core/protocol/anthropic"
	geminiprotocol "zenhub/internal/core/protocol/gemini"
	openaiprotocol "zenhub/internal/core/protocol/openai"
	"zenhub/internal/core/router"
	communityauth "zenhub/internal/server/community/auth"
	communityrelay "zenhub/internal/server/community/relay"
	communitystorage "zenhub/internal/server/community/storage"
	communitysync "zenhub/internal/server/community/sync"
)

type authService interface {
	Login(string, string) (communityauth.Session, error)
	Verify(string) (communityauth.Claims, error)
}

type syncService interface {
	Status(context.Context) (communitysync.Status, error)
	Pull(context.Context, communitysync.PullRequest) (communitysync.PullResponse, error)
	Push(context.Context, communitysync.PushRequest) (communitysync.PushResponse, error)
}

type relayService interface {
	Models(context.Context) ([]string, error)
	ProviderCatalog(context.Context) (communityrelay.ProviderCatalog, error)
	ExecuteChat(context.Context, canonical.ChatRequest) (*canonical.ChatResponse, error)
	StreamChat(context.Context, canonical.ChatRequest, func(canonical.StreamChunk) error) error
}

func New(
	auth authService,
	sync syncService,
	relay relayService,
) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("/api/v1/auth/login", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			openaiprotocol.WriteError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		handleLogin(w, r, auth)
	})

	mux.Handle("/api/v1/sync/status", withAuth(auth, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			openaiprotocol.WriteError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		status, err := sync.Status(r.Context())
		if err != nil {
			writeMappedError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, status)
	})))

	mux.Handle("/api/v1/sync/pull", withAuth(auth, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			openaiprotocol.WriteError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}

		var request communitysync.PullRequest
		if err := decodeJSON(r, &request); err != nil {
			openaiprotocol.WriteError(w, http.StatusBadRequest, err.Error())
			return
		}

		response, err := sync.Pull(r.Context(), request)
		if err != nil {
			writeMappedError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, response)
	})))

	mux.Handle("/api/v1/sync/push", withAuth(auth, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			openaiprotocol.WriteError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}

		var request communitysync.PushRequest
		if err := decodeJSON(r, &request); err != nil {
			openaiprotocol.WriteError(w, http.StatusBadRequest, err.Error())
			return
		}

		response, err := sync.Push(r.Context(), request)
		if err != nil {
			writeMappedError(w, err)
			return
		}

		statusCode := http.StatusOK
		if response.Status == communitysync.StatusConflict {
			statusCode = http.StatusConflict
		}
		writeJSON(w, statusCode, response)
	})))

	mux.Handle("/api/v1/models", withAuth(auth, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			openaiprotocol.WriteError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}

		models, err := relay.Models(r.Context())
		if err != nil {
			writeMappedError(w, err)
			return
		}
		if err := openaiprotocol.WriteModels(w, models); err != nil {
			openaiprotocol.WriteError(w, http.StatusInternalServerError, err.Error())
		}
	})))

	mux.Handle("/api/v1/catalog/providers", withAuth(auth, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			openaiprotocol.WriteError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}

		catalog, err := relay.ProviderCatalog(r.Context())
		if err != nil {
			writeMappedError(w, err)
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"version":          catalog.Version,
			"cloud_updated_at": catalog.CloudUpdatedAt,
			"cloud_hash":       catalog.CloudHash,
			"provider_groups":  catalog.ProviderGroups,
		})
	})))

	mux.Handle("/api/v1/relay/chat/completions", withAuth(auth, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			openaiprotocol.WriteError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		handleRelayOpenAIChatCompletions(w, r, relay)
	})))

	mux.Handle("/api/v1/relay/messages", withAuth(auth, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			openaiprotocol.WriteError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		handleRelayAnthropicMessages(w, r, relay)
	})))

	mux.Handle("/api/v1/relay/v1beta/models/", withAuth(auth, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			openaiprotocol.WriteError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		handleRelayGeminiGenerateContent(w, r, relay)
	})))

	mux.Handle("/api/v1/relay/v1/models/", withAuth(auth, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			openaiprotocol.WriteError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		handleRelayGeminiGenerateContent(w, r, relay)
	})))

	return mux
}

func handleLogin(w http.ResponseWriter, r *http.Request, auth authService) {
	var request struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := decodeJSON(r, &request); err != nil {
		openaiprotocol.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}

	session, err := auth.Login(request.Username, request.Password)
	if err != nil {
		writeMappedError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, session)
}

func handleRelayOpenAIChatCompletions(w http.ResponseWriter, r *http.Request, relay relayService) {
	request, err := openaiprotocol.ParseChatCompletion(r.Body)
	if err != nil {
		writeMappedError(w, err)
		return
	}
	serveRelayChat(w, r, relay, request, openaiprotocol.PrepareStream, openaiprotocol.WriteStreamChunk, openaiprotocol.WriteChatResponse)
}

func handleRelayAnthropicMessages(w http.ResponseWriter, r *http.Request, relay relayService) {
	request, err := anthropicprotocol.ParseMessages(r.Body)
	if err != nil {
		writeMappedError(w, err)
		return
	}
	serveRelayChat(w, r, relay, request, anthropicprotocol.PrepareStream, anthropicprotocol.WriteStreamChunk, anthropicprotocol.WriteResponse)
}

func handleRelayGeminiGenerateContent(w http.ResponseWriter, r *http.Request, relay relayService) {
	request, err := geminiprotocol.ParseGenerateContent(r)
	if err != nil {
		writeMappedError(w, err)
		return
	}
	serveRelayChat(w, r, relay, request, geminiprotocol.PrepareStream, geminiprotocol.WriteStreamChunk, geminiprotocol.WriteResponse)
}

func serveRelayChat(
	w http.ResponseWriter,
	r *http.Request,
	relay relayService,
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
		err := relay.StreamChat(r.Context(), request, func(chunk canonical.StreamChunk) error {
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

	response, err := relay.ExecuteChat(r.Context(), request)
	if err != nil {
		writeMappedError(w, err)
		return
	}
	if err := writeResponse(w, response); err != nil {
		writeMappedError(w, err)
	}
}

func withAuth(auth authService, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		header := strings.TrimSpace(r.Header.Get("Authorization"))
		if !strings.HasPrefix(strings.ToLower(header), "bearer ") {
			writeUnauthorized(w, communityauth.ErrInvalidToken)
			return
		}

		token := strings.TrimSpace(header[len("Bearer "):])
		if _, err := auth.Verify(token); err != nil {
			writeUnauthorized(w, err)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func writeUnauthorized(w http.ResponseWriter, err error) {
	w.Header().Set("WWW-Authenticate", `Bearer realm="zenhub-community"`)
	openaiprotocol.WriteError(w, http.StatusUnauthorized, err.Error())
}

func decodeJSON(r *http.Request, target any) error {
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
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
	case errors.Is(err, communityauth.ErrInvalidCredentials):
		status = http.StatusUnauthorized
	case errors.Is(err, communityauth.ErrInvalidToken), errors.Is(err, communityauth.ErrExpiredToken):
		status = http.StatusUnauthorized
	case errors.Is(err, router.ErrNoRoute):
		status = http.StatusNotFound
	case errors.Is(err, balancer.ErrNoHealthyNodes):
		status = http.StatusServiceUnavailable
	case errors.Is(err, context.DeadlineExceeded):
		status = http.StatusGatewayTimeout
	case errors.Is(err, communitystorage.ErrSnapshotNotFound):
		status = http.StatusServiceUnavailable
		err = errors.New("community server snapshot is not initialized")
	}

	openaiprotocol.WriteError(w, status, err.Error())
}

var _ relayService = (*communityrelay.Service)(nil)
