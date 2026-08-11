package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
