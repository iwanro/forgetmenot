package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fakeLLMServer serves an OpenAI-compatible chat endpoint that echoes a
// canned JSON reply.
func fakeLLMServer(t *testing.T, reply string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{"message": map[string]any{"content": reply}},
			},
		})
	}))
}

func TestChatJSONOpenAICompat(t *testing.T) {
	ts := fakeLLMServer(t, `{"topics":["auth","security"]}`)
	defer ts.Close()
	c := NewOpenAICompat(ts.URL+"/v1", "k", "gpt-test")
	var out struct {
		Topics []string `json:"topics"`
	}
	if err := ChatJSON(context.Background(), c, "system", "user", &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Topics) != 2 || out.Topics[0] != "auth" {
		t.Fatalf("topics = %+v", out.Topics)
	}
}

func TestChatJSONStripsFences(t *testing.T) {
	// Model wrapped JSON in a markdown fence; must still decode.
	ts := fakeLLMServer(t, "```json\n{\"topics\":[\"db\"]}\n```")
	defer ts.Close()
	c := NewOpenAICompat(ts.URL+"/v1", "k", "gpt-test")
	var out struct {
		Topics []string `json:"topics"`
	}
	if err := ChatJSON(context.Background(), c, "s", "u", &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Topics) != 1 || out.Topics[0] != "db" {
		t.Fatalf("topics = %+v", out.Topics)
	}
}

func TestChatJSONBadReply(t *testing.T) {
	ts := fakeLLMServer(t, "not json at all")
	defer ts.Close()
	c := NewOpenAICompat(ts.URL+"/v1", "k", "gpt-test")
	var out map[string]any
	if err := ChatJSON(context.Background(), c, "s", "u", &out); err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestOllamaClientPath(t *testing.T) {
	// Ollama uses /api/chat; verify the URL is built correctly by pointing at
	// a server that only answers that path.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/chat" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"message": map[string]any{"content": "{\"ok\":true}"},
		})
	}))
	defer srv.Close()
	c := NewOllama(srv.URL, "test-model")
	got, err := c.Chat(context.Background(), "s", "u")
	if err != nil {
		t.Fatal(err)
	}
	if got != "{\"ok\":true}" {
		t.Fatalf("reply = %q", got)
	}
}

// fakeAnthropicServer serves an Anthropic /v1/messages endpoint that echoes a
// canned text reply and records the request headers/body for assertions.
func fakeAnthropicServer(t *testing.T, reply string, wantAPIKey string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("x-api-key") != wantAPIKey {
			http.Error(w, `{"error":{"message":"invalid x-api-key"}}`, 401)
			return
		}
		if r.Header.Get("anthropic-version") == "" {
			http.Error(w, `{"error":{"message":"missing anthropic-version"}}`, 400)
			return
		}
		var req struct {
			Model     string `json:"model"`
			MaxTokens int    `json:"max_tokens"`
			System    string `json:"system"`
			Messages  []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad body", 400)
			return
		}
		if req.Model == "" || req.MaxTokens == 0 || req.System == "" || len(req.Messages) != 1 || req.Messages[0].Role != "user" {
			http.Error(w, `{"error":{"message":"unexpected request shape"}}`, 400)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"content": []map[string]any{{"type": "text", "text": reply}},
		})
	}))
}

func TestAnthropicClientChat(t *testing.T) {
	ts := fakeAnthropicServer(t, `{"topics":["auth","security"]}`, "sk-ant-test")
	defer ts.Close()
	c := NewAnthropic(ts.URL, "sk-ant-test", "claude-test")
	got, err := c.Chat(context.Background(), "sys", "user msg")
	if err != nil {
		t.Fatal(err)
	}
	if got != `{"topics":["auth","security"]}` {
		t.Fatalf("reply = %q", got)
	}
}

func TestChatJSONAnthropic(t *testing.T) {
	ts := fakeAnthropicServer(t, "```json\n{\"topics\":[\"auth\"]}\n```", "k")
	defer ts.Close()
	c := NewAnthropic(ts.URL, "k", "claude-test")
	var out struct {
		Topics []string `json:"topics"`
	}
	if err := ChatJSON(context.Background(), c, "system", "user", &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Topics) != 1 || out.Topics[0] != "auth" {
		t.Fatalf("topics = %+v", out.Topics)
	}
}

func TestAnthropicClientAuthError(t *testing.T) {
	ts := fakeAnthropicServer(t, "", "expected-key")
	defer ts.Close()
	c := NewAnthropic(ts.URL, "wrong-key", "claude-test")
	if _, err := c.Chat(context.Background(), "s", "u"); err == nil {
		t.Fatal("expected auth error")
	} else if !strings.Contains(err.Error(), "invalid x-api-key") {
		t.Fatalf("error should surface API message, got: %v", err)
	}
}

func TestAnthropicClientNoText(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"content": []map[string]any{{"type": "tool_use", "text": "ignored"}},
		})
	}))
	defer ts.Close()
	c := NewAnthropic(ts.URL, "k", "claude-test")
	if _, err := c.Chat(context.Background(), "s", "u"); err == nil {
		t.Fatal("expected error when no text content block is present")
	}
}
