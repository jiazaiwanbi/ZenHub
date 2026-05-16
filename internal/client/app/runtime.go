package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"sort"
	"sync"
	"time"

	clientcodex "zenhub/internal/client/codex"
	"zenhub/internal/client/config"
	localhostapi "zenhub/internal/client/localhostapi"
	clientsync "zenhub/internal/client/sync"
	"zenhub/internal/core/balancer"
	"zenhub/internal/core/canonical"
	"zenhub/internal/core/executor"
	"zenhub/internal/core/observability"
	"zenhub/internal/core/proxy"
	"zenhub/internal/core/router"
)

const (
	readHeaderTimeout = 5 * time.Second
	syncTimeout       = 15 * time.Second
)

type Runtime struct {
	mu            sync.RWMutex
	configPath    string
	runtimeConfig config.Runtime
	router        *router.Router
	service       *proxy.Service
	serviceHost   *serviceHost
	httpServer    *http.Server
	listener      net.Listener
	listenAddress string
	running       bool
	serveErr      error
	observability *observability.Recorder
	syncManager   *clientsync.Manager
	syncReport    clientsync.Report
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
	SyncEnabled        bool
	HasSyncConflict    bool
	LastSyncTime       time.Time
	LastSyncStatus     string
	SyncError          string
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

type CodexProviderView struct {
	Name    string
	Current bool
}

type SyncConflictView struct {
	HasConflict bool
	Reason      string
	Local       SyncSnapshotView
	Cloud       SyncSnapshotView
	Error       string
}

type SyncSnapshotView struct {
	Hash               string
	UpdatedAt          time.Time
	ModelCount         int
	ProviderGroupCount int
	NodeCount          int
	Models             []string
}

type SyncStateView struct {
	Enabled         bool
	Status          string
	Error           string
	HasConflict     bool
	LastSyncTime    time.Time
	LocalHash       string
	LocalModifiedAt time.Time
	CloudHash       string
	CloudUpdatedAt  time.Time
}

type ConfigEditorView struct {
	RoutesJSON         string
	ProviderGroupsJSON string
}

func NewRuntime(configPath string) (*Runtime, error) {
	syncManager := clientsync.NewManager(configPath, &http.Client{})
	syncCtx, cancel := context.WithTimeout(context.Background(), syncTimeout)
	defer cancel()

	file, syncReport, err := syncManager.SyncBeforeStart(syncCtx)
	runtimeConfig, runtimeErr := file.Runtime()
	if err != nil && runtimeErr != nil {
		return nil, err
	}
	if err == nil && runtimeErr != nil {
		return nil, runtimeErr
	}
	if err != nil {
		// Sync startup is best-effort for client availability. If the local
		// config is still valid, continue booting and surface the sync error in UI/status.
		runtimeErr = nil
	}
	if runtimeErr != nil {
		return nil, runtimeErr
	}

	observer := observability.NewRecorder(runtimeConfig.ObservabilityLimit)
	routerInstance, service, err := buildProxyRuntime(runtimeConfig, observer)
	if err != nil {
		return nil, err
	}

	serviceHost := newServiceHost(service)
	httpServer := &http.Server{
		Addr:              runtimeConfig.Listen,
		Handler:           localhostapi.New(serviceHost),
		ReadHeaderTimeout: readHeaderTimeout,
	}

	return &Runtime{
		configPath:    configPath,
		runtimeConfig: runtimeConfig,
		router:        routerInstance,
		service:       service,
		serviceHost:   serviceHost,
		httpServer:    httpServer,
		listenAddress: runtimeConfig.Listen,
		observability: observer,
		syncManager:   syncManager,
		syncReport:    syncReport,
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

	var shutdownErr error
	if listener == nil {
		shutdownErr = nil
	} else {
		shutdownErr = serverInstance.Shutdown(ctx)
	}

	syncErr := r.syncOnShutdown(ctx)
	if shutdownErr != nil {
		return shutdownErr
	}
	return syncErr
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
		SyncEnabled:        r.syncReport.Enabled,
		HasSyncConflict:    r.syncReport.HasConflict,
		LastSyncTime:       millisToTime(r.syncReport.LastSyncAt),
		LastSyncStatus:     r.syncReport.Status,
		SyncError:          r.syncReport.Error,
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

func (r *Runtime) syncOnShutdown(ctx context.Context) error {
	if r.syncManager == nil {
		return nil
	}

	report, err := r.syncManager.SyncOnShutdown(ctx)
	r.mu.Lock()
	r.syncReport = report
	r.mu.Unlock()
	return err
}

func (r *Runtime) SyncConflict() SyncConflictView {
	if r.syncManager == nil {
		return SyncConflictView{}
	}

	conflict, err := r.syncManager.CurrentConflict()
	if err != nil {
		return SyncConflictView{
			HasConflict: true,
			Error:       err.Error(),
		}
	}
	if !conflict.HasConflict {
		return SyncConflictView{}
	}

	return SyncConflictView{
		HasConflict: true,
		Reason:      conflict.Reason,
		Local:       summarizeSnapshot(conflict.Local),
		Cloud:       summarizeSnapshot(conflict.Cloud),
		Error:       conflict.Error,
	}
}

func (r *Runtime) SyncState() SyncStateView {
	if r.syncManager == nil {
		return SyncStateView{}
	}

	summary, err := r.syncManager.CurrentStatus()
	if err != nil {
		return SyncStateView{
			Enabled:      r.syncReport.Enabled,
			Status:       r.syncReport.Status,
			Error:        err.Error(),
			HasConflict:  r.syncReport.HasConflict,
			LastSyncTime: millisToTime(r.syncReport.LastSyncAt),
		}
	}

	return SyncStateView{
		Enabled:         summary.Enabled,
		Status:          summary.Status,
		Error:           chooseFirstNonEmpty(summary.Error, r.syncReport.Error),
		HasConflict:     summary.HasConflict,
		LastSyncTime:    millisToTime(summary.LastSyncAt),
		LocalHash:       summary.LocalHash,
		LocalModifiedAt: millisToTime(summary.LocalModifiedAt),
		CloudHash:       summary.CloudHash,
		CloudUpdatedAt:  millisToTime(summary.CloudUpdatedAt),
	}
}

func (r *Runtime) ResolveSyncConflict(ctx context.Context, choice string) error {
	if r.syncManager == nil {
		return errors.New("sync manager is not configured")
	}

	file, report, err := r.syncManager.ResolveConflict(ctx, clientsync.ConflictChoice(choice))
	if err != nil {
		return err
	}

	runtimeConfig, err := file.Runtime()
	if err != nil {
		return err
	}

	routerInstance, service, err := buildProxyRuntime(runtimeConfig, r.observability)
	if err != nil {
		return err
	}

	r.serviceHost.Replace(service)

	r.mu.Lock()
	r.runtimeConfig = runtimeConfig
	r.router = routerInstance
	r.service = service
	r.syncReport = report
	r.mu.Unlock()
	return nil
}

func (r *Runtime) EditableConfig() (ConfigEditorView, error) {
	file, err := config.LoadFile(r.configPath)
	if err != nil {
		return ConfigEditorView{}, err
	}

	routesJSON, err := prettyJSON(file.Routes)
	if err != nil {
		return ConfigEditorView{}, err
	}
	groupsJSON, err := prettyJSON(file.ProviderGroups)
	if err != nil {
		return ConfigEditorView{}, err
	}

	return ConfigEditorView{
		RoutesJSON:         routesJSON,
		ProviderGroupsJSON: groupsJSON,
	}, nil
}

func (r *Runtime) ApplyConfigEdits(routesJSON, providerGroupsJSON string) error {
	r.mu.RLock()
	hasConflict := r.syncReport.HasConflict
	r.mu.RUnlock()
	if hasConflict {
		return errors.New("resolve the sync conflict before applying config changes")
	}

	routes, err := decodeRoutesJSON(routesJSON)
	if err != nil {
		return err
	}
	groups, err := decodeProviderGroupsJSON(providerGroupsJSON)
	if err != nil {
		return err
	}

	file, err := config.LoadFile(r.configPath)
	if err != nil {
		return err
	}
	file.Routes = routes
	file.ProviderGroups = groups

	runtimeConfig, err := file.Runtime()
	if err != nil {
		return err
	}
	if err := config.SaveFile(r.configPath, file); err != nil {
		return err
	}

	return r.replaceRuntimeConfig(runtimeConfig)
}

func (r *Runtime) CodexProviders() []CodexProviderView {
	file, err := config.LoadFile(r.configPath)
	if err != nil {
		return nil
	}

	providers := clientcodex.ProviderViews(file)
	views := make([]CodexProviderView, 0, len(providers))
	for _, provider := range providers {
		views = append(views, CodexProviderView{
			Name:    provider.Name,
			Current: provider.Current,
		})
	}
	return views
}

func (r *Runtime) CurrentCodexProvider() string {
	file, err := config.LoadFile(r.configPath)
	if err != nil {
		return ""
	}
	return clientcodex.CurrentProviderName(file)
}

func (r *Runtime) SwitchCodexProvider(ctx context.Context, providerGroupName string) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	file, err := config.LoadFile(r.configPath)
	if err != nil {
		return err
	}

	if err := clientcodex.SwitchProvider(r.configPath, &file, providerGroupName); err != nil {
		return err
	}
	return config.SaveFile(r.configPath, file)
}

func (r *Runtime) SyncNow(ctx context.Context) error {
	r.mu.RLock()
	hasConflict := r.syncReport.HasConflict
	r.mu.RUnlock()
	if hasConflict {
		return errors.New("resolve the sync conflict before syncing")
	}
	if r.syncManager == nil {
		return errors.New("sync manager is not configured")
	}

	file, report, err := r.syncManager.SyncNow(ctx)
	r.mu.Lock()
	r.syncReport = report
	r.mu.Unlock()
	if err != nil {
		return err
	}

	runtimeConfig, err := file.Runtime()
	if err != nil {
		return err
	}
	return r.replaceRuntimeConfig(runtimeConfig)
}

func (r *Runtime) SyncProviders(ctx context.Context) error {
	if r.syncManager == nil {
		return errors.New("sync manager is not configured")
	}

	file, report, err := r.syncManager.SyncProviders(ctx)
	r.mu.Lock()
	r.syncReport = report
	r.mu.Unlock()
	if err != nil {
		return err
	}

	runtimeConfig, err := file.Runtime()
	if err != nil {
		return err
	}
	return r.replaceRuntimeConfig(runtimeConfig)
}

func millisToTime(value int64) time.Time {
	if value <= 0 {
		return time.Time{}
	}
	return time.UnixMilli(value).UTC()
}

func summarizeSnapshot(snapshot clientsync.ConflictSnapshot) SyncSnapshotView {
	models := make([]string, 0, len(snapshot.Snapshot.Routes))
	for _, route := range snapshot.Snapshot.Routes {
		models = append(models, route.Model)
	}
	sort.Strings(models)

	nodeCount := 0
	for _, group := range snapshot.Snapshot.ProviderGroups {
		nodeCount += len(group.Nodes)
	}

	return SyncSnapshotView{
		Hash:               snapshot.Hash,
		UpdatedAt:          millisToTime(snapshot.Timestamp),
		ModelCount:         len(snapshot.Snapshot.Routes),
		ProviderGroupCount: len(snapshot.Snapshot.ProviderGroups),
		NodeCount:          nodeCount,
		Models:             models,
	}
}

func buildProxyRuntime(
	runtimeConfig config.Runtime,
	observer *observability.Recorder,
) (*router.Router, *proxy.Service, error) {
	routerInstance, err := router.New(runtimeConfig.Routes)
	if err != nil {
		return nil, nil, err
	}

	balancerInstance, err := balancer.New(runtimeConfig.ProviderGroups)
	if err != nil {
		return nil, nil, err
	}

	service, err := proxy.NewWithOptions(
		routerInstance,
		balancerInstance,
		executor.NewDirect(&http.Client{}),
		observer,
		proxy.Options{
			AllowRelay:    true,
			RelayExecutor: executor.NewRelay(&http.Client{}),
		},
	)
	if err != nil {
		return nil, nil, err
	}
	return routerInstance, service, nil
}

func (r *Runtime) replaceRuntimeConfig(runtimeConfig config.Runtime) error {
	routerInstance, service, err := buildProxyRuntime(runtimeConfig, r.observability)
	if err != nil {
		return err
	}

	r.serviceHost.Replace(service)

	r.mu.Lock()
	r.runtimeConfig = runtimeConfig
	r.router = routerInstance
	r.service = service
	r.mu.Unlock()
	return nil
}

type serviceHost struct {
	mu      sync.RWMutex
	service localhostapi.ChatService
}

func newServiceHost(service localhostapi.ChatService) *serviceHost {
	return &serviceHost{service: service}
}

func (h *serviceHost) Replace(service localhostapi.ChatService) {
	h.mu.Lock()
	h.service = service
	h.mu.Unlock()
}

func (h *serviceHost) Models() []string {
	h.mu.RLock()
	service := h.service
	h.mu.RUnlock()
	if service == nil {
		return nil
	}
	return service.Models()
}

func (h *serviceHost) ExecuteChat(ctx context.Context, req canonical.ChatRequest) (*canonical.ChatResponse, error) {
	h.mu.RLock()
	service := h.service
	h.mu.RUnlock()
	if service == nil {
		return nil, errors.New("chat service is unavailable")
	}
	return service.ExecuteChat(ctx, req)
}

func (h *serviceHost) StreamChat(
	ctx context.Context,
	req canonical.ChatRequest,
	yield func(canonical.StreamChunk) error,
) error {
	h.mu.RLock()
	service := h.service
	h.mu.RUnlock()
	if service == nil {
		return errors.New("chat service is unavailable")
	}
	return service.StreamChat(ctx, req, yield)
}

func prettyJSON(value any) (string, error) {
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func decodeRoutesJSON(value string) ([]config.Route, error) {
	var routes []config.Route
	if err := decodeStrictJSON(value, &routes); err != nil {
		return nil, fmt.Errorf("decode routes JSON: %w", err)
	}
	return routes, nil
}

func decodeProviderGroupsJSON(value string) ([]config.ProviderGroup, error) {
	var groups []config.ProviderGroup
	if err := decodeStrictJSON(value, &groups); err != nil {
		return nil, fmt.Errorf("decode provider groups JSON: %w", err)
	}
	return groups, nil
}

func decodeStrictJSON(value string, target any) error {
	decoder := json.NewDecoder(bytes.NewBufferString(value))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err == nil {
		return errors.New("unexpected trailing JSON content")
	}
	return nil
}

func chooseFirstNonEmpty(primary, fallback string) string {
	if primary != "" {
		return primary
	}
	return fallback
}
