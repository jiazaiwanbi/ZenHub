package app

import (
	"context"
	"errors"
	"net"
	"net/http"
	"sort"
	"sync"
	"time"

	"zenhub/internal/balancer"
	"zenhub/internal/config"
	"zenhub/internal/executor"
	"zenhub/internal/observability"
	"zenhub/internal/proxy"
	"zenhub/internal/router"
	apiserver "zenhub/internal/server"
)

const readHeaderTimeout = 5 * time.Second

type Runtime struct {
	mu            sync.RWMutex
	configPath    string
	runtimeConfig config.Runtime
	router        *router.Router
	service       *proxy.Service
	httpServer    *http.Server
	listener      net.Listener
	listenAddress string
	running       bool
	serveErr      error
	observability *observability.Recorder
}

type StatusView struct {
	Running            bool
	ListenAddress      string
	ConfigPath         string
	ModelCount         int
	Models             []string
	ProviderGroupCount int
	NodeCount          int
	ObservabilityLimit int
	LastRequestTime    time.Time
	LastRequestOK      bool
	ServiceError       string
}

type RouteView struct {
	Model             string
	RouteMode         string
	ProviderGroup     string
	UpstreamModel     string
	BalancingStrategy string
	Timeout           time.Duration
	RetryCount        int
	MaxNodeAttempts   int
	NodeCount         int
}

type RequestView struct {
	RequestTime       time.Time
	Model             string
	RouteMode         string
	SelectedNode      string
	BalancingStrategy string
	RetryCount        int
	Duration          time.Duration
	FinalStatus       string
	Error             string
}

func NewRuntime(configPath string) (*Runtime, error) {
	runtimeConfig, err := config.Load(configPath)
	if err != nil {
		return nil, err
	}

	routerInstance, err := router.New(runtimeConfig.Routes)
	if err != nil {
		return nil, err
	}

	balancerInstance, err := balancer.New(runtimeConfig.ProviderGroups)
	if err != nil {
		return nil, err
	}

	observer := observability.NewRecorder(runtimeConfig.ObservabilityLimit)
	service, err := proxy.New(
		routerInstance,
		balancerInstance,
		executor.NewDirect(&http.Client{}),
		observer,
	)
	if err != nil {
		return nil, err
	}

	httpServer := &http.Server{
		Addr:              runtimeConfig.Listen,
		Handler:           apiserver.New(service),
		ReadHeaderTimeout: readHeaderTimeout,
	}

	return &Runtime{
		configPath:    configPath,
		runtimeConfig: runtimeConfig,
		router:        routerInstance,
		service:       service,
		httpServer:    httpServer,
		listenAddress: runtimeConfig.Listen,
		observability: observer,
	}, nil
}

func (r *Runtime) Start() error {
	r.mu.Lock()
	if r.running {
		r.mu.Unlock()
		return nil
	}
	r.serveErr = nil
	addr := r.httpServer.Addr
	r.mu.Unlock()

	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}

	r.mu.Lock()
	r.listener = listener
	r.listenAddress = listener.Addr().String()
	r.running = true
	serverInstance := r.httpServer
	r.mu.Unlock()

	go func() {
		err := serverInstance.Serve(listener)
		r.mu.Lock()
		defer r.mu.Unlock()

		r.running = false
		r.listener = nil
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			r.serveErr = err
			return
		}
		r.serveErr = nil
	}()

	return nil
}

func (r *Runtime) Shutdown(ctx context.Context) error {
	r.mu.RLock()
	serverInstance := r.httpServer
	listener := r.listener
	r.mu.RUnlock()

	if listener == nil {
		return nil
	}
	return serverInstance.Shutdown(ctx)
}

func (r *Runtime) Status() StatusView {
	models := r.router.Models()
	lastRequest := time.Time{}
	records := r.observability.Records()
	if len(records) > 0 {
		lastRequest = records[len(records)-1].RequestTime
	}

	r.mu.RLock()
	status := StatusView{
		Running:            r.running,
		ListenAddress:      r.listenAddress,
		ConfigPath:         r.configPath,
		ModelCount:         len(models),
		Models:             models,
		ProviderGroupCount: len(r.runtimeConfig.ProviderGroups),
		NodeCount:          totalNodes(r.runtimeConfig.ProviderGroups),
		ObservabilityLimit: r.runtimeConfig.ObservabilityLimit,
		LastRequestTime:    lastRequest,
		LastRequestOK:      !lastRequest.IsZero(),
	}
	if r.serveErr != nil {
		status.ServiceError = r.serveErr.Error()
	}
	r.mu.RUnlock()

	return status
}

func (r *Runtime) Routes() []RouteView {
	groupByName := make(map[string]balancer.Group, len(r.runtimeConfig.ProviderGroups))
	for _, group := range r.runtimeConfig.ProviderGroups {
		groupByName[group.Name] = group
	}

	routes := make([]RouteView, 0, len(r.runtimeConfig.Routes))
	for _, route := range r.runtimeConfig.Routes {
		group := groupByName[route.ProviderGroup]
		routes = append(routes, RouteView{
			Model:             route.Model,
			RouteMode:         string(route.Mode),
			ProviderGroup:     route.ProviderGroup,
			UpstreamModel:     route.UpstreamModel,
			BalancingStrategy: string(group.Strategy),
			Timeout:           group.Timeout,
			RetryCount:        group.RetryCount,
			MaxNodeAttempts:   group.MaxNodeAttempts,
			NodeCount:         len(group.Nodes),
		})
	}

	sort.Slice(routes, func(i, j int) bool {
		return routes[i].Model < routes[j].Model
	})
	return routes
}

func (r *Runtime) Requests() []RequestView {
	records := r.observability.Records()
	requests := make([]RequestView, 0, len(records))
	for idx := len(records) - 1; idx >= 0; idx-- {
		record := records[idx]
		requests = append(requests, RequestView{
			RequestTime:       record.RequestTime,
			Model:             record.Model,
			RouteMode:         record.RouteMode,
			SelectedNode:      record.SelectedNode,
			BalancingStrategy: record.LoadBalancingStrategy,
			RetryCount:        record.RetryCount,
			Duration:          record.Duration,
			FinalStatus:       record.FinalStatus,
			Error:             record.Error,
		})
	}
	return requests
}

func totalNodes(groups []balancer.Group) int {
	total := 0
	for _, group := range groups {
		total += len(group.Nodes)
	}
	return total
}
