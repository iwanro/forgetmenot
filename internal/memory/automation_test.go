package memory

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestProjectContextRanksAndGroups(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()
	// a: [1,0,0], b: [0.4,1,0], c: [0,0,1]
	_, _, _ = svc.Remember(ctx, RememberInput{
		Content: "backend is FastAPI on Python 3.12", Type: TypeFact, Project: "p",
	})
	_, _, _ = svc.Remember(ctx, RememberInput{
		Content: "the database is Postgres 16", Type: TypeDecision, Project: "p",
	})
	_, _, _ = svc.Remember(ctx, RememberInput{
		Content: "cache layer is Redis", Type: TypeFact, Project: "other",
	})

	text, mems, err := svc.ProjectContext(ctx, "p", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(mems) != 2 {
		t.Fatalf("project_context returned %d memories, want 2", len(mems))
	}
	if !strings.Contains(text, "# Project context: p") {
		t.Fatalf("missing header in context:\n%s", text)
	}
	if !strings.Contains(text, "## fact") || !strings.Contains(text, "## decision") {
		t.Fatalf("missing type groups in context:\n%s", text)
	}
	// "other" project must not leak in.
	if strings.Contains(text, "Redis") {
		t.Fatalf("other project leaked into context:\n%s", text)
	}
}

func TestProjectContextExcludesSuperseded(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()
	a, _, _ := svc.Remember(ctx, RememberInput{
		Content: "backend is FastAPI on Python 3.12", Type: TypeFact, Project: "p",
	})
	b, _, _ := svc.Remember(ctx, RememberInput{
		Content: "the database is Postgres 16", Type: TypeFact, Project: "p",
	})
	if err := svc.Link(ctx, a, b, string(RelationSupersedes)); err != nil {
		t.Fatal(err)
	}
	_, mems, err := svc.ProjectContext(ctx, "p", 10)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range mems {
		if m.ID == a {
			t.Fatal("superseded memory appears in project context")
		}
	}
}

func TestCaptureSummaryStoresEpisode(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()
	id, err := svc.CaptureSummary(ctx, "p", "Refactored auth module; decided on JWT for sessions.", "claude-code")
	if err != nil {
		t.Fatal(err)
	}
	if id == "" {
		t.Fatal("expected an id")
	}
	m, _, err := svc.Store.Get(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if m.Type != TypeEpisode {
		t.Fatalf("type = %s, want episode", m.Type)
	}
	if m.Metadata["kind"] != "session_summary" || m.Metadata["captured"] != "auto" {
		t.Fatalf("metadata missing: %+v", m.Metadata)
	}
	if m.Source != "claude-code" {
		t.Fatalf("source = %q", m.Source)
	}
}

func TestCaptureSummaryRequiresContent(t *testing.T) {
	svc := newTestService(t)
	if _, err := svc.CaptureSummary(context.Background(), "p", "   ", "x"); err == nil {
		t.Fatal("expected error for empty summary")
	}
}

func TestDecayLowersStaleEpisode(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()
	id, _, _ := svc.Remember(ctx, RememberInput{
		Content: "old session notes that nobody reads", Type: TypeEpisode, Project: "p",
	})
	// Force the memory to look stale: bump LastAccessedAt back 60 days.
	old := time.Now().UTC().Add(-60 * 24 * time.Hour)
	// Direct store update with a custom patch via SQLite internals is awkward;
	// instead insert with explicit timestamps.
	if err := svc.Store.Delete(ctx, id); err != nil {
		t.Fatal(err)
	}
	imp := 0.8
	m := &Memory{
		ID:             id,
		Type:           TypeEpisode,
		Content:        "old session notes that nobody reads",
		Project:        "p",
		Importance:     imp,
		AccessCount:    1,
		LastAccessedAt: old,
		CreatedAt:      old,
		UpdatedAt:      old,
		Metadata:       map[string]string{},
	}
	if err := svc.Store.Insert(ctx, m, []float64{0, 0, 1}); err != nil {
		t.Fatal(err)
	}

	touched, err := svc.Decay(ctx, 7*24*time.Hour, 0.1)
	if err != nil {
		t.Fatal(err)
	}
	if touched != 1 {
		t.Fatalf("decay touched %d, want 1", touched)
	}
	got, _, _ := svc.Store.Get(ctx, id)
	if got.Importance >= 0.8 {
		t.Fatalf("importance not lowered: %v", got.Importance)
	}
	if got.Importance < 0.1 {
		t.Fatalf("importance below floor: %v", got.Importance)
	}
}

func TestDecaySkipsFreshAndFacts(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()
	_, _, _ = svc.Remember(ctx, RememberInput{
		Content: "backend is FastAPI on Python 3.12", Type: TypeFact, Project: "p",
	})
	_, _, _ = svc.Remember(ctx, RememberInput{
		Content: "fresh episode notes", Type: TypeEpisode, Project: "p",
	})
	touched, err := svc.Decay(ctx, 7*24*time.Hour, 0.1)
	if err != nil {
		t.Fatal(err)
	}
	if touched != 0 {
		t.Fatalf("decay touched %d fresh/fact memories, want 0", touched)
	}
}
