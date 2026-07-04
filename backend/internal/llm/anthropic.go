package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Tool is an Anthropic tool definition.
type Tool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"input_schema"`
}

// Block is a content block in a message (text | tool_use | tool_result).
type Block struct {
	Type string `json:"type"`
	// text
	Text string `json:"text,omitempty"`
	// tool_use
	ID    string          `json:"id,omitempty"`
	Name  string          `json:"name,omitempty"`
	Input json.RawMessage `json:"input,omitempty"`
	// tool_result
	ToolUseID string `json:"tool_use_id,omitempty"`
	Content   string `json:"content,omitempty"`
}

// Message is one conversation turn.
type Message struct {
	Role    string  `json:"role"`
	Content []Block `json:"content"`
}

// Response is the model's reply for one turn.
type Response struct {
	Content    []Block
	StopReason string
}

// Client talks to the Anthropic Messages API.
type Client struct {
	apiKey     string
	model      string
	baseURL    string
	apiVersion string
	maxTokens  int
	temp       float64
	http       *http.Client
}

func NewAnthropic(apiKey, model, baseURL, apiVersion string, maxTokens int, temp float64) *Client {
	return &Client{
		apiKey:     apiKey,
		model:      model,
		baseURL:    baseURL,
		apiVersion: apiVersion,
		maxTokens:  maxTokens,
		temp:       temp,
		http:       &http.Client{Timeout: 5 * time.Minute},
	}
}

type apiRequest struct {
	Model       string    `json:"model"`
	MaxTokens   int       `json:"max_tokens"`
	System      string    `json:"system,omitempty"`
	Temperature float64   `json:"temperature"`
	Messages    []Message `json:"messages"`
	Tools       []Tool    `json:"tools,omitempty"`
}

type apiResponse struct {
	Content    []Block `json:"content"`
	StopReason string  `json:"stop_reason"`
	Error      *struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
}

// Create sends one turn and returns the model's response.
func (c *Client) Create(ctx context.Context, system string, messages []Message, tools []Tool) (*Response, error) {
	reqBody := apiRequest{
		Model:       c.model,
		MaxTokens:   c.maxTokens,
		System:      system,
		Temperature: c.temp,
		Messages:    messages,
		Tools:       tools,
	}
	b, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v1/messages", bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	req.Header.Set("content-type", "application/json")
	req.Header.Set("x-api-key", c.apiKey)
	req.Header.Set("anthropic-version", c.apiVersion)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("anthropic API %d: %s", resp.StatusCode, string(body))
	}

	var out apiResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("decode response: %w (body=%s)", err, string(body))
	}
	if out.Error != nil {
		return nil, fmt.Errorf("anthropic error: %s: %s", out.Error.Type, out.Error.Message)
	}
	return &Response{Content: out.Content, StopReason: out.StopReason}, nil
}
