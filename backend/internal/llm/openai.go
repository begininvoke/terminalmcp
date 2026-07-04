package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// OpenAIClient targets any OpenAI-compatible Chat Completions API:
// OpenRouter, vLLM, LiteLLM gateways (e.g. the Digikala llm-hub), Ollama, etc.
//
// toolMode:
//   "native" — send the tools[] param and read message.tool_calls (OpenRouter,
//              and any server with function-calling enabled).
//   "prompt" — DO NOT send tools; instead describe them in the system prompt and
//              parse a single JSON action {"tool","input"} from the reply. Use
//              this for servers/models without tool-calling support.
type OpenAIClient struct {
	apiKey    string
	model     string
	baseURL   string // up to but excluding /chat/completions, e.g. https://openrouter.ai/api/v1
	toolMode  string
	maxTokens int
	temp      float64
	referer   string // optional, OpenRouter ranking header
	title     string
	http      *http.Client
}

func NewOpenAI(apiKey, model, baseURL, toolMode string, maxTokens int, temp float64) *OpenAIClient {
	if toolMode == "" {
		toolMode = "prompt"
	}
	return &OpenAIClient{
		apiKey:    apiKey,
		model:     model,
		baseURL:   strings.TrimRight(baseURL, "/"),
		toolMode:  toolMode,
		maxTokens: maxTokens,
		temp:      temp,
		referer:   "http://localhost:5173",
		title:     "AI Black-Box Pentest",
		http:      &http.Client{Timeout: 5 * time.Minute},
	}
}

// ---- wire types ----

type oaMessage struct {
	Role       string       `json:"role"`
	Content    *string      `json:"content"`
	ToolCalls  []oaToolCall `json:"tool_calls,omitempty"`
	ToolCallID string       `json:"tool_call_id,omitempty"`
}

type oaToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type oaTool struct {
	Type     string `json:"type"`
	Function struct {
		Name        string          `json:"name"`
		Description string          `json:"description"`
		Parameters  json.RawMessage `json:"parameters"`
	} `json:"function"`
}

type oaRequest struct {
	Model       string      `json:"model"`
	Messages    []oaMessage `json:"messages"`
	Tools       []oaTool    `json:"tools,omitempty"`
	MaxTokens   int         `json:"max_tokens,omitempty"`
	Temperature float64     `json:"temperature"`
}

type oaResponse struct {
	Choices []struct {
		FinishReason string    `json:"finish_reason"`
		Message      oaMessage `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

func strptr(s string) *string { return &s }

// Create implements Provider.
func (c *OpenAIClient) Create(ctx context.Context, system string, messages []Message, tools []Tool) (*Response, error) {
	var req oaRequest
	req.Model = c.model
	req.MaxTokens = c.maxTokens
	req.Temperature = c.temp

	if c.toolMode == "native" {
		req.Messages = append([]oaMessage{{Role: "system", Content: strptr(system)}}, c.toNativeMessages(messages)...)
		req.Tools = toOATools(tools)
	} else {
		sys := system + "\n\n" + promptToolInstructions(tools)
		req.Messages = append([]oaMessage{{Role: "system", Content: strptr(sys)}}, c.toPromptMessages(messages)...)
	}

	body, err := c.do(ctx, &req)
	if err != nil {
		return nil, err
	}
	if c.toolMode == "native" {
		return parseNative(body)
	}
	return parsePrompt(body)
}

func (c *OpenAIClient) do(ctx context.Context, req *oaRequest) (*oaResponse, error) {
	b, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	// Retry transient failures (dropped connections, 429, 5xx) — gateways hiccup
	// and a single blip should not kill the whole engagement.
	const maxAttempts = 4
	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if attempt > 1 {
			backoff := time.Duration(attempt-1) * 2 * time.Second
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(backoff):
			}
		}

		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(b))
		if err != nil {
			return nil, err
		}
		httpReq.Header.Set("content-type", "application/json")
		httpReq.Header.Set("authorization", "Bearer "+c.apiKey)
		httpReq.Header.Set("HTTP-Referer", c.referer) // OpenRouter optional ranking headers
		httpReq.Header.Set("X-Title", c.title)

		resp, err := c.http.Do(httpReq)
		if err != nil {
			lastErr = err // network error (EOF, reset, timeout) — retry
			continue
		}
		raw, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		if resp.StatusCode == 429 || resp.StatusCode >= 500 {
			lastErr = fmt.Errorf("openai-compatible API %d: %s", resp.StatusCode, string(raw))
			continue
		}
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("openai-compatible API %d: %s", resp.StatusCode, string(raw))
		}
		var out oaResponse
		if err := json.Unmarshal(raw, &out); err != nil {
			return nil, fmt.Errorf("decode response: %w (body=%s)", err, string(raw))
		}
		if out.Error != nil {
			return nil, fmt.Errorf("openai-compatible error: %s", out.Error.Message)
		}
		if len(out.Choices) == 0 {
			return nil, fmt.Errorf("no choices in response: %s", string(raw))
		}
		return &out, nil
	}
	return nil, fmt.Errorf("LLM request failed after %d attempts: %w", maxAttempts, lastErr)
}

// ---- native mode ----

func toOATools(tools []Tool) []oaTool {
	out := make([]oaTool, 0, len(tools))
	for _, t := range tools {
		var ot oaTool
		ot.Type = "function"
		ot.Function.Name = t.Name
		ot.Function.Description = t.Description
		ot.Function.Parameters = t.InputSchema
		out = append(out, ot)
	}
	return out
}

func (c *OpenAIClient) toNativeMessages(messages []Message) []oaMessage {
	var out []oaMessage
	for _, m := range messages {
		switch m.Role {
		case "assistant":
			msg := oaMessage{Role: "assistant", Content: strptr("")}
			var text strings.Builder
			for _, b := range m.Content {
				switch b.Type {
				case "text":
					text.WriteString(b.Text)
				case "tool_use":
					var tc oaToolCall
					tc.ID = b.ID
					tc.Type = "function"
					tc.Function.Name = b.Name
					tc.Function.Arguments = string(b.Input)
					msg.ToolCalls = append(msg.ToolCalls, tc)
				}
			}
			if t := text.String(); t != "" {
				msg.Content = strptr(t)
			}
			out = append(out, msg)
		case "user":
			var text strings.Builder
			for _, b := range m.Content {
				switch b.Type {
				case "text":
					text.WriteString(b.Text)
				case "tool_result":
					out = append(out, oaMessage{Role: "tool", ToolCallID: b.ToolUseID, Content: strptr(b.Content)})
				}
			}
			if t := text.String(); t != "" {
				out = append(out, oaMessage{Role: "user", Content: strptr(t)})
			}
		}
	}
	return out
}

func parseNative(r *oaResponse) (*Response, error) {
	choice := r.Choices[0]
	var blocks []Block
	if choice.Message.Content != nil && *choice.Message.Content != "" {
		blocks = append(blocks, Block{Type: "text", Text: *choice.Message.Content})
	}
	for _, tc := range choice.Message.ToolCalls {
		args := tc.Function.Arguments
		if strings.TrimSpace(args) == "" {
			args = "{}"
		}
		blocks = append(blocks, Block{Type: "tool_use", ID: tc.ID, Name: tc.Function.Name, Input: json.RawMessage(args)})
	}
	stop := "end_turn"
	if choice.FinishReason == "tool_calls" || hasToolUse(blocks) {
		stop = "tool_use"
	}
	return &Response{Content: blocks, StopReason: stop}, nil
}

// ---- prompt-emulation mode ----

func promptToolInstructions(tools []Tool) string {
	var sb strings.Builder
	sb.WriteString("TOOL PROTOCOL (this server has no native tool-calling):\n")
	sb.WriteString("To use a tool, reply with EXACTLY ONE JSON object and nothing else:\n")
	sb.WriteString(`  {"tool":"<tool_name>","input":{...}}` + "\n")
	sb.WriteString("Do not wrap it in code fences. Do not add prose before or after the JSON.\n")
	sb.WriteString("When the engagement is fully complete (after finalize_report), reply with: ")
	sb.WriteString(`{"tool":"none","input":{}}` + "\n\n")
	sb.WriteString("Available tools:\n")
	for _, t := range tools {
		sb.WriteString(fmt.Sprintf("- %s: %s\n  input schema: %s\n", t.Name, t.Description, string(t.InputSchema)))
	}
	return sb.String()
}

func (c *OpenAIClient) toPromptMessages(messages []Message) []oaMessage {
	var out []oaMessage
	for _, m := range messages {
		switch m.Role {
		case "assistant":
			var content string
			for _, b := range m.Content {
				switch b.Type {
				case "text":
					content += b.Text
				case "tool_use":
					action := map[string]any{"tool": b.Name, "input": json.RawMessage(b.Input)}
					j, _ := json.Marshal(action)
					content += string(j)
				}
			}
			out = append(out, oaMessage{Role: "assistant", Content: strptr(content)})
		case "user":
			var content strings.Builder
			for _, b := range m.Content {
				switch b.Type {
				case "text":
					content.WriteString(b.Text)
				case "tool_result":
					content.WriteString("TOOL RESULT:\n")
					content.WriteString(b.Content)
				}
			}
			out = append(out, oaMessage{Role: "user", Content: strptr(content.String())})
		}
	}
	return out
}

func parsePrompt(r *oaResponse) (*Response, error) {
	content := ""
	if r.Choices[0].Message.Content != nil {
		content = *r.Choices[0].Message.Content
	}
	jsonStr := extractFirstJSON(content)
	if jsonStr != "" {
		var action struct {
			Tool  string          `json:"tool"`
			Input json.RawMessage `json:"input"`
		}
		if err := json.Unmarshal([]byte(jsonStr), &action); err == nil && action.Tool != "" {
			if action.Tool == "none" || action.Tool == "finish" {
				return &Response{Content: []Block{{Type: "text", Text: content}}, StopReason: "end_turn"}, nil
			}
			input := action.Input
			if len(input) == 0 {
				input = json.RawMessage("{}")
			}
			return &Response{
				Content:    []Block{{Type: "tool_use", ID: fmt.Sprintf("call_%d", time.Now().UnixNano()), Name: action.Tool, Input: input}},
				StopReason: "tool_use",
			}, nil
		}
	}
	// No parseable action — treat as a final text turn.
	return &Response{Content: []Block{{Type: "text", Text: content}}, StopReason: "end_turn"}, nil
}

// extractFirstJSON returns the first balanced {...} object in s, tolerating code
// fences and surrounding prose. Returns "" if none found.
func extractFirstJSON(s string) string {
	s = strings.ReplaceAll(s, "```json", "")
	s = strings.ReplaceAll(s, "```", "")
	start := strings.IndexByte(s, '{')
	if start < 0 {
		return ""
	}
	depth := 0
	inStr := false
	esc := false
	for i := start; i < len(s); i++ {
		ch := s[i]
		if inStr {
			if esc {
				esc = false
			} else if ch == '\\' {
				esc = true
			} else if ch == '"' {
				inStr = false
			}
			continue
		}
		switch ch {
		case '"':
			inStr = true
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return s[start : i+1]
			}
		}
	}
	return ""
}

func hasToolUse(blocks []Block) bool {
	for _, b := range blocks {
		if b.Type == "tool_use" {
			return true
		}
	}
	return false
}
