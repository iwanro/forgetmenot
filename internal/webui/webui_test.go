package webui

import (
	"bytes"
	"context"
	"encoding/json"
	"hash/fnv"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/iwanro/forgetmenot/internal/memory"
)

// fakeEmbedder keeps tests hermetic (deterministic, distinct vectors per text).
type fakeEmbedder struct{}

func (fakeEmbedder) Embed(_ context.Context, texts []string) ([][]float64, error) {
	out := make([][]float64, 0, len(texts))
	for _, t := range texts {
		h := fnv.New64a()
		h.Write([]byte(t))
		seed := h.Sum64()
		v := make([]float64, 8)
		for j := range v {
			v[j] = float64((seed>>(j*8))&0xff) / 255
		}
		out = append(out, v)
	}
	return out, nil
}

func newTestServer(t *testing.T) (*httptest.Server, *memory.Service) {
	t.Helper()
	store, err := memory.NewSQLiteStore(filepath.Join(t.TempDir(), "web.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	svc := memory.NewService(store, fakeEmbedder{})
	ts := httptest.NewServer(New(svc))
	t.Cleanup(ts.Close)
	return ts, svc
}

func seed(t *testing.T, svc *memory.Service) {
	t.Helper()
	ctx := context.Background()
	_, _, _ = svc.Remember(ctx, memory.RememberInput{
		Content: "backend is FastAPI on Python 3.12", Type: memory.TypeFact, Project: "demo",
		Topics: []string{"backend"},
	})
	_, _, _ = svc.Remember(ctx, memory.RememberInput{
		Content: "we chose JWT for sessions", Type: memory.TypeDecision, Project: "demo",
		Topics: []string{"auth"},
	})
	_, _, _ = svc.Remember(ctx, memory.RememberInput{
		Content: "untrusted doc", Type: memory.TypeFact, Project: "demo", Trust: memory.TrustLow,
	})
}

func TestWebServesPage(t *testing.T) {
	ts, _ := newTestServer(t)
	resp, err := http.Get(ts.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	buf := new(bytes.Buffer)
	buf.ReadFrom(resp.Body)
	if !strings.Contains(buf.String(), "forgetmenot") {
		t.Fatalf("page missing title: %s", buf.String()[:100])
	}
}

func TestWebMemories(t *testing.T) {
	ts, svc := newTestServer(t)
	seed(t, svc)
	resp, err := http.Get(ts.URL + "/api/memories?project=demo")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out struct {
		Memories []memoryView `json:"memories"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if len(out.Memories) != 3 {
		t.Fatalf("memories = %d, want 3", len(out.Memories))
	}
	// Topics and trust must be present.
	found := false
	for _, m := range out.Memories {
		if m.Content == "backend is FastAPI on Python 3.12" {
			found = true
			if len(m.Topics) != 1 || m.Topics[0] != "backend" {
				t.Fatalf("topics missing: %+v", m.Topics)
			}
		}
		if m.Content == "untrusted doc" && m.Trust != "low" {
			t.Fatalf("trust = %q, want low", m.Trust)
		}
	}
	if !found {
		t.Fatal("seeded memory missing")
	}
}

func TestWebTimelineByTopic(t *testing.T) {
	ts, svc := newTestServer(t)
	seed(t, svc)
	resp, err := http.Get(ts.URL + "/api/timeline?project=demo&topic=auth")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out struct {
		Entries []struct {
			Content string `json:"content"`
		} `json:"entries"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if len(out.Entries) != 1 || out.Entries[0].Content != "we chose JWT for sessions" {
		t.Fatalf("timeline wrong: %+v", out.Entries)
	}
}

func TestWebDeleteMemory(t *testing.T) {
	ts, svc := newTestServer(t)
	ctx := context.Background()
	id, _, _ := svc.Remember(ctx, memory.RememberInput{
		Content: "to delete", Type: memory.TypeFact, Project: "p",
	})
	req, _ := http.NewRequest(http.MethodDelete, ts.URL+"/api/memories/"+id, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("delete status %d", resp.StatusCode)
	}
	if _, _, err := svc.Store.Get(ctx, id); err == nil {
		t.Fatal("memory not deleted")
	}
}

func TestWebResolveConflict(t *testing.T) {
	ts, svc := newTestServer(t)
	ctx := context.Background()
	a, _, _ := svc.Remember(ctx, memory.RememberInput{
		Content: "backend is FastAPI on Python 3.12", Type: memory.TypeFact, Project: "p",
	})
	svc.ConflictThreshold = 0.30
	b, _, _ := svc.Remember(ctx, memory.RememberInput{
		Content: "the database is Postgres 16", Type: memory.TypeFact, Project: "p",
	})
	conflicts, _ := svc.Conflicts(ctx)
	if len(conflicts) != 1 {
		t.Fatalf("conflicts = %d, want 1", len(conflicts))
	}
	body, _ := json.Marshal(map[string]string{"winner": b})
	resp, err := http.Post(ts.URL+"/api/conflicts/"+conflicts[0].ID+"/resolve",
		"application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("resolve status %d", resp.StatusCode)
	}
	open, _ := svc.Conflicts(ctx)
	if len(open) != 0 {
		t.Fatalf("open conflicts after resolve = %d", len(open))
	}
	_ = a
}
