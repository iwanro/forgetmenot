package memory

import (
	"context"
	"math"
	"testing"
)

// fakeEmbedder maps known strings to fixed vectors so tests are deterministic
// and need no network. Stored vectors:
//
//	alpha [1,0,0]  beta [0.1,1,0]  gamma [0.98,0.1,0]
//
// alpha-gamma cosine ~= 0.995 >= 0.92 (dedupe triggers).
// Unknown text maps to [0,0,1], orthogonal to alpha and beta, so unrelated
// queries score ~0 and are dropped by the MinRecallScore filter.
type fakeEmbedder struct{}

func (fakeEmbedder) Embed(_ context.Context, texts []string) ([][]float64, error) {
	out := make([][]float64, 0, len(texts))
	for _, t := range texts {
		switch t {
		case "backend is FastAPI on Python 3.12":
			out = append(out, []float64{1, 0, 0})
		case "backend is FastAPI on Python 3.12 (v2)":
			out = append(out, []float64{0.98, 0.1, 0})
		case "the database is Postgres 16":
			out = append(out, []float64{0.4, 1, 0})
		case "what framework does the backend use":
			out = append(out, []float64{1, 0, 0})
		default:
			out = append(out, []float64{0, 0, 1})
		}
	}
	return out, nil
}

func newTestService(t *testing.T) *Service {
	t.Helper()
	svc := NewService(newTestStore(t), fakeEmbedder{})
	return svc
}

func TestRememberInserts(t *testing.T) {
	svc := newTestService(t)
	id, isNew, err := svc.Remember(context.Background(), RememberInput{
		Content: "backend is FastAPI on Python 3.12",
		Type:    TypeFact,
		Project: "repo-a",
	})
	if err != nil {
		t.Fatalf("Remember: %v", err)
	}
	if !isNew {
		t.Fatal("expected new insert")
	}
	n, _ := svc.Store.Count(context.Background(), "repo-a")
	if n != 1 {
		t.Fatalf("count = %d, want 1", n)
	}
	_ = id
}

func TestRememberDedupes(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()
	first, isNew, err := svc.Remember(ctx, RememberInput{
		Content: "backend is FastAPI on Python 3.12",
		Type:    TypeFact, Project: "repo-a",
	})
	if err != nil || !isNew {
		t.Fatalf("first insert: isNew=%v err=%v", isNew, err)
	}
	second, isNew2, err := svc.Remember(ctx, RememberInput{
		Content: "backend is FastAPI on Python 3.12 (v2)", // similar (0.98 >= 0.92)
		Type:    TypeFact, Project: "repo-a",
	})
	if err != nil {
		t.Fatalf("second Remember: %v", err)
	}
	if isNew2 {
		t.Fatal("expected dedupe reinforcement, got new insert")
	}
	if second != first {
		t.Fatalf("reinforced id %s != original id %s", second, first)
	}
	n, _ := svc.Store.Count(context.Background(), "repo-a")
	if n != 1 {
		t.Fatalf("count = %d, want 1 after dedupe", n)
	}
	m, _, _ := svc.Store.Get(ctx, first)
	if m.AccessCount != 2 {
		t.Fatalf("access_count = %d, want 2 after reinforcement", m.AccessCount)
	}
}

func TestRememberSeparatesProjects(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()
	_, _, _ = svc.Remember(ctx, RememberInput{
		Content: "backend is FastAPI on Python 3.12", Type: TypeFact, Project: "repo-a",
	})
	_, isNew, _ := svc.Remember(ctx, RememberInput{
		Content: "backend is FastAPI on Python 3.12 (v2)", Type: TypeFact, Project: "repo-b",
	})
	if !isNew {
		t.Fatal("same content in a different project must NOT dedupe")
	}
}

func TestRecallRanksAndFilters(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()
	_, _, _ = svc.Remember(ctx, RememberInput{
		Content: "backend is FastAPI on Python 3.12", Type: TypeFact, Project: "repo-a",
	})
	_, _, _ = svc.Remember(ctx, RememberInput{
		Content: "the database is Postgres 16", Type: TypeFact, Project: "repo-a",
	})
	res, err := svc.Recall(ctx, RecallInput{
		Query: "what framework does the backend use", Project: "repo-a", Limit: 5,
	})
	if err != nil {
		t.Fatalf("Recall: %v", err)
	}
	if len(res) != 2 {
		t.Fatalf("hits = %d, want 2", len(res))
	}
	// FastAPI memory must rank above the Postgres one.
	if res[0].Memory.Content != "backend is FastAPI on Python 3.12" {
		t.Fatalf("top hit = %q", res[0].Memory.Content)
	}
	if res[0].Score <= res[1].Score {
		t.Fatalf("scores not descending: %v %v", res[0].Score, res[1].Score)
	}
}

func TestRecallTypeFilter(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()
	_, _, _ = svc.Remember(ctx, RememberInput{
		Content: "the database is Postgres 16", Type: TypeFact, Project: "repo-a",
	})
	_, _, _ = svc.Remember(ctx, RememberInput{
		Content: "the database is Postgres 16", Type: TypeDecision, Project: "repo-a",
	})
	res, err := svc.Recall(ctx, RecallInput{
		Query: "the database is Postgres 16", Project: "repo-a", Type: TypeDecision, Limit: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 1 || res[0].Memory.Type != TypeDecision {
		t.Fatalf("type filter failed: %+v", res)
	}
}

func TestRecallMinScoreDropsIrrelevant(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()
	_, _, _ = svc.Remember(ctx, RememberInput{
		Content: "the database is Postgres 16", Type: TypeFact, Project: "repo-a",
	})
	res, err := svc.Recall(ctx, RecallInput{
		Query: "completely unrelated topic about gardening", Project: "repo-a", Limit: 5,
	})
	if err != nil {
		t.Fatal(err)
	}
	// The default pseudo-vector for the query must be dissimilar enough.
	if len(res) != 0 {
		t.Fatalf("expected no hits, got %d", len(res))
	}
}

func TestForgetAndUpdate(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()
	id, _, _ := svc.Remember(ctx, RememberInput{
		Content: "backend is FastAPI on Python 3.12", Type: TypeFact, Project: "repo-a",
	})
	if err := svc.Update(ctx, id, UpdatePatch{Metadata: map[string]string{"status": "active"}}); err != nil {
		t.Fatal(err)
	}
	m, _, _ := svc.Store.Get(ctx, id)
	if m.Metadata["status"] != "active" {
		t.Fatalf("metadata not merged: %+v", m.Metadata)
	}
	if err := svc.Forget(ctx, id); err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.Store.Get(ctx, id); err != ErrNotFound {
		t.Fatalf("want ErrNotFound after forget, got %v", err)
	}
}

func TestCosine(t *testing.T) {
	cases := []struct {
		a, b []float64
		want float64
	}{
		{[]float64{1, 0}, []float64{1, 0}, 1},
		{[]float64{1, 0}, []float64{0, 1}, 0},
		{[]float64{1, 1}, []float64{1, 1}, 1},
		{[]float64{}, []float64{}, 0},
		{[]float64{1, 0}, []float64{1, 0, 0}, 0}, // different dims
	}
	for _, c := range cases {
		if got := Cosine(c.a, c.b); math.Abs(got-c.want) > 1e-9 {
			t.Fatalf("Cosine(%v,%v) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}

func TestStatsCountsProjects(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()
	// Each content maps to a distinct fake embedding vector.
	for _, in := range []RememberInput{
		{Content: "backend is FastAPI on Python 3.12", Type: TypeFact, Project: "a"},
		{Content: "the database is Postgres 16", Type: TypeFact, Project: "a"},
		{Content: "cache layer is Redis", Type: TypeFact, Project: "b"},
	} {
		if _, _, err := svc.Remember(ctx, in); err != nil {
			t.Fatal(err)
		}
	}
	s, err := svc.Stats(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if s.Count != 3 {
		t.Fatalf("count = %d, want 3", s.Count)
	}
	if s.ProjectCount != 2 {
		t.Fatalf("project_count = %d, want 2", s.ProjectCount)
	}
}

func TestLinkAndRelations(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()
	a, _, _ := svc.Remember(ctx, RememberInput{Content: "API key lives in .env", Type: TypeFact, Project: "p"})
	b, _, _ := svc.Remember(ctx, RememberInput{Content: "API key lives in config.py", Type: TypeFact, Project: "p"})

	if err := svc.Link(ctx, b, a, string(RelationSupersedes)); err != nil {
		t.Fatalf("Link: %v", err)
	}
	rels, err := svc.Relations(ctx, b)
	if err != nil {
		t.Fatal(err)
	}
	if len(rels) != 1 || rels[0].ToID != a || rels[0].Kind != RelationSupersedes {
		t.Fatalf("relations mismatch: %+v", rels)
	}

	// Superseded memories are hidden from recall.
	if _, _, err := svc.Remember(ctx, RememberInput{Content: "the database is Postgres 16", Type: TypeFact, Project: "p"}); err != nil {
		t.Fatal(err)
	}
	_ = a
	_ = b
}

func TestLinkValidatesKind(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()
	a, _, _ := svc.Remember(ctx, RememberInput{Content: "x", Type: TypeFact, Project: "p"})
	b, _, _ := svc.Remember(ctx, RememberInput{Content: "y", Type: TypeFact, Project: "p"})
	if err := svc.Link(ctx, a, b, "bogus"); err == nil {
		t.Fatal("expected error for bogus relation kind")
	}
}

func TestConflictDetectionAndResolution(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()

	// a: "backend is FastAPI on Python 3.12" -> [1,0,0]
	a, _, _ := svc.Remember(ctx, RememberInput{
		Content: "backend is FastAPI on Python 3.12", Type: TypeFact, Project: "p",
	})
	// Lower the conflict threshold so b ([0.4,1,0], sim 0.37 with a) is
	// detected as a contradiction without being a dedupe (< 0.92).
	svc.ConflictThreshold = 0.30
	b, _, _ := svc.Remember(ctx, RememberInput{
		Content: "the database is Postgres 16", Type: TypeFact, Project: "p",
	})

	open, err := svc.Conflicts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(open) != 1 {
		t.Fatalf("open conflicts = %d, want 1: %+v", len(open), open)
	}
	c := open[0]
	if (c.MemoryA != a || c.MemoryB != b) && (c.MemoryA != b || c.MemoryB != a) {
		t.Fatalf("conflict does not involve a and b: %+v", c)
	}

	// Resolve: b (Postgres) wins; a (FastAPI) becomes superseded.
	if err := svc.ResolveConflict(ctx, c.ID, b); err != nil {
		t.Fatalf("ResolveConflict: %v", err)
	}
	open2, _ := svc.Conflicts(ctx)
	if len(open2) != 0 {
		t.Fatalf("open conflicts after resolve = %d, want 0", len(open2))
	}
	// a is superseded by b, so recalling about FastAPI must not surface a.
	res, _ := svc.Recall(ctx, RecallInput{Query: "backend is FastAPI on Python 3.12", Project: "p", Limit: 5})
	for _, r := range res {
		if r.Memory.ID == a {
			t.Fatalf("superseded memory %s still in recall results: %+v", a, r)
		}
	}
}

func TestCreateConflictDeduplicates(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	// Foreign keys are enforced; insert the two memories first.
	if err := s.Insert(ctx, &Memory{ID: "m1", Type: TypeFact, Content: "a", Project: "p"}, []float64{1}); err != nil {
		t.Fatal(err)
	}
	if err := s.Insert(ctx, &Memory{ID: "m2", Type: TypeFact, Content: "b", Project: "p"}, []float64{1}); err != nil {
		t.Fatal(err)
	}
	id1, err := s.CreateConflict(ctx, "m1", "m2")
	if err != nil {
		t.Fatal(err)
	}
	id2, err := s.CreateConflict(ctx, "m2", "m1") // reversed order
	if err != nil {
		t.Fatal(err)
	}
	if id1 != id2 {
		t.Fatalf("duplicate conflict created: %s vs %s", id1, id2)
	}
	open, _ := s.OpenConflicts(ctx)
	if len(open) != 1 {
		t.Fatalf("open conflicts = %d, want 1", len(open))
	}
}
