package executor

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"

	"zenhub/internal/core/balancer"
	"zenhub/internal/core/canonical"
	"zenhub/internal/core/router"
	"zenhub/internal/core/transformer"
)

var ErrRelayNotImplemented = errors.New("relay mode is not implemented in phase 1")

type UpstreamError struct {
	StatusCode  int
	Body        []byte
	ContentType string
}

type StreamError struct {
	Cause   error
	Started bool
}

type Direct struct {
	client *http.Client
}

func NewDirect(client *http.Client) *Direct {
	if client == nil {
		client = &http.Client{}
	}
	return &Direct{client: client}
}

func (d *Direct) Execute(
	ctx context.Context,
	group balancer.Group,
	decision router.Decision,
	node balancer.Node,
	req canonical.ChatRequest,
) (*canonical.ChatResponse, error) {
	payload, err := transformer.OpenAIChatRequest(req, decision.UpstreamModel, nil)
	if err != nil {
		return nil, fmt.Errorf("build upstream request: %w", err)
	}
	return executeOpenAIChat(ctx, d.client, group, node, payload, directChatCompletionsPath)
}

func (d *Direct) Stream(
	ctx context.Context,
	group balancer.Group,
	decision router.Decision,
	node balancer.Node,
	req canonical.ChatRequest,
	yield func(canonical.StreamChunk) error,
) error {
	forceStream := true
	payload, err := transformer.OpenAIChatRequest(req, decision.UpstreamModel, &forceStream)
	if err != nil {
		return fmt.Errorf("build upstream request: %w", err)
	}
	return streamOpenAIChat(ctx, d.client, group, node, payload, directChatCompletionsPath, yield)
}

func readUpstreamError(resp *http.Response) error {
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read upstream error: %w", err)
	}

	return &UpstreamError{
		StatusCode:  resp.StatusCode,
		Body:        append([]byte(nil), body...),
		ContentType: resp.Header.Get("Content-Type"),
	}
}

func (e *UpstreamError) Error() string {
	return fmt.Sprintf("upstream returned status %d", e.StatusCode)
}

func (e *StreamError) Error() string {
	return e.Cause.Error()
}

func (e *StreamError) Unwrap() error {
	return e.Cause
}
