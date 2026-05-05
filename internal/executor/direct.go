package executor

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"

	"zenhub/internal/balancer"
	"zenhub/internal/canonical"
	"zenhub/internal/router"
	"zenhub/internal/transformer"
)

const chatCompletionsPath = "/v1/chat/completions"

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

	httpResp, err := d.send(ctx, group, node, payload, false)
	if err != nil {
		return nil, err
	}
	defer httpResp.Body.Close()

	if httpResp.StatusCode < http.StatusOK || httpResp.StatusCode >= http.StatusMultipleChoices {
		return nil, readUpstreamError(httpResp)
	}

	body, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return nil, fmt.Errorf("read upstream response: %w", err)
	}

	response, err := transformer.OpenAIChatResponse(node.Name, body)
	if err != nil {
		return nil, fmt.Errorf("parse upstream response: %w", err)
	}
	return response, nil
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

	httpResp, err := d.send(ctx, group, node, payload, true)
	if err != nil {
		return err
	}
	defer httpResp.Body.Close()

	if httpResp.StatusCode < http.StatusOK || httpResp.StatusCode >= http.StatusMultipleChoices {
		return readUpstreamError(httpResp)
	}

	reader := bufio.NewReader(httpResp.Body)
	var dataLines [][]byte
	sentChunk := false
	doneSeen := false
	for {
		line, readErr := reader.ReadBytes('\n')
		if len(line) > 0 {
			trimmed := strings.TrimRight(string(line), "\r\n")
			switch {
			case trimmed == "":
				if len(dataLines) > 0 {
					chunk := transformer.OpenAIStreamChunk(node.Name, bytes.Join(dataLines, []byte("\n")))
					if chunk.Done {
						doneSeen = true
					}
					if err := yield(chunk); err != nil {
						return &StreamError{Cause: err, Started: sentChunk || chunk.Done || len(chunk.Data) > 0}
					}
					sentChunk = true
					dataLines = nil
				}
			case strings.HasPrefix(trimmed, ":"):
			case strings.HasPrefix(trimmed, "data:"):
				data := strings.TrimSpace(strings.TrimPrefix(trimmed, "data:"))
				dataLines = append(dataLines, []byte(data))
			}
		}

		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				break
			}
			return &StreamError{
				Cause:   fmt.Errorf("read upstream stream: %w", readErr),
				Started: sentChunk,
			}
		}
	}

	if len(dataLines) > 0 {
		chunk := transformer.OpenAIStreamChunk(node.Name, bytes.Join(dataLines, []byte("\n")))
		if chunk.Done {
			doneSeen = true
		}
		if err := yield(chunk); err != nil {
			return &StreamError{Cause: err, Started: sentChunk || chunk.Done || len(chunk.Data) > 0}
		}
		sentChunk = true
	}

	if !doneSeen {
		if err := yield(canonical.StreamChunk{Provider: node.Name, Done: true}); err != nil {
			return &StreamError{Cause: err, Started: true}
		}
	}

	return nil
}

func (d *Direct) send(
	ctx context.Context,
	group balancer.Group,
	node balancer.Node,
	payload []byte,
	stream bool,
) (*http.Response, error) {
	requestURL, err := joinURL(node.BaseURL, chatCompletionsPath)
	if err != nil {
		return nil, fmt.Errorf("build upstream URL: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, requestURL, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("create upstream request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if stream {
		httpReq.Header.Set("Accept", "text/event-stream")
	} else {
		httpReq.Header.Set("Accept", "application/json")
	}
	if node.APIKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+node.APIKey)
	}
	for key, value := range node.Headers {
		httpReq.Header.Set(key, value)
	}

	client := *d.client
	client.Timeout = group.Timeout

	httpResp, err := client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("send upstream request: %w", err)
	}
	return httpResp, nil
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

func joinURL(baseURL, suffix string) (string, error) {
	parsed, err := url.Parse(baseURL)
	if err != nil {
		return "", err
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/") + suffix
	return parsed.String(), nil
}

func IsRetryable(err error) bool {
	if err == nil {
		return false
	}

	var streamErr *StreamError
	if errors.As(err, &streamErr) {
		if streamErr.Started {
			return false
		}
		return IsRetryable(streamErr.Cause)
	}

	if errors.Is(err, context.Canceled) {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}

	var upstreamErr *UpstreamError
	if errors.As(err, &upstreamErr) {
		return upstreamErr.StatusCode == http.StatusRequestTimeout ||
			upstreamErr.StatusCode == http.StatusTooManyRequests ||
			upstreamErr.StatusCode >= http.StatusInternalServerError
	}

	var netErr net.Error
	return errors.As(err, &netErr)
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
