package embed

import (
	"context"
	"math"
	"testing"

	"github.com/iwanro/forgetmenot/internal/memory"
)

func sim(a, b []float64) float64 { return memory.Cosine(a, b) }

func TestLexicalDeterministic(t *testing.T) {
	l := NewLexical()
	ctx := context.Background()
	a, err := l.Embed(ctx, []string{"the backend is FastAPI on Python 3.12"})
	if err != nil {
		t.Fatal(err)
	}
	b, err := l.Embed(ctx, []string{"the backend is FastAPI on Python 3.12"})
	if err != nil {
		t.Fatal(err)
	}
	if len(a[0]) != DefaultLexicalDim {
		t.Fatalf("dim = %d, want %d", len(a[0]), DefaultLexicalDim)
	}
	for i := range a[0] {
		if a[0][i] != b[0][i] {
			t.Fatalf("nondeterministic at index %d: %v vs %v", i, a[0][i], b[0][i])
		}
	}
}

func TestLexicalExactDuplicateIsIdentity(t *testing.T) {
	l := NewLexical()
	ctx := context.Background()
	a, _ := l.Embed(ctx, []string{"we chose JWT for authentication"})
	b, _ := l.Embed(ctx, []string{"we chose JWT for authentication"})
	if s := sim(a[0], b[0]); math.Abs(s-1) > 1e-9 {
		t.Fatalf("exact duplicate cosine = %f, want 1", s)
	}
}

func TestLexicalRelatedBeatsUnrelated(t *testing.T) {
	l := NewLexical()
	ctx := context.Background()
	fact, _ := l.Embed(ctx, []string{"the database is Postgres 16 with alembic migrations"})
	related, _ := l.Embed(ctx, []string{"which database postgres version do we run"})
	unrelated, _ := l.Embed(ctx, []string{"the mobile app uses Flutter for the UI"})

	simRel := sim(fact[0], related[0])
	simUnrel := sim(fact[0], unrelated[0])
	if simRel <= 0.2 {
		t.Fatalf("related cosine = %f, want > 0.2", simRel)
	}
	if simUnrel >= simRel/2 {
		t.Fatalf("unrelated cosine = %f, want < %f", simUnrel, simRel/2)
	}
}

func TestLexicalStopwordsDoNotInflate(t *testing.T) {
	l := NewLexical()
	ctx := context.Background()
	a, _ := l.Embed(ctx, []string{"the and of to for with"}) // stopwords only
	b, _ := l.Embed(ctx, []string{"completely different content jwt auth"})
	// All-stopword text has no features and must not be similar to anything.
	if s := sim(a[0], b[0]); s != 0 {
		t.Fatalf("stopword-only vector cosine = %f, want 0", s)
	}
}

func TestLexicalEmptyAndNonASCII(t *testing.T) {
	l := NewLexical()
	ctx := context.Background()
	vecs, err := l.Embed(ctx, []string{"", "memorie despre autentificare JWT", "🧠 12:30 termen limita"})
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range vecs {
		if len(v) != DefaultLexicalDim {
			t.Fatalf("dim = %d, want %d", len(v), DefaultLexicalDim)
		}
	}
}

func TestLexicalIsLexical(t *testing.T) {
	if !NewLexical().IsLexical() {
		t.Fatal("LexicalEmbedder must report IsLexical()=true")
	}
}
