package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	clientconfig "zenhub/internal/client/config"
	clientsync "zenhub/internal/client/sync"
	"zenhub/internal/core/runtimeconfig"
	communityapi "zenhub/internal/server/community/api"
	communityauth "zenhub/internal/server/community/auth"
	communitycontrolgrpc "zenhub/internal/server/community/controlgrpc"
	communityrelay "zenhub/internal/server/community/relay"
	memorystore "zenhub/internal/server/community/storage/memory"
	communitysync "zenhub/internal/server/community/sync"
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

func TestRuntimeStartsWithLocalConfigWhenStartupSyncFails(t *testing.T) {
	upstream := startTestUpstream(t)

	configPath := writeTestConfig(t, map[string]any{
		"listen": "127.0.0.1:0",
		"sync": map[string]any{
			"enabled":    true,
			"server_url": "http://127.0.0.1:1",
			"username":   "admin",
			"password":   "secret-pass",
		},
		"routes": []map[string]any{
			{
				"model":          "fallback-model",
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
		t.Fatalf("NewRuntime() with startup sync failure error = %v", err)
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
	if !status.SyncEnabled {
		t.Fatal("Status().SyncEnabled = false, want true")
	}
	if status.LastSyncStatus != clientsync.StatusError {
		t.Fatalf("Status().LastSyncStatus = %q, want error", status.LastSyncStatus)
	}
	if strings.TrimSpace(status.SyncError) == "" {
		t.Fatal("Status().SyncError is empty, want startup sync failure details")
	}

	response, err := http.Post("http://"+status.ListenAddress+"/v1/chat/completions", "application/json", strings.NewReader(`{"model":"fallback-model","messages":[{"role":"user","content":"hello"}]}`))
	if err != nil {
		t.Fatalf("POST chat during startup sync failure fallback: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("POST chat status = %d, want 200", response.StatusCode)
	}
}

func TestRuntimeQuarantinesCorruptedSyncStateAndStarts(t *testing.T) {
	upstream := startTestUpstream(t)

	configPath := writeTestConfig(t, map[string]any{
		"listen": "127.0.0.1:0",
		"sync": map[string]any{
			"enabled":    true,
			"server_url": "http://127.0.0.1:8081",
			"username":   "admin",
			"password":   "secret-pass",
		},
		"routes": []map[string]any{
			{
				"model":          "state-recovery-model",
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

	statePath := clientconfig.SyncStatePath(configPath)
	if err := os.WriteFile(statePath, []byte("{this is not json"), 0o600); err != nil {
		t.Fatalf("os.WriteFile(corrupted sync state) error = %v", err)
	}

	runtime, err := NewRuntime(configPath)
	if err != nil {
		t.Fatalf("NewRuntime() with corrupted sync state error = %v", err)
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
	if !status.SyncEnabled {
		t.Fatal("Status().SyncEnabled = false, want true")
	}
	if status.LastSyncStatus != clientsync.StatusError {
		t.Fatalf("Status().LastSyncStatus = %q, want error", status.LastSyncStatus)
	}
	if !strings.Contains(status.SyncError, "moved corrupted state") {
		t.Fatalf("Status().SyncError = %q, want corrupted-state recovery detail", status.SyncError)
	}

	if _, err := os.Stat(statePath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("os.Stat(original sync state) error = %v, want not exist", err)
	}
	matches, err := filepath.Glob(statePath + ".corrupt-*")
	if err != nil {
		t.Fatalf("filepath.Glob() error = %v", err)
	}
	if len(matches) != 1 {
		t.Fatalf("corrupted sync state backups = %#v, want 1 file", matches)
	}

	response, err := http.Post("http://"+status.ListenAddress+"/v1/chat/completions", "application/json", strings.NewReader(`{"model":"state-recovery-model","messages":[{"role":"user","content":"hello"}]}`))
	if err != nil {
		t.Fatalf("POST chat during corrupted state recovery: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("POST chat status = %d, want 200", response.StatusCode)
	}
}

func TestRuntimeRelayRequests(t *testing.T) {
	relay := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/relay/chat/completions" {
			t.Fatalf("unexpected relay path %q", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer relay-token" {
			t.Fatalf("Authorization = %q, want Bearer relay-token", got)
		}

		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode relay payload: %v", err)
		}
		if got := payload["model"]; got != "relay-model" {
			t.Fatalf("payload model = %#v, want relay-model", got)
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"resp_relay","model":"relay-model","choices":[{"message":{"role":"assistant","content":"relay ok"},"finish_reason":"stop"}]}`))
	}))
	t.Cleanup(relay.Close)

	configPath := writeTestConfig(t, map[string]any{
		"listen": "127.0.0.1:0",
		"routes": []map[string]any{
			{
				"model":          "relay-model",
				"mode":           "relay",
				"provider_group": "relay",
			},
		},
		"provider_groups": []map[string]any{
			{
				"name":              "relay",
				"strategy":          "fill_first",
				"timeout":           "2s",
				"retry_count":       0,
				"max_node_attempts": 1,
				"passive_health": map[string]any{
					"failure_threshold": 1,
					"cooldown":          "1s",
				},
				"nodes": []map[string]any{
					{
						"name":     "relay-node",
						"base_url": relay.URL,
						"api_key":  "relay-token",
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
	body := strings.NewReader(`{"model":"relay-model","messages":[{"role":"user","content":"hello"}]}`)
	response, err := http.Post("http://"+status.ListenAddress+"/v1/chat/completions", "application/json", body)
	if err != nil {
		t.Fatalf("POST relay chat: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("POST relay chat status = %d, want 200", response.StatusCode)
	}

	requests := runtime.Requests()
	if len(requests) != 1 {
		t.Fatalf("Requests() len = %d, want 1", len(requests))
	}
	request := requests[0]
	if request.RouteMode != "relay" || request.SelectedNode != "relay-node" {
		t.Fatalf("request = %#v", request)
	}
	if request.FinalStatus != "ok" {
		t.Fatalf("request status = %#v", request)
	}
}

func TestRuntimeApplyConfigEditsReloadsProxy(t *testing.T) {
	upstreamA := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"resp_a","model":"upstream-a","choices":[{"message":{"role":"assistant","content":"from-a"},"finish_reason":"stop"}]}`))
	}))
	t.Cleanup(upstreamA.Close)

	upstreamB := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"resp_b","model":"upstream-b","choices":[{"message":{"role":"assistant","content":"from-b"},"finish_reason":"stop"}]}`))
	}))
	t.Cleanup(upstreamB.Close)

	configPath := writeTestConfig(t, map[string]any{
		"listen": "127.0.0.1:0",
		"routes": []map[string]any{
			{
				"model":          "initial-model",
				"mode":           "direct",
				"provider_group": "direct-a",
				"upstream_model": "upstream-a",
			},
		},
		"provider_groups": []map[string]any{
			{
				"name":              "direct-a",
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
						"name":     "a",
						"base_url": upstreamA.URL,
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

	routesJSON := mustPrettyJSON(t, []map[string]any{
		{
			"model":          "edited-model",
			"mode":           "direct",
			"provider_group": "direct-b",
			"upstream_model": "upstream-b",
		},
	})
	groupsJSON := mustPrettyJSON(t, []map[string]any{
		{
			"name":              "direct-b",
			"strategy":          "fill_first",
			"timeout":           "2s",
			"retry_count":       0,
			"max_node_attempts": 1,
			"passive_health": map[string]any{
				"failure_threshold": 1,
				"cooldown":          "1s",
			},
			"nodes": []map[string]any{
				{
					"name":     "b",
					"base_url": upstreamB.URL,
				},
			},
		},
	})

	if err := runtime.ApplyConfigEdits(routesJSON, groupsJSON); err != nil {
		t.Fatalf("ApplyConfigEdits() error = %v", err)
	}

	routes := runtime.Routes()
	if len(routes) != 1 || routes[0].Model != "edited-model" || routes[0].ProviderGroup != "direct-b" {
		t.Fatalf("Routes() = %#v", routes)
	}

	status := runtime.Status()
	response, err := http.Post("http://"+status.ListenAddress+"/v1/chat/completions", "application/json", strings.NewReader(`{"model":"edited-model","messages":[{"role":"user","content":"hello"}]}`))
	if err != nil {
		t.Fatalf("POST chat after ApplyConfigEdits: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("POST chat status = %d, want 200", response.StatusCode)
	}

	file, err := clientconfig.LoadFile(configPath)
	if err != nil {
		t.Fatalf("LoadFile() error = %v", err)
	}
	if len(file.Routes) != 1 || file.Routes[0].Model != "edited-model" {
		t.Fatalf("config routes = %#v", file.Routes)
	}
}

func TestRuntimeSwitchCodexProviderBacksUpOriginalAndBackfillsPrevious(t *testing.T) {
	codexDir := filepath.Join(t.TempDir(), ".codex")
	if err := os.MkdirAll(codexDir, 0o700); err != nil {
		t.Fatalf("os.MkdirAll(codexDir) error = %v", err)
	}

	originalAuth := []byte("{\"access_token\":\"original-token\"}\n")
	originalConfig := strings.TrimSpace(`
model_provider = "original-live"

[model_providers.original-live]
base_url = "https://original.example.com"

[profiles.default]
model_provider = "original-live"
`) + "\n"
	if err := os.WriteFile(filepath.Join(codexDir, "auth.json"), originalAuth, 0o600); err != nil {
		t.Fatalf("os.WriteFile(auth.json) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(codexDir, "config.toml"), []byte(originalConfig), 0o600); err != nil {
		t.Fatalf("os.WriteFile(config.toml) error = %v", err)
	}

	configPath := writeTestConfig(t, map[string]any{
		"listen": "127.0.0.1:0",
		"codex": map[string]any{
			"config_dir": codexDir,
		},
		"routes": []map[string]any{
			{
				"model":          "codex-model",
				"mode":           "direct",
				"provider_group": "provider-a",
				"upstream_model": "upstream-model",
			},
		},
		"provider_groups": []map[string]any{
			{
				"name":              "provider-a",
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
						"name":     "a",
						"base_url": "https://provider-a.example.com",
					},
				},
				"codex": map[string]any{
					"auth": map[string]any{
						"access_token": "token-a",
					},
					"config": strings.TrimSpace(`
model_provider = "provider-a"

[model_providers.provider-a]
base_url = "https://provider-a.example.com"

[profiles.default]
model_provider = "provider-a"
`) + "\n",
				},
			},
			{
				"name":              "provider-b",
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
						"name":     "b",
						"base_url": "https://provider-b.example.com",
					},
				},
				"codex": map[string]any{
					"auth": map[string]any{
						"access_token": "token-b",
					},
					"config": strings.TrimSpace(`
model_provider = "provider-b"

[model_providers.provider-b]
base_url = "https://provider-b.example.com"

[profiles.default]
model_provider = "provider-b"
`) + "\n",
				},
			},
		},
	})

	runtime, err := NewRuntime(configPath)
	if err != nil {
		t.Fatalf("NewRuntime() error = %v", err)
	}

	switchCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := runtime.SwitchCodexProvider(switchCtx, "provider-a"); err != nil {
		t.Fatalf("SwitchCodexProvider(provider-a) error = %v", err)
	}

	backupDir := filepath.Join(filepath.Dir(configPath), "codex-live-backup")
	backupAuth, err := os.ReadFile(filepath.Join(backupDir, "original-auth.json"))
	if err != nil {
		t.Fatalf("os.ReadFile(original-auth.json) error = %v", err)
	}
	if !bytes.Equal(backupAuth, originalAuth) {
		t.Fatalf("backup auth = %q, want %q", backupAuth, originalAuth)
	}
	backupConfig, err := os.ReadFile(filepath.Join(backupDir, "original-config.toml"))
	if err != nil {
		t.Fatalf("os.ReadFile(original-config.toml) error = %v", err)
	}
	if !bytes.Equal(backupConfig, []byte(originalConfig)) {
		t.Fatalf("backup config = %q, want %q", backupConfig, originalConfig)
	}

	liveAuth, err := os.ReadFile(filepath.Join(codexDir, "auth.json"))
	if err != nil {
		t.Fatalf("os.ReadFile(live auth) error = %v", err)
	}
	if !strings.Contains(string(liveAuth), "token-a") {
		t.Fatalf("live auth after switch = %s, want token-a", liveAuth)
	}
	liveConfig, err := os.ReadFile(filepath.Join(codexDir, "config.toml"))
	if err != nil {
		t.Fatalf("os.ReadFile(live config) error = %v", err)
	}
	if !strings.Contains(string(liveConfig), `model_provider = "original-live"`) {
		t.Fatalf("live config after switch = %s, want stable original-live provider id", liveConfig)
	}
	if !strings.Contains(string(liveConfig), "https://provider-a.example.com") {
		t.Fatalf("live config after switch = %s, want provider-a base_url", liveConfig)
	}

	fileAfterA, err := clientconfig.LoadFile(configPath)
	if err != nil {
		t.Fatalf("LoadFile() after first codex switch error = %v", err)
	}
	if fileAfterA.Codex.CurrentProviderGroup != "provider-a" {
		t.Fatalf("current_provider_group = %q, want provider-a", fileAfterA.Codex.CurrentProviderGroup)
	}

	editedAuth := []byte("{\"access_token\":\"token-a-edited\"}\n")
	editedConfig := strings.TrimSpace(`
model_provider = "original-live"

[model_providers.original-live]
base_url = "https://provider-a-edited.example.com"

[profiles.default]
model_provider = "original-live"
`) + "\n"
	if err := os.WriteFile(filepath.Join(codexDir, "auth.json"), editedAuth, 0o600); err != nil {
		t.Fatalf("os.WriteFile(edited auth) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(codexDir, "config.toml"), []byte(editedConfig), 0o600); err != nil {
		t.Fatalf("os.WriteFile(edited config) error = %v", err)
	}

	if err := runtime.SwitchCodexProvider(switchCtx, "provider-b"); err != nil {
		t.Fatalf("SwitchCodexProvider(provider-b) error = %v", err)
	}

	fileAfterB, err := clientconfig.LoadFile(configPath)
	if err != nil {
		t.Fatalf("LoadFile() after second codex switch error = %v", err)
	}
	if fileAfterB.Codex.CurrentProviderGroup != "provider-b" {
		t.Fatalf("current_provider_group = %q, want provider-b", fileAfterB.Codex.CurrentProviderGroup)
	}
	if fileAfterB.ProviderGroups[0].Codex == nil {
		t.Fatal("provider-a codex settings = nil after backfill")
	}
	var providerAAuth map[string]any
	if err := json.Unmarshal(fileAfterB.ProviderGroups[0].Codex.Auth, &providerAAuth); err != nil {
		t.Fatalf("json.Unmarshal(provider-a backfilled auth) error = %v", err)
	}
	if providerAAuth["access_token"] != "token-a-edited" {
		t.Fatalf("provider-a backfilled auth = %#v, want edited token", providerAAuth)
	}
	if !strings.Contains(fileAfterB.ProviderGroups[0].Codex.Config, `model_provider = "provider-a"`) {
		t.Fatalf("provider-a backfilled config = %s, want restored provider-a id", fileAfterB.ProviderGroups[0].Codex.Config)
	}
	if !strings.Contains(fileAfterB.ProviderGroups[0].Codex.Config, "https://provider-a-edited.example.com") {
		t.Fatalf("provider-a backfilled config = %s, want edited base_url", fileAfterB.ProviderGroups[0].Codex.Config)
	}

	liveAuthAfterB, err := os.ReadFile(filepath.Join(codexDir, "auth.json"))
	if err != nil {
		t.Fatalf("os.ReadFile(live auth after second switch) error = %v", err)
	}
	if !strings.Contains(string(liveAuthAfterB), "token-b") {
		t.Fatalf("live auth after second switch = %s, want token-b", liveAuthAfterB)
	}
	liveConfigAfterB, err := os.ReadFile(filepath.Join(codexDir, "config.toml"))
	if err != nil {
		t.Fatalf("os.ReadFile(live config after second switch) error = %v", err)
	}
	if !strings.Contains(string(liveConfigAfterB), `model_provider = "original-live"`) {
		t.Fatalf("live config after second switch = %s, want stable original-live provider id", liveConfigAfterB)
	}
	if !strings.Contains(string(liveConfigAfterB), "https://provider-b.example.com") {
		t.Fatalf("live config after second switch = %s, want provider-b base_url", liveConfigAfterB)
	}
}

func TestRuntimeSyncProvidersReloadsProviderGroups(t *testing.T) {
	upstreamA := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"resp_a","model":"provider-sync-model","choices":[{"message":{"role":"assistant","content":"from-a"},"finish_reason":"stop"}]}`))
	}))
	t.Cleanup(upstreamA.Close)

	upstreamB := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"resp_b","model":"provider-sync-model","choices":[{"message":{"role":"assistant","content":"from-b"},"finish_reason":"stop"}]}`))
	}))
	t.Cleanup(upstreamB.Close)

	server, _, syncService := startCommunitySyncServer(t)
	if _, err := syncService.Bootstrap(context.Background(), testSnapshot("provider-sync-model", upstreamB.URL)); err != nil {
		t.Fatalf("Bootstrap() error = %v", err)
	}

	configPath := writeClientConfigFile(t, testSnapshot("provider-sync-model", upstreamA.URL), server.URL, true)

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
	before, err := http.Post("http://"+status.ListenAddress+"/v1/chat/completions", "application/json", strings.NewReader(`{"model":"provider-sync-model","messages":[{"role":"user","content":"hello"}]}`))
	if err != nil {
		t.Fatalf("POST chat before provider sync: %v", err)
	}
	defer before.Body.Close()
	beforeRaw, err := io.ReadAll(before.Body)
	if err != nil {
		t.Fatalf("io.ReadAll(before) error = %v", err)
	}
	if !strings.Contains(string(beforeRaw), "from-a") {
		t.Fatalf("provider sync before body = %s, want upstream A response", beforeRaw)
	}

	syncCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := runtime.SyncProviders(syncCtx); err != nil {
		t.Fatalf("SyncProviders() error = %v", err)
	}

	after, err := http.Post("http://"+status.ListenAddress+"/v1/chat/completions", "application/json", strings.NewReader(`{"model":"provider-sync-model","messages":[{"role":"user","content":"hello"}]}`))
	if err != nil {
		t.Fatalf("POST chat after provider sync: %v", err)
	}
	defer after.Body.Close()
	afterRaw, err := io.ReadAll(after.Body)
	if err != nil {
		t.Fatalf("io.ReadAll(after) error = %v", err)
	}
	if !strings.Contains(string(afterRaw), "from-b") {
		t.Fatalf("provider sync after body = %s, want upstream B response", afterRaw)
	}

	file, err := clientconfig.LoadFile(configPath)
	if err != nil {
		t.Fatalf("LoadFile() after provider sync error = %v", err)
	}
	if len(file.ProviderGroups) != 1 {
		t.Fatalf("provider groups len = %d, want 1", len(file.ProviderGroups))
	}
	if len(file.ProviderGroups[0].Nodes) != 1 || file.ProviderGroups[0].Nodes[0].BaseURL != upstreamB.URL {
		t.Fatalf("provider groups after sync = %#v, want upstream B", file.ProviderGroups)
	}
}

func TestRuntimeSyncNowPushesLocalSnapshot(t *testing.T) {
	upstream := startTestUpstream(t)
	server, store, _ := startCommunitySyncServer(t)

	snapshot := testSnapshot("manual-sync-model", upstream.URL)
	hash, err := runtimeconfig.HashSnapshot(snapshot)
	if err != nil {
		t.Fatalf("HashSnapshot() error = %v", err)
	}
	configPath := writeClientConfigFile(t, snapshot, server.URL, true)

	runtime, err := NewRuntime(configPath)
	if err != nil {
		t.Fatalf("NewRuntime() error = %v", err)
	}

	syncCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := runtime.SyncNow(syncCtx); err != nil {
		t.Fatalf("SyncNow() error = %v", err)
	}

	current, err := store.CurrentSnapshot(context.Background())
	if err != nil {
		t.Fatalf("CurrentSnapshot() error = %v", err)
	}
	if current.Hash != hash {
		t.Fatalf("CurrentSnapshot().Hash = %q, want %q", current.Hash, hash)
	}

	status := runtime.Status()
	if status.LastSyncStatus != clientsync.StatusApplied {
		t.Fatalf("Status().LastSyncStatus = %q, want applied", status.LastSyncStatus)
	}

	syncState := runtime.SyncState()
	if !syncState.Enabled {
		t.Fatal("SyncState().Enabled = false, want true")
	}
	if syncState.Status != clientsync.StatusApplied {
		t.Fatalf("SyncState().Status = %q, want applied", syncState.Status)
	}
	if syncState.LocalHash != hash {
		t.Fatalf("SyncState().LocalHash = %q, want %q", syncState.LocalHash, hash)
	}
	if syncState.CloudHash != hash {
		t.Fatalf("SyncState().CloudHash = %q, want %q", syncState.CloudHash, hash)
	}
	if syncState.LocalModifiedAt.IsZero() {
		t.Fatal("SyncState().LocalModifiedAt is zero")
	}
	if syncState.CloudUpdatedAt.IsZero() {
		t.Fatal("SyncState().CloudUpdatedAt is zero")
	}
}

func TestRuntimeStartupPullAppliesCloudSnapshot(t *testing.T) {
	upstream := startTestUpstream(t)
	server, store, syncService := startCommunitySyncServer(t)

	initialSnapshot := testSnapshot("initial-model", upstream.URL)
	initialRecord, err := syncService.Bootstrap(context.Background(), initialSnapshot)
	if err != nil {
		t.Fatalf("Bootstrap() error = %v", err)
	}
	initialHash, err := runtimeconfig.HashSnapshot(initialSnapshot)
	if err != nil {
		t.Fatalf("HashSnapshot(initial) error = %v", err)
	}

	configPath := writeClientConfigFile(t, initialSnapshot, server.URL, true)
	writeSyncState(t, configPath, clientsync.State{
		LastSyncAt:     initialRecord.UpdatedAt.UnixMilli(),
		CloudHash:      initialHash,
		LastSyncStatus: clientsync.StatusApplied,
	})
	if err := os.Chtimes(configPath, initialRecord.UpdatedAt, initialRecord.UpdatedAt); err != nil {
		t.Fatalf("os.Chtimes() error = %v", err)
	}

	time.Sleep(2 * time.Millisecond)

	updatedSnapshot := testSnapshot("cloud-model", upstream.URL)
	updatedHash, err := runtimeconfig.HashSnapshot(updatedSnapshot)
	if err != nil {
		t.Fatalf("HashSnapshot(updated) error = %v", err)
	}
	pushResponse, err := syncService.Push(context.Background(), communitysync.PushRequest{
		LastSyncAt:      initialRecord.UpdatedAt.UnixMilli(),
		LocalModifiedAt: time.Now().UTC().UnixMilli(),
		LocalHash:       updatedHash,
		Snapshot:        updatedSnapshot,
	})
	if err != nil {
		t.Fatalf("Push() error = %v", err)
	}
	if pushResponse.Status != communitysync.StatusApplied {
		t.Fatalf("Push().Status = %q, want applied", pushResponse.Status)
	}

	runtime, err := NewRuntime(configPath)
	if err != nil {
		t.Fatalf("NewRuntime() error = %v", err)
	}

	routes := runtime.Routes()
	if len(routes) != 1 || routes[0].Model != "cloud-model" {
		t.Fatalf("Routes() = %#v, want cloud snapshot", routes)
	}

	status := runtime.Status()
	if !status.SyncEnabled {
		t.Fatal("Status().SyncEnabled = false, want true")
	}
	if status.LastSyncStatus != clientsync.StatusUpdated {
		t.Fatalf("Status().LastSyncStatus = %q, want updated", status.LastSyncStatus)
	}
	if status.LastSyncTime.IsZero() {
		t.Fatal("Status().LastSyncTime is zero")
	}

	file, err := clientconfig.LoadFile(configPath)
	if err != nil {
		t.Fatalf("LoadFile() error = %v", err)
	}
	if len(file.Routes) != 1 || file.Routes[0].Model != "cloud-model" {
		t.Fatalf("config file routes = %#v, want cloud snapshot", file.Routes)
	}

	state := readSyncState(t, configPath)
	if state.LastSyncStatus != clientsync.StatusUpdated {
		t.Fatalf("sync state status = %q, want updated", state.LastSyncStatus)
	}
	if state.CloudHash != pushResponse.CloudHash {
		t.Fatalf("sync state hash = %q, want %q", state.CloudHash, pushResponse.CloudHash)
	}

	current, err := store.CurrentSnapshot(context.Background())
	if err != nil {
		t.Fatalf("CurrentSnapshot() error = %v", err)
	}
	if current.Hash != pushResponse.CloudHash {
		t.Fatalf("CurrentSnapshot().Hash = %q, want %q", current.Hash, pushResponse.CloudHash)
	}
}

func TestRuntimeShutdownPushesLocalSnapshot(t *testing.T) {
	upstream := startTestUpstream(t)
	server, store, _ := startCommunitySyncServer(t)
	localSnapshot := testSnapshot("local-model", upstream.URL)
	localHash, err := runtimeconfig.HashSnapshot(localSnapshot)
	if err != nil {
		t.Fatalf("HashSnapshot() error = %v", err)
	}

	configPath := writeClientConfigFile(t, localSnapshot, server.URL, true)

	runtime, err := NewRuntime(configPath)
	if err != nil {
		t.Fatalf("NewRuntime() error = %v", err)
	}
	if err := runtime.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := runtime.Shutdown(shutdownCtx); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}

	current, err := store.CurrentSnapshot(context.Background())
	if err != nil {
		t.Fatalf("CurrentSnapshot() error = %v", err)
	}
	if current.Hash != localHash {
		t.Fatalf("CurrentSnapshot().Hash = %q, want %q", current.Hash, localHash)
	}
	if len(current.Snapshot.Routes) != 1 || current.Snapshot.Routes[0].Model != "local-model" {
		t.Fatalf("CurrentSnapshot().Routes = %#v", current.Snapshot.Routes)
	}

	state := readSyncState(t, configPath)
	if state.LastSyncStatus != clientsync.StatusApplied {
		t.Fatalf("sync state status = %q, want applied", state.LastSyncStatus)
	}
	if state.CloudHash != localHash {
		t.Fatalf("sync state hash = %q, want %q", state.CloudHash, localHash)
	}
}

func TestRuntimeShutdownSyncFailureKeepsLocalConfig(t *testing.T) {
	upstream := startTestUpstream(t)
	server, _, _ := startCommunitySyncServer(t)
	localSnapshot := testSnapshot("local-model", upstream.URL)
	configPath := writeClientConfigFile(t, localSnapshot, server.URL, true)

	originalRaw, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("os.ReadFile(config) error = %v", err)
	}

	runtime, err := NewRuntime(configPath)
	if err != nil {
		t.Fatalf("NewRuntime() error = %v", err)
	}
	if err := runtime.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	server.Close()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err = runtime.Shutdown(shutdownCtx)
	if err == nil {
		t.Fatal("Shutdown() error = nil, want sync failure")
	}
	if !strings.Contains(err.Error(), "sync login") {
		t.Fatalf("Shutdown() error = %q, want sync login failure", err)
	}

	currentRaw, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("os.ReadFile(config after shutdown) error = %v", err)
	}
	if !bytes.Equal(currentRaw, originalRaw) {
		t.Fatalf("config changed after failed shutdown sync = %q, want %q", currentRaw, originalRaw)
	}

	file, err := clientconfig.LoadFile(configPath)
	if err != nil {
		t.Fatalf("LoadFile() after failed shutdown sync error = %v", err)
	}
	if len(file.Routes) != 1 || file.Routes[0].Model != "local-model" {
		t.Fatalf("config routes after failed shutdown sync = %#v, want local-model", file.Routes)
	}

	state := readSyncState(t, configPath)
	if state.LastSyncStatus != clientsync.StatusError {
		t.Fatalf("sync state status = %q, want error", state.LastSyncStatus)
	}
	if state.LastError == "" {
		t.Fatal("sync state LastError is empty, want shutdown sync failure")
	}
}

func TestRuntimeShowsFirstSyncConflict(t *testing.T) {
	runtime, configPath, _ := newRuntimeWithFirstSyncConflict(t, "local-model", "cloud-model")

	status := runtime.Status()
	if !status.HasSyncConflict {
		t.Fatal("Status().HasSyncConflict = false, want true")
	}
	if status.LastSyncStatus != clientsync.StatusConflict {
		t.Fatalf("Status().LastSyncStatus = %q, want conflict", status.LastSyncStatus)
	}

	conflict := runtime.SyncConflict()
	if !conflict.HasConflict {
		t.Fatal("SyncConflict().HasConflict = false, want true")
	}
	if conflict.Local.ModelCount != 1 || conflict.Cloud.ModelCount != 1 {
		t.Fatalf("conflict summaries = %#v", conflict)
	}

	syncState := runtime.SyncState()
	if !syncState.HasConflict {
		t.Fatal("SyncState().HasConflict = false, want true")
	}
	if syncState.LocalHash == "" {
		t.Fatal("SyncState().LocalHash is empty")
	}
	if syncState.CloudHash == "" {
		t.Fatal("SyncState().CloudHash is empty")
	}
	if syncState.LocalHash == syncState.CloudHash {
		t.Fatalf("SyncState hashes unexpectedly match: %q", syncState.LocalHash)
	}
	if syncState.CloudUpdatedAt.IsZero() {
		t.Fatal("SyncState().CloudUpdatedAt is zero")
	}

	state := readSyncState(t, configPath)
	if state.LastSyncStatus != clientsync.StatusConflict {
		t.Fatalf("sync state status = %q, want conflict", state.LastSyncStatus)
	}
	if state.Conflict == nil {
		t.Fatal("sync state conflict = nil, want populated conflict details")
	}

	file, err := clientconfig.LoadFile(configPath)
	if err != nil {
		t.Fatalf("LoadFile() during first-sync conflict error = %v", err)
	}
	if len(file.Routes) != 1 || file.Routes[0].Model != "local-model" {
		t.Fatalf("config routes during first-sync conflict = %#v, want local-model", file.Routes)
	}
}

func TestRuntimeResolvesConflictWithCloudSnapshot(t *testing.T) {
	runtime, configPath, _ := newRuntimeWithFirstSyncConflict(t, "local-model", "cloud-model")

	resolveCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := runtime.ResolveSyncConflict(resolveCtx, "cloud"); err != nil {
		t.Fatalf("ResolveSyncConflict(cloud) error = %v", err)
	}

	status := runtime.Status()
	if status.HasSyncConflict {
		t.Fatal("Status().HasSyncConflict = true, want false")
	}
	if status.LastSyncStatus != clientsync.StatusUpdated {
		t.Fatalf("Status().LastSyncStatus = %q, want updated", status.LastSyncStatus)
	}

	routes := runtime.Routes()
	if len(routes) != 1 || routes[0].Model != "cloud-model" {
		t.Fatalf("Routes() = %#v, want cloud-model", routes)
	}

	file, err := clientconfig.LoadFile(configPath)
	if err != nil {
		t.Fatalf("LoadFile() error = %v", err)
	}
	if len(file.Routes) != 1 || file.Routes[0].Model != "cloud-model" {
		t.Fatalf("config routes = %#v, want cloud-model", file.Routes)
	}

	state := readSyncState(t, configPath)
	if state.Conflict != nil {
		t.Fatalf("sync state conflict = %#v, want nil", state.Conflict)
	}
}

func TestRuntimeResolvesConflictByKeepingLocalSnapshot(t *testing.T) {
	runtime, configPath, store := newRuntimeWithFirstSyncConflict(t, "local-model", "cloud-model")
	localFile, err := clientconfig.LoadFile(configPath)
	if err != nil {
		t.Fatalf("LoadFile() error = %v", err)
	}
	localHash, err := runtimeconfig.HashSnapshot(localFile.Snapshot())
	if err != nil {
		t.Fatalf("HashSnapshot(local) error = %v", err)
	}

	resolveCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := runtime.ResolveSyncConflict(resolveCtx, "local"); err != nil {
		t.Fatalf("ResolveSyncConflict(local) error = %v", err)
	}

	status := runtime.Status()
	if status.HasSyncConflict {
		t.Fatal("Status().HasSyncConflict = true, want false")
	}
	if status.LastSyncStatus != clientsync.StatusApplied {
		t.Fatalf("Status().LastSyncStatus = %q, want applied", status.LastSyncStatus)
	}

	current, err := store.CurrentSnapshot(context.Background())
	if err != nil {
		t.Fatalf("CurrentSnapshot() error = %v", err)
	}
	if current.Hash != localHash {
		t.Fatalf("CurrentSnapshot().Hash = %q, want %q", current.Hash, localHash)
	}

	routes := runtime.Routes()
	if len(routes) != 1 || routes[0].Model != "local-model" {
		t.Fatalf("Routes() = %#v, want local-model", routes)
	}

	state := readSyncState(t, configPath)
	if state.Conflict != nil {
		t.Fatalf("sync state conflict = %#v, want nil", state.Conflict)
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

func startCommunitySyncServer(t *testing.T) (*httptest.Server, *memorystore.Store, *communitysync.Service) {
	t.Helper()

	store := memorystore.NewStore()
	authService, err := communityauth.New("admin", "secret-pass", "0123456789abcdef", time.Hour)
	if err != nil {
		t.Fatalf("auth.New() error = %v", err)
	}
	syncService := communitysync.NewService(store)
	relayService := communityrelay.NewService(store, &http.Client{})
	httpHandler := communityapi.New(authService, syncService, relayService)
	grpcServer := communitycontrolgrpc.NewServer(authService, syncService, relayService)
	server := httptest.NewServer(communitycontrolgrpc.NewMixedHandler(httpHandler, grpcServer))
	t.Cleanup(server.Close)
	return server, store, syncService
}

func newRuntimeWithFirstSyncConflict(t *testing.T, localModel, cloudModel string) (*Runtime, string, *memorystore.Store) {
	t.Helper()

	upstream := startTestUpstream(t)
	server, store, syncService := startCommunitySyncServer(t)

	cloudSnapshot := testSnapshot(cloudModel, upstream.URL)
	if _, err := syncService.Bootstrap(context.Background(), cloudSnapshot); err != nil {
		t.Fatalf("Bootstrap() error = %v", err)
	}

	localSnapshot := testSnapshot(localModel, upstream.URL)
	configPath := writeClientConfigFile(t, localSnapshot, server.URL, true)

	runtime, err := NewRuntime(configPath)
	if err != nil {
		t.Fatalf("NewRuntime() error = %v", err)
	}
	return runtime, configPath, store
}

func testSnapshot(model, baseURL string) runtimeconfig.Snapshot {
	return runtimeconfig.Snapshot{
		Routes: []runtimeconfig.Route{
			{
				Model:         model,
				Mode:          "direct",
				ProviderGroup: "direct",
				UpstreamModel: "upstream-model",
			},
		},
		ProviderGroups: []runtimeconfig.ProviderGroup{
			{
				Name:            "direct",
				Strategy:        "round_robin",
				Timeout:         runtimeconfig.Duration{Duration: 2 * time.Second},
				RetryCount:      0,
				MaxNodeAttempts: 1,
				PassiveHealth: runtimeconfig.PassiveHealthConfig{
					FailureThreshold: 1,
					Cooldown:         runtimeconfig.Duration{Duration: time.Second},
				},
				Nodes: []runtimeconfig.Node{
					{
						Name:    "primary",
						BaseURL: baseURL,
					},
				},
			},
		},
	}
}

func writeClientConfigFile(t *testing.T, snapshot runtimeconfig.Snapshot, serverURL string, enableSync bool) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "config.json")
	file := clientconfig.File{
		Listen:         "127.0.0.1:0",
		Routes:         snapshot.Routes,
		ProviderGroups: snapshot.ProviderGroups,
		Sync: clientconfig.SyncConfig{
			Enabled:   enableSync,
			ServerURL: serverURL,
			Username:  "admin",
			Password:  "secret-pass",
		},
	}
	if err := clientconfig.SaveFile(path, file); err != nil {
		t.Fatalf("SaveFile() error = %v", err)
	}
	return path
}

func writeSyncState(t *testing.T, configPath string, state clientsync.State) {
	t.Helper()

	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatalf("json.Marshal(sync state) error = %v", err)
	}
	if err := os.WriteFile(clientconfig.SyncStatePath(configPath), raw, 0o600); err != nil {
		t.Fatalf("os.WriteFile(sync state) error = %v", err)
	}
}

func readSyncState(t *testing.T, configPath string) clientsync.State {
	t.Helper()

	raw, err := os.ReadFile(clientconfig.SyncStatePath(configPath))
	if err != nil {
		t.Fatalf("os.ReadFile(sync state) error = %v", err)
	}

	var state clientsync.State
	if err := json.Unmarshal(raw, &state); err != nil {
		t.Fatalf("json.Unmarshal(sync state) error = %v", err)
	}
	return state
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

func mustPrettyJSON(t *testing.T, value any) string {
	t.Helper()

	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatalf("json.MarshalIndent() error = %v", err)
	}
	return string(raw)
}
