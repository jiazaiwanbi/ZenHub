package executor

import (
	"context"
	"net/http"
	"net/url"

	"zenhub/internal/core/balancer"
	"zenhub/internal/core/canonical"
	"zenhub/internal/core/router"
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
	path := relayPath(req.Protocol, req.Model, false)
	return executeProtocolChat(ctx, r.client, group, node, req, req.Protocol, path)
}

func (r *Relay) Stream(
	ctx context.Context,
	group balancer.Group,
	decision router.Decision,
	node balancer.Node,
	req canonical.ChatRequest,
	yield func(canonical.StreamChunk) error,
) error {
	path := relayPath(req.Protocol, req.Model, true)
	streamReq := req
	streamReq.Stream = true
	return streamProtocolChat(ctx, r.client, group, node, streamReq, req.Protocol, path, yield)
}

func relayPath(protocol, model string, stream bool) string {
	switch normalizeProtocol(protocol) {
	case "anthropic":
		return "/api/v1/relay/messages"
	case "gemini":
		operation := "generateContent"
		if stream {
			operation = "streamGenerateContent"
		}
		return "/api/v1/relay/v1beta/models/" + url.PathEscape(model) + ":" + operation
	default:
		return "/api/v1/relay/chat/completions"
	}
}
