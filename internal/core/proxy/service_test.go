package proxy

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"zenhub/internal/core/balancer"
	"zenhub/internal/core/canonical"
	"zenhub/internal/core/executor"
	openaiprotocol "zenhub/internal/core/protocol/openai"
	"zenhub/internal/core/router"
)

func TestServiceRetriesWithinGroupAndRecordsObservation(t *testing.T) {
	var mu sync.Mutex
	node1Hits := 0
	node2Hits := 0

	node1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		node1Hits++
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, `{"error":{"message":"node1 failed"}}`)
	}))
	defer node1.Close()

	node2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		node2Hits++
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"id":"resp_1","model":"upstream-model","choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`)
	}))
	defer node2.Close()

	service := newTestService(t, []router.Rule{
		{Model: "local-model", Mode: router.RouteModeDirect, ProviderGroup: "direct", UpstreamModel: "upstream-model"},
	}, []balancer.Group{
		{
			Name:            "direct",
			Strategy:        balancer.StrategyRoundRobin,
			Timeout:         2 * time.Second,
			RetryCount:      1,
			MaxNodeAttempts: 2,
			PassiveHealth: balancer.PassiveHealth{
				FailureThreshold: 10,
				Cooldown:         time.Minute,
			},
			Nodes: []balancer.Node{
				{Name: "node1", BaseURL: node1.URL},
				{Name: "node2", BaseURL: node2.URL},
			},
		},
	})

	request := mustParseRequest(t, `{"model":"local-model","messages":[{"role":"user","content":"hello"}]}`)
	response, err := service.ExecuteChat(t.Context(), request)
	if err != nil {
		t.Fatalf("ExecuteChat() error = %v", err)
	}
	if response.Model != "upstream-model" {
		t.Fatalf("response.Model = %q, want upstream-model", response.Model)
	}

	mu.Lock()
	defer mu.Unlock()
	if node1Hits != 2 {
		t.Fatalf("node1Hits = %d, want 2", node1Hits)
	}
	if node2Hits != 1 {
		t.Fatalf("node2Hits = %d, want 1", node2Hits)
	}

	records := service.Records()
	if len(records) != 1 {
		t.Fatalf("records len = %d, want 1", len(records))
	}
	record := records[0]
	if record.RouteMode != string(router.RouteModeDirect) {
		t.Fatalf("RouteMode = %q, want direct", record.RouteMode)
	}
	if record.SelectedNode != "node2" {
		t.Fatalf("SelectedNode = %q, want node2", record.SelectedNode)
	}
	if record.RetryCount != 1 {
		t.Fatalf("RetryCount = %d, want 1", record.RetryCount)
	}
	if record.FinalStatus != "ok" {
		t.Fatalf("FinalStatus = %q, want ok", record.FinalStatus)
	}
}

func TestServiceHonorsMaxNodeAttempts(t *testing.T) {
	var mu sync.Mutex
	hits := map[string]int{"node1": 0, "node2": 0, "node3": 0}

	makeFailingNode := func(name string) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			mu.Lock()
			hits[name]++
			mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			fmt.Fprintf(w, `{"error":{"message":"%s failed"}}`, name)
		}))
	}

	node1 := makeFailingNode("node1")
	defer node1.Close()
	node2 := makeFailingNode("node2")
	defer node2.Close()
	node3 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		hits["node3"]++
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"id":"resp_3","model":"upstream-model","choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`)
	}))
	defer node3.Close()

	service := newTestService(t, []router.Rule{
		{Model: "local-model", Mode: router.RouteModeDirect, ProviderGroup: "direct"},
	}, []balancer.Group{
		{
			Name:            "direct",
			Strategy:        balancer.StrategyRoundRobin,
			Timeout:         2 * time.Second,
			RetryCount:      1,
			MaxNodeAttempts: 2,
			PassiveHealth: balancer.PassiveHealth{
				FailureThreshold: 10,
				Cooldown:         time.Minute,
			},
			Nodes: []balancer.Node{
				{Name: "node1", BaseURL: node1.URL},
				{Name: "node2", BaseURL: node2.URL},
				{Name: "node3", BaseURL: node3.URL},
			},
		},
	})

	request := mustParseRequest(t, `{"model":"local-model","messages":[{"role":"user","content":"hello"}]}`)
	_, err := service.ExecuteChat(t.Context(), request)
	var upstreamErr *executor.UpstreamError
	if !errors.As(err, &upstreamErr) {
		t.Fatalf("ExecuteChat() error = %v, want UpstreamError", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if hits["node1"] != 2 || hits["node2"] != 2 {
		t.Fatalf("hits = %#v, want node1=2 and node2=2", hits)
	}
	if hits["node3"] != 0 {
		t.Fatalf("node3Hits = %d, want 0", hits["node3"])
	}
}

func TestServiceRelayRouteReturnsNotImplemented(t *testing.T) {
	service := newTestService(t, []router.Rule{
		{Model: "relay-model", Mode: router.RouteModeRelay, ProviderGroup: "relay"},
	}, []balancer.Group{
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
				{Name: "relay-node", BaseURL: "http://relay.invalid"},
			},
		},
	})

	request := mustParseRequest(t, `{"model":"relay-model","messages":[{"role":"user","content":"hello"}]}`)
	_, err := service.ExecuteChat(t.Context(), request)
	if !errors.Is(err, executor.ErrRelayNotImplemented) {
		t.Fatalf("ExecuteChat() error = %v, want ErrRelayNotImplemented", err)
	}
}

func TestServiceRelayRouteCanBeEnabledForServerProducts(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"id":"resp_1","model":"upstream-model","choices":[{"message":{"role":"assistant","content":"relay ok"},"finish_reason":"stop"}]}`)
	}))
	defer upstream.Close()

	routerInstance, err := router.New([]router.Rule{
		{Model: "relay-model", Mode: router.RouteModeRelay, ProviderGroup: "relay", UpstreamModel: "upstream-model"},
	})
	if err != nil {
		t.Fatalf("router.New() error = %v", err)
	}

	balancerInstance, err := balancer.New([]balancer.Group{
		{
			Name:            "relay",
			Strategy:        balancer.StrategyRoundRobin,
			Timeout:         2 * time.Second,
			RetryCount:      0,
			MaxNodeAttempts: 1,
			PassiveHealth: balancer.PassiveHealth{
				FailureThreshold: 1,
				Cooldown:         time.Second,
			},
			Nodes: []balancer.Node{
				{Name: "relay-upstream", BaseURL: upstream.URL},
			},
		},
	})
	if err != nil {
		t.Fatalf("balancer.New() error = %v", err)
	}

	service, err := NewWithOptions(
		routerInstance,
		balancerInstance,
		executor.NewDirect(&http.Client{}),
		nil,
		Options{AllowRelay: true},
	)
	if err != nil {
		t.Fatalf("NewWithOptions() error = %v", err)
	}

	request := mustParseRequest(t, `{"model":"relay-model","messages":[{"role":"user","content":"hello"}]}`)
	response, err := service.ExecuteChat(t.Context(), request)
	if err != nil {
		t.Fatalf("ExecuteChat() error = %v", err)
	}
	if response.Model != "upstream-model" {
		t.Fatalf("response.Model = %q, want upstream-model", response.Model)
	}
}

func newTestService(t *testing.T, rules []router.Rule, groups []balancer.Group) *Service {
	t.Helper()

	routerInstance, err := router.New(rules)
	if err != nil {
		t.Fatalf("router.New() error = %v", err)
	}
	balancerInstance, err := balancer.New(groups)
	if err != nil {
		t.Fatalf("balancer.New() error = %v", err)
	}
	service, err := New(routerInstance, balancerInstance, executor.NewDirect(&http.Client{}), nil)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return service
}

func mustParseRequest(t *testing.T, body string) canonical.ChatRequest {
	t.Helper()

	request, err := openaiprotocol.ParseChatCompletion(strings.NewReader(body))
	if err != nil {
		t.Fatalf("ParseChatCompletion() error = %v", err)
	}
	return request
}
