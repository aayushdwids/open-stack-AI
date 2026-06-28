// Package inference provides the OpenAI-compatible /v1 surface, a native router with
// weighted failover, and pluggable backends (mock, llama.cpp, vLLM). The Go daemon owns
// the OpenAI surface and all telemetry; Python engines are dumb workers behind it.
package inference

// Message is one chat message.
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// ChatCompletionRequest is the OpenAI request shape.
type ChatCompletionRequest struct {
	Model       string    `json:"model"`
	Messages    []Message `json:"messages"`
	Temperature float64   `json:"temperature,omitempty"`
	MaxTokens   int       `json:"max_tokens,omitempty"`
	Stream      bool      `json:"stream,omitempty"`
}

// Choice is one completion choice.
type Choice struct {
	Index        int     `json:"index"`
	Message      Message `json:"message"`
	FinishReason string  `json:"finish_reason"`
}

// Usage reports token counts.
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// ChatCompletionResponse is the OpenAI response shape.
type ChatCompletionResponse struct {
	ID      string   `json:"id"`
	Object  string   `json:"object"`
	Created int64    `json:"created"`
	Model   string   `json:"model"`
	Choices []Choice `json:"choices"`
	Usage   Usage    `json:"usage"`
}

// ModelCard is one entry in /v1/models.
type ModelCard struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	OwnedBy string `json:"owned_by"`
}

// ModelsResponse is the /v1/models list.
type ModelsResponse struct {
	Object string      `json:"object"`
	Data   []ModelCard `json:"data"`
}

// LastUser returns the content of the last user message.
func (r ChatCompletionRequest) LastUser() string {
	for i := len(r.Messages) - 1; i >= 0; i-- {
		if r.Messages[i].Role == "user" {
			return r.Messages[i].Content
		}
	}
	if len(r.Messages) > 0 {
		return r.Messages[len(r.Messages)-1].Content
	}
	return ""
}
