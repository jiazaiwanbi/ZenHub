package canonical

import "encoding/json"

const ProtocolOpenAIChatCompletions = "openai_chat_completions"
const ProtocolAnthropicMessages = "anthropic_messages"
const ProtocolGeminiGenerateContent = "gemini_generate_content"

type ChatRequest struct {
	ID            string
	Protocol      string
	Model         string
	RawBody       json.RawMessage
	Messages      []Message
	Tools         json.RawMessage
	Temperature   *float64
	TopP          *float64
	MaxTokens     *int
	Stream        bool
	Metadata      map[string]any
	RawExtensions map[string]json.RawMessage
	RawFields     map[string]json.RawMessage
}

type Message struct {
	Role       string
	Content    json.RawMessage
	Name       string
	ToolCallID string
	ToolCalls  json.RawMessage
	RawFields  map[string]json.RawMessage
}

type Usage struct {
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
}

type ChatResponse struct {
	ResponseID    string
	Provider      string
	Model         string
	OutputChunks  []json.RawMessage
	FinishReason  string
	Usage         *Usage
	RawExtensions map[string]json.RawMessage
	Raw           json.RawMessage
}

type StreamChunk struct {
	ResponseID    string
	Provider      string
	Model         string
	Data          json.RawMessage
	Done          bool
	FinishReason  string
	RawExtensions map[string]json.RawMessage
}
