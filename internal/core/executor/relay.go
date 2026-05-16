package executor

import (
	"context"
	"fmt"
	"net/http"

	"zenhub/internal/core/balancer"
	"zenhub/internal/core/canonical"
	"zenhub/internal/core/router"
	"zenhub/internal/core/transformer"
)

type Relay struct {
	client *http.Client
}

func NewRelay(client *http.Client) *Relay {
	if client == nil {
		client = &http.Client{}
	}
	return &Relay{client: client}
}

func (r *Relay) Execute(
	ctx context.Context,
	group balancer.Group,
	decision router.Decision,
	node balancer.Node,
	req canonical.ChatRequest,
) (*canonical.ChatResponse, error) {
	payload, err := transformer.OpenAIChatRequest(req, "", nil)
	if err != nil {
		return nil, fmt.Errorf("build relay request: %w", err)
	}
	return executeOpenAIChat(ctx, r.client, group, node, payload, relayChatCompletionsPath)
}

func (r *Relay) Stream(
	ctx context.Context,
	group balancer.Group,
	decision router.Decision,
	node balancer.Node,
	req canonical.ChatRequest,
	yield func(canonical.StreamChunk) error,
) error {
	forceStream := true
	payload, err := transformer.OpenAIChatRequest(req, "", &forceStream)
	if err != nil {
		return fmt.Errorf("build relay request: %w", err)
	}
	return streamOpenAIChat(ctx, r.client, group, node, payload, relayChatCompletionsPath, yield)
}
