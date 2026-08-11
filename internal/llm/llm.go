// Package llm provides a minimal chat client for LLM-powered memory features:
// auto topic extraction and project summarization. Supports Ollama, any
// OpenAI-compatible endpoint and the Anthropic Messages API. Optional: nil
// client disables LLM features.
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

// Client is the minimal chat interface used by memory features.
type Client interface {
	// Chat sends a system+user message and returns the assistant reply.
	Chat(ctx context.Context, system, user string) (string, error)
}

// Client is the shared HTTP client for LLM calls.
var httpClient = &http.Client{Timeout: 120 * time.Second}

// OllamaClient talks to a local Ollama /api/chat endpoint.
type OllamaClient struct {
	BaseURL string
	Model   string
}

// NewOllama returns an Ollama client with defaults.
func NewOllama(baseURL, model string) *OllamaClient {
	if baseURL == "" {
		baseURL = "http://localhost:11434"
	}
	if model == "" {
		model = "llama3.2"
	}
	return &OllamaClient{BaseURL: strings.TrimRight(baseURL, "/"), Model: model}
}

func (o *OllamaClient) Chat(ctx context.Context, system, user string) (string, error) {
	body, err := json.Marshal(map[string]any{
		"model":  o.Model,
		"stream": false,
		"format": "json",
		"messages": []map[string]string{
			{"role": "system", "content": system},
			{"role": "user", "content": user},
		},
	})
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, o.BaseURL+"/api/chat", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("ollama chat: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return "", fmt.Errorf("ollama chat: status %d: %s", resp.StatusCode, string(b))
	}
	var out struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", fmt.Errorf("ollama chat decode: %w", err)
	}
	return out.Message.Content, nil
}

// OpenAICompatClient talks to any OpenAI-compatible /v1/chat/completions endpoint.
type OpenAICompatClient struct {
	BaseURL string
	APIKey  string
	Model   string
}

// NewOpenAICompat returns an OpenAI-compatible chat client with defaults.
func NewOpenAICompat(baseURL, apiKey, model string) *OpenAICompatClient {
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}
	if model == "" {
		model = "gpt-4o-mini"
	}
	return &OpenAICompatClient{BaseURL: strings.TrimRight(baseURL, "/"), APIKey: apiKey, Model: model}
}

func (o *OpenAICompatClient) Chat(ctx context.Context, system, user string) (string, error) {
	body, err := json.Marshal(map[string]any{
		"model": o.Model,
		"messages": []map[string]string{
			{"role": "system", "content": system},
			{"role": "user", "content": user},
		},
		"response_format": map[string]string{"type": "json_object"},
	})
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, o.BaseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	if o.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+o.APIKey)
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("openai-compat chat: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return "", fmt.Errorf("openai-compat chat: status %d: %s", resp.StatusCode, string(b))
	}
	var out struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", fmt.Errorf("openai-compat chat decode: %w", err)
	}
	if len(out.Choices) == 0 {
		return "", fmt.Errorf("openai-compat chat: no choices")
	}
	return out.Choices[0].Message.Content, nil
}

// AnthropicClient talks to the Anthropic Messages API (/v1/messages). Any
// Anthropic-compatible endpoint (api.anthropic.com, Bedrock/Vertex proxies,
// self-hosted gateways) works through the same client.
type AnthropicClient struct {
	BaseURL   string
	APIKey    string
	Model     string
	MaxTokens int
}

// NewAnthropic returns an Anthropic client with defaults. A small fast model
// is chosen: topic extraction and summarization are short JSON tasks.
func NewAnthropic(baseURL, apiKey, model string) *AnthropicClient {
	if baseURL == "" {
		baseURL = "https://api.anthropic.com"
	}
	// Tolerate both "https://api.anthropic.com" and "https://api.anthropic.com/v1"
	// (the OpenAI convention users are used to) without building /v1/v1/messages.
	baseURL = strings.TrimSuffix(strings.TrimRight(baseURL, "/"), "/v1")
	if model == "" {
		model = "claude-3-5-haiku-latest"
	}
	return &AnthropicClient{
		BaseURL:   strings.TrimRight(baseURL, "/"),
		APIKey:    apiKey,
		Model:     model,
		MaxTokens: 1024,
	}
}

func (a *AnthropicClient) Chat(ctx context.Context, system, user string) (string, error) {
	body, err := json.Marshal(map[string]any{
		"model":      a.Model,
		"max_tokens": a.MaxTokens,
		"system":     system,
		"messages":   []map[string]string{{"role": "user", "content": user}},
	})
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.BaseURL+"/v1/messages", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", a.APIKey)
	req.Header.Set("anthropic-version", "2023-06-01")
	resp, err := httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("anthropic chat: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		// Anthropic errors carry a human-readable message in the body.
		var apiErr struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		msg := string(b)
		if json.Unmarshal(b, &apiErr) == nil && apiErr.Error.Message != "" {
			msg = apiErr.Error.Message
		}
		return "", fmt.Errorf("anthropic chat: status %d: %s", resp.StatusCode, msg)
	}
	var out struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", fmt.Errorf("anthropic chat decode: %w", err)
	}
	for _, block := range out.Content {
		if block.Type == "text" && strings.TrimSpace(block.Text) != "" {
			return block.Text, nil
		}
	}
	return "", fmt.Errorf("anthropic chat: no text content in response")
}

// ChatJSON asks the model to return a JSON object and decodes it into v.
// It strips markdown code fences if the model wraps the JSON.
func ChatJSON(ctx context.Context, c Client, system, user string, v any) error {
	reply, err := c.Chat(ctx, system, user)
	if err != nil {
		return err
	}
	reply = stripFences(reply)
	if err := json.Unmarshal([]byte(reply), v); err != nil {
		return fmt.Errorf("llm returned invalid JSON: %w\nreply: %s", err, reply)
	}
	return nil
}

func stripFences(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "```") {
		if i := strings.IndexByte(s, '\n'); i >= 0 {
			s = s[i+1:]
		}
		s = strings.TrimSuffix(s, "```")
	}
	return strings.TrimSpace(s)
}
