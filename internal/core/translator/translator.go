package translator

import (
	"context"
	"fmt"
	"strings"

	sdktranslator "github.com/router-for-me/CLIProxyAPI/v7/sdk/translator"
	_ "github.com/router-for-me/CLIProxyAPI/v7/sdk/translator/builtin"
	"zenhub/internal/core/canonical"
)

type StreamState struct {
	First  any
	Second any
}

func Request(fromProtocol, toProtocol, model string, rawJSON []byte, stream bool) ([]byte, error) {
	from, err := formatForProtocol(fromProtocol)
	if err != nil {
		return nil, err
	}
	to, err := formatForProtocol(toProtocol)
	if err != nil {
		return nil, err
	}

	if from == to {
		return sdktranslator.TranslateRequestByFormatName(from, to, model, rawJSON, stream), nil
	}
	if from != sdktranslator.FormatOpenAI && to != sdktranslator.FormatOpenAI {
		openaiReq := sdktranslator.TranslateRequestByFormatName(from, sdktranslator.FormatOpenAI, model, rawJSON, stream)
		return sdktranslator.TranslateRequestByFormatName(sdktranslator.FormatOpenAI, to, model, openaiReq, stream), nil
	}
	return sdktranslator.TranslateRequestByFormatName(from, to, model, rawJSON, stream), nil
}

func NonStream(
	ctx context.Context,
	fromProtocol, toProtocol, model string,
	originalRequestRawJSON, translatedRequestRawJSON, rawJSON []byte,
) ([]byte, error) {
	from, err := formatForProtocol(fromProtocol)
	if err != nil {
		return nil, err
	}
	to, err := formatForProtocol(toProtocol)
	if err != nil {
		return nil, err
	}

	if from == to {
		return append([]byte(nil), rawJSON...), nil
	}
	if from != sdktranslator.FormatOpenAI && to != sdktranslator.FormatOpenAI {
		openaiBody := sdktranslator.TranslateNonStreamByFormatName(
			ctx,
			from,
			sdktranslator.FormatOpenAI,
			model,
			translatedRequestRawJSON,
			translatedRequestRawJSON,
			rawJSON,
			nil,
		)
		return sdktranslator.TranslateNonStreamByFormatName(
			ctx,
			sdktranslator.FormatOpenAI,
			to,
			model,
			originalRequestRawJSON,
			openaiBody,
			openaiBody,
			nil,
		), nil
	}
	return sdktranslator.TranslateNonStreamByFormatName(
		ctx,
		from,
		to,
		model,
		originalRequestRawJSON,
		translatedRequestRawJSON,
		rawJSON,
		nil,
	), nil
}

func Stream(
	ctx context.Context,
	fromProtocol, toProtocol, model string,
	originalRequestRawJSON, translatedRequestRawJSON, rawChunk []byte,
	state *StreamState,
) ([][]byte, error) {
	from, err := formatForProtocol(fromProtocol)
	if err != nil {
		return nil, err
	}
	to, err := formatForProtocol(toProtocol)
	if err != nil {
		return nil, err
	}

	if from == to {
		return [][]byte{append([]byte(nil), rawChunk...)}, nil
	}
	if state == nil {
		state = &StreamState{}
	}
	if from != sdktranslator.FormatOpenAI && to != sdktranslator.FormatOpenAI {
		openaiChunks := sdktranslator.TranslateStreamByFormatName(
			ctx,
			from,
			sdktranslator.FormatOpenAI,
			model,
			translatedRequestRawJSON,
			translatedRequestRawJSON,
			rawChunk,
			&state.First,
		)
		out := make([][]byte, 0, len(openaiChunks))
		for _, openaiChunk := range openaiChunks {
			translated := sdktranslator.TranslateStreamByFormatName(
				ctx,
				sdktranslator.FormatOpenAI,
				to,
				model,
				originalRequestRawJSON,
				openaiChunk,
				openaiChunk,
				&state.Second,
			)
			out = append(out, translated...)
		}
		return out, nil
	}

	return sdktranslator.TranslateStreamByFormatName(
		ctx,
		from,
		to,
		model,
		originalRequestRawJSON,
		translatedRequestRawJSON,
		rawChunk,
		&state.First,
	), nil
}

func FinalizeStream(
	ctx context.Context,
	fromProtocol, toProtocol, model string,
	originalRequestRawJSON []byte,
	state *StreamState,
) ([][]byte, error) {
	from, err := formatForProtocol(fromProtocol)
	if err != nil {
		return nil, err
	}
	to, err := formatForProtocol(toProtocol)
	if err != nil {
		return nil, err
	}
	if from == to {
		return nil, nil
	}
	if state == nil {
		state = &StreamState{}
	}
	if to == sdktranslator.FormatOpenAI {
		return [][]byte{[]byte("data: [DONE]\n\n")}, nil
	}
	if to == sdktranslator.FormatClaude {
		done := []byte("data: [DONE]")
		if from != sdktranslator.FormatOpenAI {
			return sdktranslator.TranslateStreamByFormatName(
				ctx,
				sdktranslator.FormatOpenAI,
				to,
				model,
				originalRequestRawJSON,
				done,
				done,
				&state.Second,
			), nil
		}
		return sdktranslator.TranslateStreamByFormatName(
			ctx,
			from,
			to,
			model,
			originalRequestRawJSON,
			done,
			done,
			&state.First,
		), nil
	}
	return nil, nil
}

func formatForProtocol(protocol string) (sdktranslator.Format, error) {
	switch strings.ToLower(strings.TrimSpace(protocol)) {
	case canonical.ProtocolOpenAIChatCompletions, "openai":
		return sdktranslator.FormatOpenAI, nil
	case canonical.ProtocolAnthropicMessages, "anthropic", "claude":
		return sdktranslator.FormatClaude, nil
	case canonical.ProtocolGeminiGenerateContent, "gemini":
		return sdktranslator.FormatGemini, nil
	default:
		return "", fmt.Errorf("unsupported protocol %q", protocol)
	}
}
