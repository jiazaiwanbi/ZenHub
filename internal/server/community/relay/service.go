package relay

import (
	"context"
	"encoding/json"
	"net/http"

	"zenhub/internal/core/balancer"
	"zenhub/internal/core/canonical"
	"zenhub/internal/core/executor"
	"zenhub/internal/core/proxy"
	"zenhub/internal/core/router"
	"zenhub/internal/core/runtimeconfig"
	"zenhub/internal/server/community/storage"
)

type Service struct {
	store      storage.Store
	httpClient *http.Client
}

type ProviderCatalog struct {
	Version        int64
	CloudUpdatedAt int64
	CloudHash      string
	Providers      []runtimeconfig.Provider
	ProviderGroups []runtimeconfig.ProviderGroup
}

func NewService(store storage.Store, httpClient *http.Client) *Service {
	if httpClient == nil {
		httpClient = &http.Client{}
	}
	return &Service{
		store:      store,
		httpClient: httpClient,
	}
}

func (s *Service) Models(ctx context.Context) ([]string, error) {
	service, err := s.loadProxy(ctx)
	if err != nil {
		return nil, err
	}
	return service.Models(), nil
}

func (s *Service) ProviderCatalog(ctx context.Context) (ProviderCatalog, error) {
	record, err := s.store.CurrentSnapshot(ctx)
	if err != nil {
		return ProviderCatalog{}, err
	}

	return ProviderCatalog{
		Version:        record.Version,
		CloudUpdatedAt: record.UpdatedAt.UnixMilli(),
		CloudHash:      record.Hash,
		Providers:      cloneProviders(record.Snapshot.Providers),
		ProviderGroups: cloneProviderGroups(record.Snapshot.ProviderGroups),
	}, nil
}

func (s *Service) ExecuteChat(ctx context.Context, req canonical.ChatRequest) (*canonical.ChatResponse, error) {
	service, err := s.loadProxy(ctx)
	if err != nil {
		return nil, err
	}
	return service.ExecuteChat(ctx, req)
}

func (s *Service) StreamChat(
	ctx context.Context,
	req canonical.ChatRequest,
	yield func(canonical.StreamChunk) error,
) error {
	service, err := s.loadProxy(ctx)
	if err != nil {
		return err
	}
	return service.StreamChat(ctx, req, yield)
}

func (s *Service) loadProxy(ctx context.Context) (*proxy.Service, error) {
	record, err := s.store.CurrentSnapshot(ctx)
	if err != nil {
		return nil, err
	}

	runtime, err := record.Snapshot.Runtime("127.0.0.1:0", 1)
	if err != nil {
		return nil, err
	}

	routerInstance, err := router.New(runtime.Routes)
	if err != nil {
		return nil, err
	}

	balancerInstance, err := balancer.New(runtime.ProviderGroups)
	if err != nil {
		return nil, err
	}

	return proxy.New(
		routerInstance,
		balancerInstance,
		executor.NewDirect(s.httpClient),
		nil,
	)
}

func cloneProviderGroups(groups []runtimeconfig.ProviderGroup) []runtimeconfig.ProviderGroup {
	cloned := cloneSnapshot(runtimeconfig.Snapshot{ProviderGroups: groups})
	if cloned == nil {
		return nil
	}
	return cloned.ProviderGroups
}

func cloneProviders(providers []runtimeconfig.Provider) []runtimeconfig.Provider {
	cloned := cloneSnapshot(runtimeconfig.Snapshot{Providers: providers})
	if cloned == nil {
		return nil
	}
	return cloned.Providers
}

func cloneSnapshot(snapshot runtimeconfig.Snapshot) *runtimeconfig.Snapshot {
	raw, err := json.Marshal(snapshot)
	if err != nil {
		return nil
	}

	var cloned runtimeconfig.Snapshot
	if err := json.Unmarshal(raw, &cloned); err != nil {
		return nil
	}
	return &cloned
}
