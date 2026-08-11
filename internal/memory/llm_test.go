package memory

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/iwanro/forgetmenot/internal/llm"
)

// NewOpenAICompatLLM builds a real llm client pointed at a test server.
func NewOpenAICompatLLM(t *testing.T, baseURL string) llm.Client {
	t.Helper()
	return llm.NewOpenAICompat(baseURL+"/v1", "test-key", "gpt-test")
}

// fakeLLM returns canned JSON replies for chat requests.
type fakeLLM struct {
	reply string
}

func (f fakeLLM) Chat(_ context.Context, _, _ string) (string, error) { return f.reply, nil }

func TestAutoTopics(t *testing.T) {
	svc := newTestService(t)
	svc.LLM = fakeLLM{reply: `{"topics":["auth"," Security "]}`}
	topics, err := svc.AutoTopics(context.Background(), "we chose JWT", "p")
	if err != nil {
		t.Fatal(err)
	}
	if len(topics) != 2 || topics[0] != "auth" || topics[1] != "security" {
		t.Fatalf("topics = %+v", topics)
	}
}

func TestAutoTopicsNoLLM(t *testing.T) {
	svc := newTestService(t) // LLM nil
	topics, err := svc.AutoTopics(context.Background(), "x", "p")
	if err != nil || topics != nil {
		t.Fatalf("want nil,nil without LLM, got %v, %v", topics, err)
	}
}

func TestRememberAutoTopics(t *testing.T) {
	svc := newTestService(t)
	svc.LLM = fakeLLM{reply: `{"topics":["auth"]}`}
	id, _, err := svc.Remember(context.Background(), RememberInput{
		Content:    "we chose JWT for sessions",
		Type:       TypeDecision,
		Project:    "p",
		AutoTopics: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	topics, _ := svc.Store.TopicsForMemory(context.Background(), id)
	if len(topics) != 1 || topics[0].Name != "auth" {
		t.Fatalf("auto topics not assigned: %+v", topics)
	}
}

func TestSummarizeProject(t *testing.T) {
	svc := newTestService(t)
	svc.LLM = fakeLLM{reply: `{"summary":"Old sessions covered auth and caching."}`}
	ctx := context.Background()
	// Two stale episodes: inserted directly with old timestamps so they
	// qualify as "stale" (LastAccessedAt older than the threshold).
	old := time.Now().UTC().Add(-48 * time.Hour)
	for _, c := range []string{"old auth notes", "old cache notes"} {
		m := &Memory{ID: NewID(), Type: TypeEpisode, Content: c, Project: "p",
			Importance: 0.5, AccessCount: 1, LastAccessedAt: old, CreatedAt: old,
			UpdatedAt: old, Source: "test", Trust: TrustHigh, Metadata: map[string]string{}}
		if err := svc.Store.Insert(ctx, m, nil); err != nil {
			t.Fatal(err)
		}
	}
	sum, err := svc.SummarizeProject(ctx, "p", 1*time.Hour) // everything is stale
	if err != nil {
		t.Fatal(err)
	}
	if sum != "Old sessions covered auth and caching." {
		t.Fatalf("summary = %q", sum)
	}
	// The summary must be stored as a context memory.
	n, _ := svc.Store.Count(ctx, "p")
	if n != 3 {
		t.Fatalf("count = %d, want 3 (2 episodes + 1 summary)", n)
	}
}

func TestSummarizeProjectNoLLM(t *testing.T) {
	svc := newTestService(t) // LLM nil
	if _, err := svc.SummarizeProject(context.Background(), "p", 0); err == nil {
		t.Fatal("expected error without LLM")
	}
}

// TestLLMOverHTTP verifies the service path with a real HTTP fake LLM server
// (OpenAI-compatible), exercising llm.ChatJSON end to end.
func TestLLMOverHTTP(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{"message": map[string]any{"content": `{"topics":["db"]}`}},
			},
		})
	}))
	defer srv.Close()

	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "llm.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	svc := NewService(store, fakeEmbedder{})
	svc.LLM = NewOpenAICompatLLM(t, srv.URL)

	id, _, err := svc.Remember(context.Background(), RememberInput{
		Content: "the database is Postgres 16", Type: TypeFact, Project: "p", AutoTopics: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	topics, _ := svc.Store.TopicsForMemory(context.Background(), id)
	if len(topics) != 1 || topics[0].Name != "db" {
		t.Fatalf("topics = %+v", topics)
	}
}
