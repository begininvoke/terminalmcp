package llm

import "context"

// Provider is the LLM abstraction the agent loop depends on. Implementations:
// Anthropic (native tools) and OpenAI-compatible (OpenRouter, vLLM/LiteLLM
// gateways, etc., with native OR prompt-emulated tools).
type Provider interface {
	Create(ctx context.Context, system string, messages []Message, tools []Tool) (*Response, error)
}
