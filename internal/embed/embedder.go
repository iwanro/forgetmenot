// Package embed provides embedding providers: a local Ollama embedder and an
// OpenAI-compatible remote embedder used as a fallback. The MCP server picks
// one via flags/env (default: Ollama at http://localhost:11434).
package embed

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Client is the shared HTTP client for all providers.
var Client = &http.Client{Timeout: 60 * time.Second}

// OllamaEmbedder calls the local Ollama /api/embed endpoint.
type OllamaEmbedder struct {
	BaseURL string // e.g. http://localhost:11434
	Model   string // e.g. nomic-embed-text
}

// NewOllama returns an OllamaEmbedder with defaults.
func NewOllama(baseURL, model string) *OllamaEmbedder {
	if baseURL == "" {
		baseURL = "http://localhost:11434"
	}
	if model == "" {
		model = "nomic-embed-text"
	}
	return &OllamaEmbedder{BaseURL: baseURL, Model: model}
}

func (o *OllamaEmbedder) Embed(ctx context.Context, texts []string) ([][]float64, error) {
	body, err := json.Marshal(map[string]any{"model": o.Model, "input": texts})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, o.BaseURL+"/api/embed", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := Client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ollama embed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("ollama embed: status %d: %s", resp.StatusCode, string(b))
	}
	var out struct {
		Embeddings [][]float64 `json:"embeddings"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("ollama embed decode: %w", err)
	}
	if len(out.Embeddings) != len(texts) {
		return nil, fmt.Errorf("ollama embed: got %d embeddings for %d texts", len(out.Embeddings), len(texts))
	}
	return out.Embeddings, nil
}

// OpenAICompatEmbedder calls any OpenAI-compatible /v1/embeddings endpoint.
type OpenAICompatEmbedder struct {
	BaseURL string // e.g. https://api.openai.com/v1
	APIKey  string
	Model   string
}

// NewOpenAICompat returns an OpenAICompatEmbedder with defaults.
func NewOpenAICompat(baseURL, apiKey, model string) *OpenAICompatEmbedder {
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}
	if model == "" {
		model = "text-embedding-3-small"
	}
	return &OpenAICompatEmbedder{BaseURL: baseURL, APIKey: apiKey, Model: model}
}

func (o *OpenAICompatEmbedder) Embed(ctx context.Context, texts []string) ([][]float64, error) {
	body, err := json.Marshal(map[string]any{"model": o.Model, "input": texts})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, o.BaseURL+"/embeddings", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if o.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+o.APIKey)
	}
	resp, err := Client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("openai-compat embed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("openai-compat embed: status %d: %s", resp.StatusCode, string(b))
	}
	var out struct {
		Data []struct {
			Embedding []float64 `json:"embedding"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("openai-compat embed decode: %w", err)
	}
	if len(out.Data) != len(texts) {
		return nil, fmt.Errorf("openai-compat embed: got %d embeddings for %d texts", len(out.Data), len(texts))
	}
	res := make([][]float64, 0, len(out.Data))
	for _, d := range out.Data {
		res = append(res, d.Embedding)
	}
	return res, nil
}
