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

// OpenAICompatibleProvider speaks the OpenAI chat-completions protocol.
// Requesty and most gateways expose exactly this surface, so a single
// implementation covers many backends.
type OpenAICompatibleProvider struct {
	name    string
	baseURL string
	apiKey  string
	model   string
	client  *http.Client
}

type Config struct {
	Name    string
	BaseURL string
	APIKey  string
	Model   string
	Client  *http.Client
}

func NewOpenAICompatible(cfg Config) *OpenAICompatibleProvider {
	if cfg.Name == "" {
		cfg.Name = "openai-compatible"
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = "https://api.openai.com/v1"
	}
	client := cfg.Client
	if client == nil {
		client = &http.Client{Timeout: 180 * time.Second}
	}
	return &OpenAICompatibleProvider{
		name:    cfg.Name,
		baseURL: strings.TrimSuffix(cfg.BaseURL, "/"),
		apiKey:  cfg.APIKey,
		model:   cfg.Model,
		client:  client,
	}
}

func (p *OpenAICompatibleProvider) Name() string { return p.name }

type chatRequest struct {
	Model    string    `json:"model"`
	Messages []Message `json:"messages"`
	Tools    []Tool    `json:"tools,omitempty"`
}

type chatResponse struct {
	Choices []struct {
		Message struct {
			Content   string     `json:"content"`
			ToolCalls []ToolCall `json:"tool_calls"`
		} `json:"message"`
	} `json:"choices"`
	Usage Usage `json:"usage"`
}

type chatError struct {
	Error struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error"`
}

func (p *OpenAICompatibleProvider) Generate(ctx context.Context, req Request) (*Response, error) {
	if req.Model == "" {
		req.Model = p.model
	}
	body, err := json.Marshal(chatRequest{Model: req.Model, Messages: req.Messages, Tools: req.Tools})
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("new request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if p.apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)
	}
	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("do request: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}
	if resp.StatusCode >= 400 {
		var ce chatError
		_ = json.Unmarshal(raw, &ce)
		return nil, fmt.Errorf("llm api error (%d): %s", resp.StatusCode, ce.Error.Message)
	}
	var out chatResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}
	if len(out.Choices) == 0 {
		return nil, fmt.Errorf("llm returned no choices")
	}
	msg := out.Choices[0].Message
	return &Response{Content: msg.Content, ToolCalls: msg.ToolCalls, Usage: out.Usage}, nil
}
