package localhostapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"zenhub/internal/core/balancer"
	"zenhub/internal/core/executor"
	"zenhub/internal/core/proxy"
	"zenhub/internal/core/router"
)

func TestServerModelsAndChatHandlers(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Fatalf("unexpected upstream path %q", r.URL.Path)
		}

		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode upstream request: %v", err)
		}

		if payload["stream"] == true {
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprint(w, "data: {\"id\":\"stream_1\",\"model\":\"upstream-stream\",\"choices\":[{\"delta\":{\"content\":\"hel\"},\"finish_reason\":\"\"}]}\n\n")
			fmt.Fprint(w, "data: {\"id\":\"stream_1\",\"model\":\"upstream-stream\",\"choices\":[{\"delta\":{\"content\":\"lo\"},\"finish_reason\":\"stop\"}]}\n\n")
			fmt.Fprint(w, "data: [DONE]\n\n")
			return
		}

		if got := payload["model"]; got != "upstream-model" {
			t.Fatalf("upstream model = %#v, want upstream-model", got)
		}
		if got := payload["extra_flag"]; got != true {
			t.Fatalf("extra_flag = %#v, want true", got)
		}

		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"id":"resp_1","model":"upstream-model","choices":[{"message":{"role":"assistant","content":"hello"},"finish_reason":"stop"}]}`)
	}))
	defer upstream.Close()

	handler := buildHandler(t, upstream.URL)

	t.Run("models", func(t *testing.T) {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, "/v1/models", nil)

		handler.ServeHTTP(recorder, request)

		if recorder.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", recorder.Code)
		}

		var response struct {
			Data []struct {
				ID string `json:"id"`
			} `json:"data"`
		}
		if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		if len(response.Data) != 3 {
			t.Fatalf("models len = %d, want 3", len(response.Data))
		}
	})

	t.Run("non_streaming", func(t *testing.T) {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{
			"model":"alias-model",
			"messages":[{"role":"user","content":"hi"}],
			"extra_flag": true
		}`))

		handler.ServeHTTP(recorder, request)

		if recorder.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200 body=%s", recorder.Code, recorder.Body.String())
		}
		if got := strings.TrimSpace(recorder.Body.String()); got != `{"id":"resp_1","model":"upstream-model","choices":[{"message":{"role":"assistant","content":"hello"},"finish_reason":"stop"}]}` {
			t.Fatalf("body = %s", got)
		}
	})

	t.Run("streaming", func(t *testing.T) {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{
			"model":"stream-model",
			"stream": true,
			"messages":[{"role":"user","content":"hi"}]
		}`))

		handler.ServeHTTP(recorder, request)

		if recorder.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200 body=%s", recorder.Code, recorder.Body.String())
		}
		want := "" +
			"data: {\"id\":\"stream_1\",\"model\":\"upstream-stream\",\"choices\":[{\"delta\":{\"content\":\"hel\"},\"finish_reason\":\"\"}]}\n\n" +
			"data: {\"id\":\"stream_1\",\"model\":\"upstream-stream\",\"choices\":[{\"delta\":{\"content\":\"lo\"},\"finish_reason\":\"stop\"}]}\n\n" +
			"data: [DONE]\n\n"
		if recorder.Body.String() != want {
			t.Fatalf("stream body = %q, want %q", recorder.Body.String(), want)
		}
		if contentType := recorder.Header().Get("Content-Type"); contentType != "text/event-stream" {
			t.Fatalf("Content-Type = %q, want text/event-stream", contentType)
		}
	})

	t.Run("relay_not_implemented", func(t *testing.T) {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{
			"model":"relay-model",
			"messages":[{"role":"user","content":"hi"}]
		}`))

		handler.ServeHTTP(recorder, request)

		if recorder.Code != http.StatusNotImplemented {
			t.Fatalf("status = %d, want 501 body=%s", recorder.Code, recorder.Body.String())
		}
	})
}

func buildHandler(t *testing.T, upstreamURL string) http.Handler {
	t.Helper()

	routerInstance, err := router.New([]router.Rule{
		{Model: "alias-model", Mode: router.RouteModeDirect, ProviderGroup: "direct", UpstreamModel: "upstream-model"},
		{Model: "stream-model", Mode: router.RouteModeDirect, ProviderGroup: "direct", UpstreamModel: "upstream-stream"},
		{Model: "relay-model", Mode: router.RouteModeRelay, ProviderGroup: "relay"},
	})
	if err != nil {
		t.Fatalf("router.New() error = %v", err)
	}

	balancerInstance, err := balancer.New([]balancer.Group{
		{
			Name:            "direct",
			Strategy:        balancer.StrategyRoundRobin,
			Timeout:         2 * time.Second,
			RetryCount:      0,
			MaxNodeAttempts: 1,
			PassiveHealth: balancer.PassiveHealth{
				FailureThreshold: 1,
				Cooldown:         time.Second,
			},
			Nodes: []balancer.Node{
				{Name: "upstream", BaseURL: upstreamURL},
			},
		},
		{
			Name:            "relay",
			Strategy:        balancer.StrategyFillFirst,
			Timeout:         time.Second,
			RetryCount:      0,
			MaxNodeAttempts: 1,
			PassiveHealth: balancer.PassiveHealth{
				FailureThreshold: 1,
				Cooldown:         time.Second,
			},
			Nodes: []balancer.Node{
				{Name: "relay", BaseURL: "http://relay.invalid"},
			},
		},
	})
	if err != nil {
		t.Fatalf("balancer.New() error = %v", err)
	}

	service, err := proxy.New(routerInstance, balancerInstance, executor.NewDirect(&http.Client{}), nil)
	if err != nil {
		t.Fatalf("proxy.New() error = %v", err)
	}

	return New(service)
}
