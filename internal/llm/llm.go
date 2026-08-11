// Package llm provides a minimal chat client for LLM-powered memory features:
// auto topic extraction and project summarization. Supports Ollama and any
// OpenAI-compatible endpoint. Optional: nil client disables LLM features.
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
