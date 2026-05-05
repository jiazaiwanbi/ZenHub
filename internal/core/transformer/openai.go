package transformer

import (
	"bytes"
	"encoding/json"

	"zenhub/internal/core/canonical"
)

func OpenAIChatRequest(req canonical.ChatRequest, upstreamModel string, forceStream *bool) ([]byte, error) {
	fields := cloneRawMap(req.RawExtensions)
	if fields == nil {
		fields = make(map[string]json.RawMessage)
	}

	model := req.Model
	if upstreamModel != "" {
		model = upstreamModel
	}
	modelJSON, err := json.Marshal(model)
	if err != nil {
		return nil, err
	}
	fields["model"] = modelJSON

	messagesJSON, err := marshalMessages(req.Messages)
	if err != nil {
		return nil, err
	}
	fields["messages"] = messagesJSON

	if err := setOptionalRawField(fields, req.RawFields, "tools", req.Tools); err != nil {
		return nil, err
	}
	if err := setOptionalJSONField(fields, req.RawFields, "temperature", req.Temperature); err != nil {
		return nil, err
	}
	if err := setOptionalJSONField(fields, req.RawFields, "top_p", req.TopP); err != nil {
		return nil, err
	}
	if err := setOptionalJSONField(fields, req.RawFields, "max_tokens", req.MaxTokens); err != nil {
		return nil, err
	}
	if err := setOptionalJSONField(fields, req.RawFields, "metadata", req.Metadata); err != nil {
		return nil, err
	}

	stream := req.Stream
	if forceStream != nil {
		stream = *forceStream
	}
	streamJSON, err := json.Marshal(stream)
	if err != nil {
		return nil, err
	}
	fields["stream"] = streamJSON

	return json.Marshal(fields)
}

func OpenAIChatResponse(provider string, body []byte) (*canonical.ChatResponse, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(body, &fields); err != nil {
		return nil, err
	}

	response := &canonical.ChatResponse{
		Provider:      provider,
		Raw:           append(json.RawMessage(nil), bytes.TrimSpace(body)...),
		RawExtensions: extensionFields(fields, knownChatResponseFields()),
	}

	_ = json.Unmarshal(fields["id"], &response.ResponseID)
	_ = json.Unmarshal(fields["model"], &response.Model)

	var usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	}
	if err := json.Unmarshal(fields["usage"], &usage); err == nil && usage.TotalTokens+usage.PromptTokens+usage.CompletionTokens > 0 {
		response.Usage = &canonical.Usage{
			PromptTokens:     usage.PromptTokens,
			CompletionTokens: usage.CompletionTokens,
			TotalTokens:      usage.TotalTokens,
		}
	}

	var choices []struct {
		FinishReason string          `json:"finish_reason"`
		Message      json.RawMessage `json:"message"`
		Delta        json.RawMessage `json:"delta"`
	}
	if err := json.Unmarshal(fields["choices"], &choices); err == nil {
		for _, choice := range choices {
			if response.FinishReason == "" {
				response.FinishReason = choice.FinishReason
			}
			if len(choice.Message) > 0 {
				response.OutputChunks = append(response.OutputChunks, append(json.RawMessage(nil), choice.Message...))
			}
			if len(choice.Delta) > 0 {
				response.OutputChunks = append(response.OutputChunks, append(json.RawMessage(nil), choice.Delta...))
			}
		}
	}

	return response, nil
}

func OpenAIStreamChunk(provider string, data []byte) canonical.StreamChunk {
	trimmed := bytes.TrimSpace(data)
	if bytes.Equal(trimmed, []byte("[DONE]")) {
		return canonical.StreamChunk{Provider: provider, Done: true}
	}

	chunk := canonical.StreamChunk{
		Provider: provider,
		Data:     append(json.RawMessage(nil), trimmed...),
	}

	var fields map[string]json.RawMessage
	if err := json.Unmarshal(trimmed, &fields); err == nil {
		_ = json.Unmarshal(fields["id"], &chunk.ResponseID)
		_ = json.Unmarshal(fields["model"], &chunk.Model)
		chunk.RawExtensions = extensionFields(fields, knownChatResponseFields())

		var choices []struct {
			FinishReason string `json:"finish_reason"`
		}
		if err := json.Unmarshal(fields["choices"], &choices); err == nil && len(choices) > 0 {
			chunk.FinishReason = choices[0].FinishReason
		}
	}

	return chunk
}

func cloneRawMap(fields map[string]json.RawMessage) map[string]json.RawMessage {
	if fields == nil {
		return nil
	}
	copied := make(map[string]json.RawMessage, len(fields))
	for key, value := range fields {
		copied[key] = append(json.RawMessage(nil), value...)
	}
	return copied
}

func cloneRaw(raw json.RawMessage) json.RawMessage {
	if raw == nil {
		return nil
	}
	return append(json.RawMessage(nil), raw...)
}

func marshalMessages(messages []canonical.Message) (json.RawMessage, error) {
	rawMessages := make([]json.RawMessage, 0, len(messages))
	for _, message := range messages {
		rawMessage, err := marshalMessage(message)
		if err != nil {
			return nil, err
		}
		rawMessages = append(rawMessages, rawMessage)
	}
	return json.Marshal(rawMessages)
}

func marshalMessage(message canonical.Message) (json.RawMessage, error) {
	fields := messageExtensionFields(message.RawFields)
	if fields == nil {
		fields = make(map[string]json.RawMessage)
	}

	roleJSON, err := json.Marshal(message.Role)
	if err != nil {
		return nil, err
	}
	fields["role"] = roleJSON

	if err := setOptionalRawField(fields, message.RawFields, "content", message.Content); err != nil {
		return nil, err
	}
	if err := setOptionalJSONField(fields, message.RawFields, "name", message.Name); err != nil {
		return nil, err
	}
	if err := setOptionalJSONField(fields, message.RawFields, "tool_call_id", message.ToolCallID); err != nil {
		return nil, err
	}
	if err := setOptionalRawField(fields, message.RawFields, "tool_calls", message.ToolCalls); err != nil {
		return nil, err
	}

	return json.Marshal(fields)
}

func messageExtensionFields(fields map[string]json.RawMessage) map[string]json.RawMessage {
	if fields == nil {
		return nil
	}

	extensions := make(map[string]json.RawMessage)
	for key, value := range fields {
		if knownMessageFields()[key] {
			continue
		}
		extensions[key] = cloneRaw(value)
	}
	return extensions
}

func knownMessageFields() map[string]bool {
	return map[string]bool{
		"role":         true,
		"content":      true,
		"name":         true,
		"tool_call_id": true,
		"tool_calls":   true,
	}
}

func setOptionalRawField(
	fields map[string]json.RawMessage,
	rawFields map[string]json.RawMessage,
	key string,
	value json.RawMessage,
) error {
	if value != nil {
		fields[key] = cloneRaw(value)
		return nil
	}
	if rawValue, ok := rawFields[key]; ok {
		fields[key] = cloneRaw(rawValue)
		return nil
	}
	delete(fields, key)
	return nil
}

func setOptionalJSONField(
	fields map[string]json.RawMessage,
	rawFields map[string]json.RawMessage,
	key string,
	value any,
) error {
	if shouldMarshalOptionalValue(value) {
		rawValue, err := json.Marshal(value)
		if err != nil {
			return err
		}
		fields[key] = rawValue
		return nil
	}
	if rawValue, ok := rawFields[key]; ok {
		fields[key] = cloneRaw(rawValue)
		return nil
	}
	delete(fields, key)
	return nil
}

func shouldMarshalOptionalValue(value any) bool {
	switch typed := value.(type) {
	case nil:
		return false
	case string:
		return typed != ""
	default:
		return true
	}
}

func extensionFields(fields map[string]json.RawMessage, known map[string]bool) map[string]json.RawMessage {
	extensions := make(map[string]json.RawMessage)
	for key, value := range fields {
		if known[key] {
			continue
		}
		extensions[key] = append(json.RawMessage(nil), value...)
	}
	return extensions
}

func knownChatResponseFields() map[string]bool {
	return map[string]bool{
		"id":       true,
		"object":   true,
		"created":  true,
		"model":    true,
		"choices":  true,
		"usage":    true,
		"provider": true,
	}
}
