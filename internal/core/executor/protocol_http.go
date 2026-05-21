package executor

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"

	"zenhub/internal/core/balancer"
	"zenhub/internal/core/canonical"
	"zenhub/internal/core/transformer"
	coretranslator "zenhub/internal/core/translator"
)

func executeProtocolChat(
	ctx context.Context,
	client *http.Client,
	group balancer.Group,
	node balancer.Node,
	req canonical.ChatRequest,
	upstreamProtocol string,
	path string,
) (*canonical.ChatResponse, error) {
	payload, err := buildUpstreamRequest(req, upstreamProtocol)
	if err != nil {
		return nil, fmt.Errorf("build upstream request: %w", err)
	}

	httpResp, err := sendProtocolRequest(ctx, client, group, node, upstreamProtocol, payload, path, false)
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

	out := append([]byte(nil), body...)
	if !sameProtocol(req.Protocol, upstreamProtocol) {
		out, err = coretranslator.NonStream(ctx, upstreamProtocol, req.Protocol, req.Model, req.RawBody, payload, body)
		if err != nil {
			return nil, fmt.Errorf("translate upstream response: %w", err)
		}
	}

	response := &canonical.ChatResponse{
		Provider: node.Name,
		Model:    req.Model,
		Raw:      append([]byte(nil), out...),
	}
	if normalizeProtocol(req.Protocol) == "openai" {
		if parsed, err := transformer.OpenAIChatResponse(node.Name, out); err == nil {
			response.ResponseID = parsed.ResponseID
			response.Model = parsed.Model
			response.OutputChunks = parsed.OutputChunks
			response.FinishReason = parsed.FinishReason
			response.Usage = parsed.Usage
			response.RawExtensions = parsed.RawExtensions
		}
	}
	return response, nil
}

func streamProtocolChat(
	ctx context.Context,
	client *http.Client,
	group balancer.Group,
	node balancer.Node,
	req canonical.ChatRequest,
	upstreamProtocol string,
	path string,
	yield func(canonical.StreamChunk) error,
) error {
	payload, err := buildUpstreamRequest(req, upstreamProtocol)
	if err != nil {
		return fmt.Errorf("build upstream request: %w", err)
	}

	httpResp, err := sendProtocolRequest(ctx, client, group, node, upstreamProtocol, payload, path, true)
	if err != nil {
		return err
	}
	defer httpResp.Body.Close()

	if httpResp.StatusCode < http.StatusOK || httpResp.StatusCode >= http.StatusMultipleChoices {
		return readUpstreamError(httpResp)
	}

	if sameProtocol(req.Protocol, upstreamProtocol) {
		return proxyRawStream(httpResp.Body, node.Name, yield)
	}

	return translateStreamResponse(ctx, httpResp.Body, req, payload, upstreamProtocol, node.Name, yield)
}

func buildUpstreamRequest(req canonical.ChatRequest, upstreamProtocol string) ([]byte, error) {
	if len(req.RawBody) == 0 {
		return nil, fmt.Errorf("request body is empty")
	}
	return coretranslator.Request(req.Protocol, upstreamProtocol, req.Model, req.RawBody, req.Stream)
}

func translateStreamResponse(
	ctx context.Context,
	body io.Reader,
	req canonical.ChatRequest,
	translatedRequest []byte,
	upstreamProtocol string,
	provider string,
	yield func(canonical.StreamChunk) error,
) error {
	reader := bufio.NewReader(body)
	translatorState := &coretranslator.StreamState{}
	sentChunk := false
	sawOpenAIDone := false
	for {
		event, readErr := readSSEDataEvent(reader)
		if len(event) > 0 {
			if bytes.Equal(bytes.TrimSpace(event), []byte("data: [DONE]")) {
				sawOpenAIDone = true
			}
			translatedChunks, err := coretranslator.Stream(
				ctx,
				upstreamProtocol,
				req.Protocol,
				req.Model,
				req.RawBody,
				translatedRequest,
				event,
				translatorState,
			)
			if err != nil {
				return &StreamError{Cause: err, Started: sentChunk}
			}
			for _, translated := range translatedChunks {
				framed := frameStreamChunk(req.Protocol, translated)
				if len(framed) == 0 {
					continue
				}
				if err := yield(canonical.StreamChunk{Provider: provider, Data: framed}); err != nil {
					return &StreamError{Cause: err, Started: sentChunk || len(framed) > 0}
				}
				sentChunk = true
			}
		}

		if readErr != nil {
			if readErr == io.EOF {
				break
			}
			return &StreamError{
				Cause:   fmt.Errorf("read upstream stream: %w", readErr),
				Started: sentChunk,
			}
		}
	}

	if !sawOpenAIDone {
		finalChunks, err := coretranslator.FinalizeStream(ctx, upstreamProtocol, req.Protocol, req.Model, req.RawBody, translatorState)
		if err != nil {
			return &StreamError{Cause: err, Started: sentChunk}
		}
		for _, translated := range finalChunks {
			framed := frameStreamChunk(req.Protocol, translated)
			if len(framed) == 0 {
				continue
			}
			if err := yield(canonical.StreamChunk{Provider: provider, Data: framed}); err != nil {
				return &StreamError{Cause: err, Started: true}
			}
			sentChunk = true
		}
	}

	return nil
}

func proxyRawStream(body io.Reader, provider string, yield func(canonical.StreamChunk) error) error {
	reader := bufio.NewReader(body)
	sentChunk := false
	buffer := make([]byte, 32*1024)
	for {
		n, err := reader.Read(buffer)
		if n > 0 {
			chunk := append([]byte(nil), buffer[:n]...)
			if yieldErr := yield(canonical.StreamChunk{Provider: provider, Data: chunk}); yieldErr != nil {
				return &StreamError{Cause: yieldErr, Started: sentChunk || len(chunk) > 0}
			}
			sentChunk = true
		}
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return &StreamError{Cause: fmt.Errorf("read upstream stream: %w", err), Started: sentChunk}
		}
	}
}

func readSSEDataEvent(reader *bufio.Reader) ([]byte, error) {
	var dataLines [][]byte
	for {
		line, err := reader.ReadBytes('\n')
		if len(line) > 0 {
			trimmed := strings.TrimRight(string(line), "\r\n")
			switch {
			case trimmed == "":
				if len(dataLines) > 0 {
					return append([]byte("data: "), bytes.Join(dataLines, []byte("\n"))...), err
				}
			case strings.HasPrefix(trimmed, ":"):
			case strings.HasPrefix(trimmed, "data:"):
				dataLines = append(dataLines, []byte(strings.TrimSpace(strings.TrimPrefix(trimmed, "data:"))))
			}
		}
		if err != nil {
			if err == io.EOF && len(dataLines) > 0 {
				return append([]byte("data: "), bytes.Join(dataLines, []byte("\n"))...), nil
			}
			return nil, err
		}
	}
}

func frameStreamChunk(protocol string, chunk []byte) []byte {
	trimmed := bytes.TrimSpace(chunk)
	if len(trimmed) == 0 {
		return nil
	}
	switch protocol {
	case canonical.ProtocolOpenAIChatCompletions, canonical.ProtocolGeminiGenerateContent:
		if bytes.HasPrefix(trimmed, []byte("data:")) {
			return append(append([]byte(nil), trimmed...), '\n', '\n')
		}
		return append(append([]byte("data: "), trimmed...), '\n', '\n')
	case canonical.ProtocolAnthropicMessages:
		return append([]byte(nil), chunk...)
	default:
		return append([]byte(nil), chunk...)
	}
}

func sendProtocolRequest(
	ctx context.Context,
	client *http.Client,
	group balancer.Group,
	node balancer.Node,
	protocol string,
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
	setProtocolHeaders(httpReq, protocol, node.APIKey)
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

func setProtocolHeaders(req *http.Request, protocol, apiKey string) {
	switch protocol {
	case "anthropic", canonical.ProtocolAnthropicMessages:
		if apiKey != "" {
			req.Header.Set("x-api-key", apiKey)
		}
		req.Header.Set("anthropic-version", "2023-06-01")
	case "gemini", canonical.ProtocolGeminiGenerateContent:
		if apiKey != "" {
			req.Header.Set("x-goog-api-key", apiKey)
		}
	default:
		if apiKey != "" {
			req.Header.Set("Authorization", "Bearer "+apiKey)
		}
	}
}

func sameProtocol(a, b string) bool {
	return normalizeProtocol(a) == normalizeProtocol(b)
}

func normalizeProtocol(protocol string) string {
	switch strings.ToLower(strings.TrimSpace(protocol)) {
	case canonical.ProtocolOpenAIChatCompletions, "openai":
		return "openai"
	case canonical.ProtocolAnthropicMessages, "anthropic", "claude":
		return "anthropic"
	case canonical.ProtocolGeminiGenerateContent, "gemini":
		return "gemini"
	default:
		return strings.ToLower(strings.TrimSpace(protocol))
	}
}
