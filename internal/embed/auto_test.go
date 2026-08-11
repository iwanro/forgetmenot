package embed

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fakePrimary is a controllable embedding provider for AutoEmbedder tests.
type fakePrimary struct {
	mu    sync.Mutex
	down  bool
	calls int32
	vec   []float64
}

func (f *fakePrimary) Embed(_ context.Context, texts []string) ([][]float64, error) {
	atomic.AddInt32(&f.calls, 1)
	f.mu.Lock()
	down := f.down
	vec := f.vec
	f.mu.Unlock()
	if down {
		return nil, errors.New("connection refused")
	}
	out := make([][]float64, len(texts))
	for i := range out {
		out[i] = vec
	}
	return out, nil
}

func (f *fakePrimary) setDown(down bool) {
	f.mu.Lock()
	f.down = down
	f.mu.Unlock()
}

func TestAutoPrimaryHealthy(t *testing.T) {
	p := &fakePrimary{vec: []float64{1, 0}}
	a := NewAuto(p, NewLexical())
	ctx := context.Background()

	vecs, err := a.Embed(ctx, []string{"hello"})
	if err != nil {
		t.Fatal(err)
	}
	if len(vecs) != 1 || len(vecs[0]) != 2 || vecs[0][0] != 1 {
		t.Fatalf("primary vectors not used: %v", vecs)
	}
	if a.IsLexical() {
		t.Fatal("IsLexical() = true while primary is healthy")
	}
}

func TestAutoFallsBackAndCooldowns(t *testing.T) {
	p := &fakePrimary{vec: []float64{1, 0}}
	a := &AutoEmbedder{
		primary:   p,
		fallback:  NewLexical(),
		cooldown:  time.Hour, // never probe again within this test
		probeTime: time.Second,
		primaryUp: true,
	}
	ctx := context.Background()

	// First call fails -> falls back, logs, and stops calling primary.
	before := atomic.LoadInt32(&p.calls)
	p.setDown(true)
	vecs, err := a.Embed(ctx, []string{"some content here"})
	if err != nil {
		t.Fatal(err)
	}
	if len(vecs[0]) != DefaultLexicalDim {
		t.Fatalf("expected lexical fallback vector (dim %d), got %d", DefaultLexicalDim, len(vecs[0]))
	}
	if !a.IsLexical() {
		t.Fatal("IsLexical() = false after primary failure")
	}
	// Within cooldown, further calls must not touch the primary at all.
	for i := 0; i < 5; i++ {
		if _, err := a.Embed(ctx, []string{"more content"}); err != nil {
			t.Fatal(err)
		}
	}
	after := atomic.LoadInt32(&p.calls)
	if after-before != 1 {
		t.Fatalf("primary called %d times during cooldown, want exactly 1", after-before)
	}
}

func TestAutoRecoversAfterCooldown(t *testing.T) {
	p := &fakePrimary{vec: []float64{1, 0}}
	a := &AutoEmbedder{
		primary:   p,
		fallback:  NewLexical(),
		cooldown:  time.Millisecond,
		probeTime: time.Second,
		primaryUp: true,
	}
	ctx := context.Background()

	p.setDown(true)
	if _, err := a.Embed(ctx, []string{"fail"}); err != nil {
		t.Fatal(err)
	}
	if !a.IsLexical() {
		t.Fatal("expected lexical mode after failure")
	}

	// Let the cooldown pass, then bring the primary back.
	time.Sleep(2 * time.Millisecond)
	p.setDown(false)
	vecs, err := a.Embed(ctx, []string{"recover"})
	if err != nil {
		t.Fatal(err)
	}
	if len(vecs[0]) != 2 {
		t.Fatalf("primary vectors not restored after recovery: %v", vecs)
	}
	if a.IsLexical() {
		t.Fatal("IsLexical() = true after primary recovery")
	}
}
