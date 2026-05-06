package relay

import (
	"context"
	"net/http"

	"zenhub/internal/core/balancer"
	"zenhub/internal/core/canonical"
	"zenhub/internal/core/executor"
	"zenhub/internal/core/proxy"
	"zenhub/internal/core/router"
	"zenhub/internal/server/community/storage"
)

type Service struct {
	store      storage.Store
	httpClient *http.Client
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

	return proxy.NewWithOptions(
		routerInstance,
		balancerInstance,
		executor.NewDirect(s.httpClient),
		nil,
		proxy.Options{AllowRelay: true},
	)
}
