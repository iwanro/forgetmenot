package eval

import (
	"context"
	"hash/fnv"
	"math"
	"path/filepath"
	"strings"
	"testing"

	"github.com/iwanro/forgetmenot/internal/memory"
)

// bowEmbedder is a deterministic bag-of-words embedder (64 dims) so the eval
// harness runs offline and hermetically in tests.
type bowEmbedder struct{}

func (bowEmbedder) Embed(_ context.Context, texts []string) ([][]float64, error) {
	out := make([][]float64, 0, len(texts))
	for _, t := range texts {
		v := make([]float64, 64)
		for _, w := range strings.Fields(strings.ToLower(t)) {
			h := fnv.New32a()
			h.Write([]byte(w))
			v[h.Sum32()%64]++
		}
		var norm float64
		for _, x := range v {
			norm += x * x
		}
		norm = math.Sqrt(norm)
		if norm > 0 {
			for i := range v {
				v[i] /= norm
			}
		}
		out = append(out, v)
	}
	return out, nil
}

func TestEvalHarnessScores(t *testing.T) {
	store, err := memory.NewSQLiteStore(filepath.Join(t.TempDir(), "eval.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	svc := memory.NewService(store, bowEmbedder{})
	ctx := context.Background()

	n, err := SeedDataset(ctx, svc)
	if err != nil {
		t.Fatal(err)
	}
	if n != 20 {
		t.Fatalf("seeded %d facts, want 20", n)
	}

	res := Run(ctx, svc, DefaultDataset)
	t.Logf("eval result:\n%s", res.String())

	if res.Total != 20 {
		t.Fatalf("total = %d, want 20", res.Total)
	}
	// With a bag-of-words embedder the exact-word matches should mostly land
	// in the top-3. Require a high bar so regressions are visible.
	if res.RecallAtK < 0.85 {
		t.Fatalf("recall@k = %.2f, want >= 0.85\n%s", res.RecallAtK, res.String())
	}
}

func TestSeedDatasetIdempotent(t *testing.T) {
	store, err := memory.NewSQLiteStore(filepath.Join(t.TempDir(), "eval2.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	svc := memory.NewService(store, bowEmbedder{})
	ctx := context.Background()

	n1, _ := SeedDataset(ctx, svc)
	n2, err := SeedDataset(ctx, svc)
	if err != nil {
		t.Fatal(err)
	}
	if n1 != 20 || n2 != 0 {
		t.Fatalf("seed idempotency broken: n1=%d n2=%d", n1, n2)
	}
}
