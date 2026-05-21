package executor

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"zenhub/internal/core/balancer"
	"zenhub/internal/core/canonical"
	"zenhub/internal/core/router"
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
	model := req.Model
	if decision.UpstreamModel != "" {
		model = decision.UpstreamModel
	}
	path := directPath(group.Protocol, model, false)
	return executeProtocolChat(ctx, d.client, group, node, withModel(req, model), group.Protocol, path)
}

func (d *Direct) Stream(
	ctx context.Context,
	group balancer.Group,
	decision router.Decision,
	node balancer.Node,
	req canonical.ChatRequest,
	yield func(canonical.StreamChunk) error,
) error {
	model := req.Model
	if decision.UpstreamModel != "" {
		model = decision.UpstreamModel
	}
	path := directPath(group.Protocol, model, true)
	streamReq := withModel(req, model)
	streamReq.Stream = true
	return streamProtocolChat(ctx, d.client, group, node, streamReq, group.Protocol, path, yield)
}

func directPath(protocol, model string, stream bool) string {
	switch normalizeProtocol(protocol) {
	case "anthropic":
		return "/v1/messages"
	case "gemini":
		operation := "generateContent"
		if stream {
			operation = "streamGenerateContent"
		}
		return fmt.Sprintf("/v1beta/models/%s:%s", url.PathEscape(model), operation)
	default:
		return "/v1/chat/completions"
	}
}

func withModel(req canonical.ChatRequest, model string) canonical.ChatRequest {
	cloned := req
	cloned.Model = model
	return cloned
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
