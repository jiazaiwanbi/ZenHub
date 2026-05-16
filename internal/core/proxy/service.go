package proxy

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"zenhub/internal/core/balancer"
	"zenhub/internal/core/canonical"
	"zenhub/internal/core/executor"
	"zenhub/internal/core/observability"
	"zenhub/internal/core/router"
)

type Service struct {
	router   *router.Router
	balancer *balancer.Manager
	direct   ChatExecutor
	relay    ChatExecutor
	observer *observability.Recorder
	now      func() time.Time
	options  Options
}

type ChatExecutor interface {
	Execute(context.Context, balancer.Group, router.Decision, balancer.Node, canonical.ChatRequest) (*canonical.ChatResponse, error)
	Stream(context.Context, balancer.Group, router.Decision, balancer.Node, canonical.ChatRequest, func(canonical.StreamChunk) error) error
}

type Options struct {
	AllowRelay    bool
	RelayExecutor ChatExecutor
}

func New(
	routerInstance *router.Router,
	balancerInstance *balancer.Manager,
	directExecutor ChatExecutor,
	observer *observability.Recorder,
) (*Service, error) {
	return NewWithOptions(routerInstance, balancerInstance, directExecutor, observer, Options{})
}

func NewWithOptions(
	routerInstance *router.Router,
	balancerInstance *balancer.Manager,
	directExecutor ChatExecutor,
	observer *observability.Recorder,
	options Options,
) (*Service, error) {
	if routerInstance == nil {
		return nil, errors.New("router is required")
	}
	if balancerInstance == nil {
		return nil, errors.New("balancer is required")
	}
	if directExecutor == nil {
		return nil, errors.New("direct executor is required")
	}
	if observer == nil {
		observer = observability.NewRecorder(100)
	}

	return &Service{
		router:   routerInstance,
		balancer: balancerInstance,
		direct:   directExecutor,
		relay:    options.RelayExecutor,
		observer: observer,
		now:      time.Now,
		options:  options,
	}, nil
}

func (s *Service) Models() []string {
	return s.router.Models()
}

func (s *Service) ExecuteChat(ctx context.Context, req canonical.ChatRequest) (*canonical.ChatResponse, error) {
	return s.run(ctx, req, nil)
}

func (s *Service) StreamChat(
	ctx context.Context,
	req canonical.ChatRequest,
	yield func(canonical.StreamChunk) error,
) error {
	_, err := s.run(ctx, req, yield)
	return err
}

func (s *Service) Records() []observability.Record {
	return s.observer.Records()
}

func (s *Service) run(
	ctx context.Context,
	req canonical.ChatRequest,
	yield func(canonical.StreamChunk) error,
) (*canonical.ChatResponse, error) {
	startedAt := s.now()
	record := observability.Record{
		RequestTime: startedAt,
		Model:       req.Model,
	}

	finish := func(selectedNode string, strategy balancer.Strategy, retries int, err error) {
		record.SelectedNode = selectedNode
		record.LoadBalancingStrategy = string(strategy)
		record.RetryCount = retries
		record.Duration = time.Since(startedAt)
		if err == nil {
			record.FinalStatus = "ok"
		} else {
			record.FinalStatus = classifyError(err)
			record.Error = err.Error()
		}
		s.observer.Record(record)
	}

	decision, err := s.router.Select(req)
	if err != nil {
		finish("", "", 0, err)
		return nil, err
	}
	record.RouteMode = string(decision.Mode)

	exec := s.executorForMode(decision.Mode)
	if exec == nil {
		err := executor.ErrRelayNotImplemented
		finish("", "", 0, err)
		return nil, err
	}

	group, err := s.balancer.Group(decision.ProviderGroup)
	if err != nil {
		finish("", "", 0, err)
		return nil, err
	}

	selectedNode := ""
	retryCount := 0
	usedNodes := make(map[string]bool, group.MaxNodeAttempts)
	var lastErr error
	for nodeAttempt := 0; nodeAttempt < group.MaxNodeAttempts; nodeAttempt++ {
		selection, err := s.balancer.Select(group.Name, usedNodes)
		if err != nil {
			if lastErr == nil {
				lastErr = err
			}
			break
		}
		usedNodes[selection.Node.Name] = true
		selectedNode = selection.Node.Name

		for retry := 0; retry <= group.RetryCount; retry++ {
			if yield == nil {
				response, executeErr := exec.Execute(ctx, group, decision, selection.Node, req)
				if executeErr == nil {
					s.balancer.ReportSuccess(group.Name, selection.Node.Name)
					finish(selectedNode, group.Strategy, retryCount, nil)
					return response, nil
				}
				lastErr = executeErr
			} else {
				streamErr := exec.Stream(ctx, group, decision, selection.Node, req, yield)
				if streamErr == nil {
					s.balancer.ReportSuccess(group.Name, selection.Node.Name)
					finish(selectedNode, group.Strategy, retryCount, nil)
					return nil, nil
				}
				lastErr = streamErr

				var startedStream *executor.StreamError
				if errors.As(streamErr, &startedStream) && startedStream.Started {
					s.balancer.ReportFailure(group.Name, selection.Node.Name)
					finish(selectedNode, group.Strategy, retryCount, streamErr)
					return nil, streamErr
				}
			}

			s.balancer.ReportFailure(group.Name, selection.Node.Name)
			if !executor.IsRetryable(lastErr) {
				finish(selectedNode, group.Strategy, retryCount, lastErr)
				return nil, lastErr
			}
			if retry < group.RetryCount {
				retryCount++
			}
		}
	}

	if lastErr == nil {
		lastErr = fmt.Errorf("%w in %q", balancer.ErrNoHealthyNodes, group.Name)
	}
	finish(selectedNode, group.Strategy, retryCount, lastErr)
	return nil, lastErr
}

func (s *Service) executorForMode(mode router.RouteMode) ChatExecutor {
	switch mode {
	case router.RouteModeDirect:
		return s.direct
	case router.RouteModeRelay:
		if !s.options.AllowRelay {
			return nil
		}
		return s.relay
	default:
		return nil
	}
}

func classifyError(err error) string {
	if err == nil {
		return "ok"
	}

	switch {
	case errors.Is(err, executor.ErrRelayNotImplemented):
		return "relay_not_implemented"
	case errors.Is(err, balancer.ErrNoHealthyNodes):
		return "no_healthy_nodes"
	case errors.Is(err, router.ErrNoRoute):
		return "no_route"
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	}

	var upstreamErr *executor.UpstreamError
	if errors.As(err, &upstreamErr) {
		return fmt.Sprintf("upstream_%d", upstreamErr.StatusCode)
	}
	return "error"
}

func HTTPStatus(err error) int {
	switch {
	case err == nil:
		return http.StatusOK
	case errors.Is(err, executor.ErrRelayNotImplemented):
		return http.StatusNotImplemented
	case errors.Is(err, router.ErrNoRoute):
		return http.StatusNotFound
	case errors.Is(err, balancer.ErrNoHealthyNodes):
		return http.StatusServiceUnavailable
	case errors.Is(err, context.DeadlineExceeded):
		return http.StatusGatewayTimeout
	default:
		return http.StatusBadGateway
	}
}
