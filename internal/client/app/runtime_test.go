package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRuntimeStatusRoutesAndRequests(t *testing.T) {
	upstream := startTestUpstream(t)

	configPath := writeTestConfig(t, map[string]any{
		"listen": "127.0.0.1:0",
		"observability": map[string]any{
			"max_records": 7,
		},
		"routes": []map[string]any{
			{
				"model":          "alias-model",
				"mode":           "direct",
				"provider_group": "direct",
				"upstream_model": "upstream-model",
			},
		},
		"provider_groups": []map[string]any{
			{
				"name":              "direct",
				"strategy":          "round_robin",
				"timeout":           "2s",
				"retry_count":       0,
				"max_node_attempts": 1,
				"passive_health": map[string]any{
					"failure_threshold": 1,
					"cooldown":          "1s",
				},
				"nodes": []map[string]any{
					{
						"name":     "primary",
						"base_url": upstream.URL,
					},
				},
			},
		},
	})

	runtime, err := NewRuntime(configPath)
	if err != nil {
		t.Fatalf("NewRuntime() error = %v", err)
	}
	if err := runtime.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	t.Cleanup(func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = runtime.Shutdown(shutdownCtx)
	})

	status := runtime.Status()
	if !status.Running {
		t.Fatal("Status().Running = false, want true")
	}
	if status.ConfigPath != configPath {
		t.Fatalf("Status().ConfigPath = %q, want %q", status.ConfigPath, configPath)
	}
	if status.ModelCount != 1 {
		t.Fatalf("Status().ModelCount = %d, want 1", status.ModelCount)
	}
	if status.ProviderGroupCount != 1 || status.NodeCount != 1 {
		t.Fatalf("group/node counts = %d/%d, want 1/1", status.ProviderGroupCount, status.NodeCount)
	}
	if status.ObservabilityLimit != 7 {
		t.Fatalf("Status().ObservabilityLimit = %d, want 7", status.ObservabilityLimit)
	}
	if !strings.HasPrefix(status.ListenAddress, "127.0.0.1:") {
		t.Fatalf("Status().ListenAddress = %q, want dynamic localhost address", status.ListenAddress)
	}

	routes := runtime.Routes()
	if len(routes) != 1 {
		t.Fatalf("Routes() len = %d, want 1", len(routes))
	}
	route := routes[0]
	if route.Model != "alias-model" || route.UpstreamModel != "upstream-model" {
		t.Fatalf("route = %#v", route)
	}
	if route.BalancingStrategy != "round_robin" || route.NodeCount != 1 {
		t.Fatalf("route group info = %#v", route)
	}

	body := strings.NewReader(`{"model":"alias-model","messages":[{"role":"user","content":"hello"}]}`)
	response, err := http.Post("http://"+status.ListenAddress+"/v1/chat/completions", "application/json", body)
	if err != nil {
		t.Fatalf("POST chat: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("POST chat status = %d, want 200", response.StatusCode)
	}

	requests := runtime.Requests()
	if len(requests) != 1 {
		t.Fatalf("Requests() len = %d, want 1", len(requests))
	}
	request := requests[0]
	if request.Model != "alias-model" || request.SelectedNode != "primary" {
		t.Fatalf("request = %#v", request)
	}
	if request.FinalStatus != "ok" || request.RouteMode != "direct" {
		t.Fatalf("request status = %#v", request)
	}

	status = runtime.Status()
	if !status.LastRequestOK {
		t.Fatal("Status().LastRequestOK = false, want true")
	}
}

func startTestUpstream(t *testing.T) *httptest.Server {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"resp_1","model":"upstream-model","choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`))
	}))
	t.Cleanup(server.Close)
	return server
}

func writeTestConfig(t *testing.T, content map[string]any) string {
	t.Helper()

	raw, err := json.Marshal(content)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatalf("os.WriteFile() error = %v", err)
	}
	return path
}
