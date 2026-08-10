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
