package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"zenhub/internal/core/runtimeconfig"
	communityauth "zenhub/internal/server/community/auth"
	communityrelay "zenhub/internal/server/community/relay"
	memorystore "zenhub/internal/server/community/storage/memory"
	communitysync "zenhub/internal/server/community/sync"
)

func TestCommunityServerLoginSyncModelsAndRelay(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Fatalf("unexpected upstream path %q", r.URL.Path)
		}

		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode upstream payload: %v", err)
		}
		if payload["stream"] == true {
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprint(w, "data: {\"id\":\"stream_1\",\"model\":\"upstream-model\",\"choices\":[{\"delta\":{\"content\":\"hel\"},\"finish_reason\":\"\"}]}\n\n")
			fmt.Fprint(w, "data: {\"id\":\"stream_1\",\"model\":\"upstream-model\",\"choices\":[{\"delta\":{\"content\":\"lo\"},\"finish_reason\":\"stop\"}]}\n\n")
			fmt.Fprint(w, "data: [DONE]\n\n")
			return
		}

		if got := payload["model"]; got != "upstream-model" {
			t.Fatalf("payload model = %#v, want upstream-model", got)
		}

		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"id":"resp_1","model":"upstream-model","choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`)
	}))
	defer upstream.Close()

	store := memorystore.NewStore()
	authService, err := communityauth.New("admin", "secret-pass", "0123456789abcdef", time.Hour)
	if err != nil {
		t.Fatalf("auth.New() error = %v", err)
	}
	syncService := communitysync.NewService(store)
	relayService := communityrelay.NewService(store, &http.Client{})
	handler := New(authService, syncService, relayService)

	snapshot := runtimeconfig.Snapshot{
		Routes: []runtimeconfig.Route{
			{
				Model:         "relay-model",
				Mode:          "relay",
				ProviderGroup: "relay",
				UpstreamModel: "upstream-model",
			},
		},
		ProviderGroups: []runtimeconfig.ProviderGroup{
			{
				Name:            "relay",
				Strategy:        "round_robin",
				Timeout:         runtimeconfig.Duration{Duration: 2 * time.Second},
				RetryCount:      0,
				MaxNodeAttempts: 1,
				PassiveHealth: runtimeconfig.PassiveHealthConfig{
					FailureThreshold: 1,
					Cooldown:         runtimeconfig.Duration{Duration: time.Second},
				},
				Nodes: []runtimeconfig.Node{
					{Name: "primary", BaseURL: upstream.URL},
				},
			},
		},
	}
	hash, err := communitysync.HashSnapshot(snapshot)
	if err != nil {
		t.Fatalf("HashSnapshot() error = %v", err)
	}

	token := loginToken(t, handler)

	t.Run("unauthorized_models", func(t *testing.T) {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, "/api/v1/models", nil)

		handler.ServeHTTP(recorder, request)

		if recorder.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401", recorder.Code)
		}
	})

	t.Run("push_status_and_pull", func(t *testing.T) {
		pushRecorder := httptest.NewRecorder()
		pushRequest := jsonRequest(t, http.MethodPost, "/api/v1/sync/push", map[string]any{
			"local_hash":        hash,
			"local_modified_at": time.Now().UTC().UnixMilli(),
			"snapshot":          snapshot,
		}, token)

		handler.ServeHTTP(pushRecorder, pushRequest)

		if pushRecorder.Code != http.StatusOK {
			t.Fatalf("push status = %d body=%s", pushRecorder.Code, pushRecorder.Body.String())
		}

		var pushResponse communitysync.PushResponse
		if err := json.Unmarshal(pushRecorder.Body.Bytes(), &pushResponse); err != nil {
			t.Fatalf("decode push response: %v", err)
		}
		if pushResponse.Status != communitysync.StatusApplied {
			t.Fatalf("push response = %#v", pushResponse)
		}

		statusRecorder := httptest.NewRecorder()
		statusRequest := jsonRequest(t, http.MethodGet, "/api/v1/sync/status", nil, token)
		handler.ServeHTTP(statusRecorder, statusRequest)
		if statusRecorder.Code != http.StatusOK {
			t.Fatalf("status code = %d body=%s", statusRecorder.Code, statusRecorder.Body.String())
		}

		var statusResponse communitysync.Status
		if err := json.Unmarshal(statusRecorder.Body.Bytes(), &statusResponse); err != nil {
			t.Fatalf("decode status response: %v", err)
		}
		if !statusResponse.HasSnapshot || statusResponse.CloudHash != hash {
			t.Fatalf("status response = %#v", statusResponse)
		}

		pullRecorder := httptest.NewRecorder()
		pullRequest := jsonRequest(t, http.MethodPost, "/api/v1/sync/pull", map[string]any{
			"last_sync_at":      pushResponse.CloudUpdatedAt,
			"local_modified_at": pushResponse.CloudUpdatedAt,
			"local_hash":        hash,
		}, token)
		handler.ServeHTTP(pullRecorder, pullRequest)
		if pullRecorder.Code != http.StatusOK {
			t.Fatalf("pull status = %d body=%s", pullRecorder.Code, pullRecorder.Body.String())
		}

		var pullResponse communitysync.PullResponse
		if err := json.Unmarshal(pullRecorder.Body.Bytes(), &pullResponse); err != nil {
			t.Fatalf("decode pull response: %v", err)
		}
		if pullResponse.Status != communitysync.StatusUpToDate {
			t.Fatalf("pull response = %#v", pullResponse)
		}
	})

	t.Run("models_and_relay", func(t *testing.T) {
		modelRecorder := httptest.NewRecorder()
		modelRequest := jsonRequest(t, http.MethodGet, "/api/v1/models", nil, token)

		handler.ServeHTTP(modelRecorder, modelRequest)

		if modelRecorder.Code != http.StatusOK {
			t.Fatalf("models status = %d body=%s", modelRecorder.Code, modelRecorder.Body.String())
		}

		var modelsResponse struct {
			Data []struct {
				ID string `json:"id"`
			} `json:"data"`
		}
		if err := json.Unmarshal(modelRecorder.Body.Bytes(), &modelsResponse); err != nil {
			t.Fatalf("decode models response: %v", err)
		}
		if len(modelsResponse.Data) != 1 || modelsResponse.Data[0].ID != "relay-model" {
			t.Fatalf("models response = %#v", modelsResponse)
		}

		chatRecorder := httptest.NewRecorder()
		chatRequest := jsonRequest(t, http.MethodPost, "/api/v1/relay/chat/completions", map[string]any{
			"model": "relay-model",
			"messages": []map[string]any{
				{"role": "user", "content": "hello"},
			},
		}, token)

		handler.ServeHTTP(chatRecorder, chatRequest)

		if chatRecorder.Code != http.StatusOK {
			t.Fatalf("chat status = %d body=%s", chatRecorder.Code, chatRecorder.Body.String())
		}
		if got := strings.TrimSpace(chatRecorder.Body.String()); got != `{"id":"resp_1","model":"upstream-model","choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}` {
			t.Fatalf("chat body = %s", got)
		}
	})

	t.Run("streaming_relay", func(t *testing.T) {
		recorder := httptest.NewRecorder()
		request := jsonRequest(t, http.MethodPost, "/api/v1/relay/chat/completions", map[string]any{
			"model":  "relay-model",
			"stream": true,
			"messages": []map[string]any{
				{"role": "user", "content": "hello"},
			},
		}, token)

		handler.ServeHTTP(recorder, request)

		if recorder.Code != http.StatusOK {
			t.Fatalf("status = %d body=%s", recorder.Code, recorder.Body.String())
		}
		want := "" +
			"data: {\"id\":\"stream_1\",\"model\":\"upstream-model\",\"choices\":[{\"delta\":{\"content\":\"hel\"},\"finish_reason\":\"\"}]}\n\n" +
			"data: {\"id\":\"stream_1\",\"model\":\"upstream-model\",\"choices\":[{\"delta\":{\"content\":\"lo\"},\"finish_reason\":\"stop\"}]}\n\n" +
			"data: [DONE]\n\n"
		if recorder.Body.String() != want {
			t.Fatalf("stream body = %q, want %q", recorder.Body.String(), want)
		}
	})
}

func loginToken(t *testing.T, handler http.Handler) string {
	t.Helper()

	recorder := httptest.NewRecorder()
	request := jsonRequest(t, http.MethodPost, "/api/v1/auth/login", map[string]any{
		"username": "admin",
		"password": "secret-pass",
	}, "")

	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("login status = %d body=%s", recorder.Code, recorder.Body.String())
	}

	var response struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode login response: %v", err)
	}
	if response.AccessToken == "" {
		t.Fatal("access_token is empty")
	}
	return response.AccessToken
}

func jsonRequest(t *testing.T, method, path string, payload any, token string) *http.Request {
	t.Helper()

	var bodyReader *bytes.Reader
	if payload == nil {
		bodyReader = bytes.NewReader(nil)
	} else {
		raw, err := json.Marshal(payload)
		if err != nil {
			t.Fatalf("json.Marshal(payload) error = %v", err)
		}
		bodyReader = bytes.NewReader(raw)
	}

	request := httptest.NewRequest(method, path, bodyReader)
	if payload != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	return request
}
