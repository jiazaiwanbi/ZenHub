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

	"zenhub/internal/core/balancer"
	"zenhub/internal/core/canonical"
	"zenhub/internal/core/transformer"
)

const (
	directChatCompletionsPath = "/v1/chat/completions"
	relayChatCompletionsPath  = "/api/v1/relay/chat/completions"
)

func executeOpenAIChat(
	ctx context.Context,
	client *http.Client,
	group balancer.Group,
	node balancer.Node,
	payload []byte,
	path string,
) (*canonical.ChatResponse, error) {
	httpResp, err := sendOpenAIRequest(ctx, client, group, node, payload, path, false)
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

	response, err := transformerOpenAIChatResponse(node.Name, body)
	if err != nil {
		return nil, fmt.Errorf("parse upstream response: %w", err)
	}
	return response, nil
}

func streamOpenAIChat(
	ctx context.Context,
	client *http.Client,
	group balancer.Group,
	node balancer.Node,
	payload []byte,
	path string,
	yield func(canonical.StreamChunk) error,
) error {
	httpResp, err := sendOpenAIRequest(ctx, client, group, node, payload, path, true)
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

func sendOpenAIRequest(
	ctx context.Context,
	client *http.Client,
	group balancer.Group,
	node balancer.Node,
	payload []byte,
	path string,
	stream bool,
) (*http.Response, error) {
	requestURL, err := joinURL(node.BaseURL, path)
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

	clonedClient := *client
	clonedClient.Timeout = group.Timeout

	httpResp, err := clonedClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("send upstream request: %w", err)
	}
	return httpResp, nil
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

func transformerOpenAIChatResponse(provider string, body []byte) (*canonical.ChatResponse, error) {
	return transformer.OpenAIChatResponse(provider, body)
}
