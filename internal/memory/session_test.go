package memory

import (
	"context"
	"math"
	"path/filepath"
	"testing"
)

func TestSessionLifecycle(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	sess := &Session{Project: "p"}
	if err := s.CreateSession(ctx, sess); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetSession(ctx, sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Project != "p" || got.StartedAt.IsZero() {
		t.Fatalf("session wrong: %+v", got)
	}
	if got.EndedAt != nil {
		t.Fatal("session should not be ended yet")
	}
	if err := s.EndSession(ctx, sess.ID); err != nil {
		t.Fatal(err)
	}
	got2, _ := s.GetSession(ctx, sess.ID)
	if got2.EndedAt == nil {
		t.Fatal("session should be ended")
	}
	sessions, err := s.SessionsForProject(ctx, "p")
	if err != nil || len(sessions) != 1 {
		t.Fatalf("sessions = %d, %v", len(sessions), err)
	}
}

func TestTopicsAssignAndQuery(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	// Two memories across two "sessions" about the same topic.
	m1 := &Memory{ID: "m1", Type: TypeDecision, Content: "chose JWT for sessions", Project: "p"}
	m2 := &Memory{ID: "m2", Type: TypeFact, Content: "JWT refresh tokens rotate 15m", Project: "p"}
	if err := s.Insert(ctx, m1, []float64{1}); err != nil {
		t.Fatal(err)
	}
	if err := s.Insert(ctx, m2, []float64{1}); err != nil {
		t.Fatal(err)
	}
	tp := &Topic{Name: "auth", Project: "p"}
	if err := s.AddTopic(ctx, tp); err != nil {
		t.Fatal(err)
	}
	// Same topic name again must return the same id (dedupe).
	tp2 := &Topic{Name: "auth", Project: "p"}
	if err := s.AddTopic(ctx, tp2); err != nil {
		t.Fatal(err)
	}
	if tp.ID != tp2.ID {
		t.Fatalf("topic dedupe broken: %s vs %s", tp.ID, tp2.ID)
	}
	if err := s.AssignTopic(ctx, "m1", tp.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.AssignTopic(ctx, "m2", tp.ID); err != nil {
		t.Fatal(err)
	}
	topics, err := s.TopicsForMemory(ctx, "m1")
	if err != nil || len(topics) != 1 || topics[0].Name != "auth" {
		t.Fatalf("topics for m1 = %+v, %v", topics, err)
	}
	mems, err := s.MemoriesByTopic(ctx, "auth", "p")
	if err != nil || len(mems) != 2 {
		t.Fatalf("memories by topic = %d, %v", len(mems), err)
	}
}

func TestServiceTimeline(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()
	// Assign topics via service.Remember with a fake embedder.
	_, _, _ = svc.Remember(ctx, RememberInput{
		Content: "we chose JWT for sessions", Type: TypeDecision, Project: "p",
		Topics: []string{"auth"},
	})
	_, _, _ = svc.Remember(ctx, RememberInput{
		Content: "the database is Postgres 16", Type: TypeFact, Project: "p",
		Topics: []string{"auth"},
	})
	entries, err := svc.Timeline(ctx, "p", "auth", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("timeline = %d entries, want 2", len(entries))
	}
	// Oldest first.
	if entries[0].Memory.Content != "we chose JWT for sessions" {
		t.Fatalf("timeline order wrong: %q first", entries[0].Memory.Content)
	}
}

func TestServiceTimelineAll(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()
	_, _, _ = svc.Remember(ctx, RememberInput{Content: "backend is FastAPI on Python 3.12", Type: TypeFact, Project: "p"})
	_, _, _ = svc.Remember(ctx, RememberInput{Content: "cache layer is Redis", Type: TypeFact, Project: "p"})
	entries, err := svc.Timeline(ctx, "p", "", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("timeline(all) = %d, want 2", len(entries))
	}
}

func TestBinaryEmbeddingRoundtrip(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	m := &Memory{ID: "emb1", Type: TypeFact, Content: "x", Project: "p"}
	orig := []float64{0.1, 0.5, -1.0, 2.25}
	if err := s.Insert(ctx, m, orig); err != nil {
		t.Fatal(err)
	}
	_, got, err := s.Get(ctx, "emb1")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(orig) {
		t.Fatalf("len = %d, want %d", len(got), len(orig))
	}
	for i := range orig {
		if math.Abs(got[i]-orig[i]) > 1e-6 {
			t.Fatalf("embedding[%d] = %v, want %v", i, got[i], orig[i])
		}
	}
}

func TestServiceSessionAttachment(t *testing.T) {
	dir := t.TempDir()
	store, err := NewSQLiteStore(filepath.Join(dir, "s.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	svc := NewService(store, fakeEmbedder{})
	svc.SetDBPath(filepath.Join(dir, "s.db"))
	ctx := context.Background()

	sess, err := svc.StartSession(ctx, "p")
	if err != nil {
		t.Fatal(err)
	}
	id, _, _ := svc.Remember(ctx, RememberInput{
		Content: "backend is FastAPI on Python 3.12", Type: TypeFact, Project: "p",
	})
	m, _, _ := svc.Store.Get(ctx, id)
	if m.SessionID != sess.ID {
		t.Fatalf("session not attached: got %q want %q", m.SessionID, sess.ID)
	}
	if err := svc.EndSession(ctx, ""); err != nil {
		t.Fatal(err)
	}
}
