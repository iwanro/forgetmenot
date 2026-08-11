// Package embed: auto.go implements an embedder that transparently falls back
// to the built-in lexical provider when the configured remote (Ollama /
// OpenAI-compatible) is unreachable. This is what makes forgetmenot work in
// every agent environment out of the box: no Ollama running, no API key, no
// PATH tricks - remember/recall still function.
package embed

import (
	"context"
	"log"
	"sync"
	"time"
)

// embedder is the minimal embedding contract used here. It is structurally
// identical to memory.Embedder; kept local so this package stays dependency
// free.
type embedder interface {
	Embed(ctx context.Context, texts []string) ([][]float64, error)
}

// AutoEmbedder tries a primary provider and degrades to a fallback when the
// primary fails. After a failure it stops hammering the dead endpoint: it
// serves fallback vectors for cooldown, then probes the primary once (with a
// short timeout) and switches back the moment it answers.
type AutoEmbedder struct {
	primary   embedder
	fallback  embedder
	cooldown  time.Duration
	probeTime time.Duration

	mu        sync.Mutex
	primaryUp bool
	retryAt   time.Time
	warned    bool
}

// NewAuto returns an AutoEmbedder with a 30s cooldown and 3s probe timeout.
func NewAuto(primary, fallback embedder) *AutoEmbedder {
	return &AutoEmbedder{
		primary:   primary,
		fallback:  fallback,
		cooldown:  30 * time.Second,
		probeTime: 3 * time.Second,
		primaryUp: true,
	}
}

// Embed returns primary vectors while the primary is healthy; otherwise it
// returns fallback vectors.
func (a *AutoEmbedder) Embed(ctx context.Context, texts []string) ([][]float64, error) {
	a.mu.Lock()
	now := time.Now()
	shouldTry := a.primaryUp || now.After(a.retryAt)
	probing := !a.primaryUp
	a.mu.Unlock()
	if !shouldTry {
		return a.fallback.Embed(ctx, texts)
	}

	// Probing a previously-dead primary: cap the wait so a blackholed
	// endpoint cannot stall an agent call for the full 60s client timeout.
	if probing {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, a.probeTime)
		defer cancel()
	}

	vecs, err := a.primary.Embed(ctx, texts)
	if err == nil {
		a.mu.Lock()
		wasDown := !a.primaryUp
		a.primaryUp = true
		a.warned = false
		a.mu.Unlock()
		if wasDown {
			log.Printf("embedding provider is back online; semantic embeddings resumed")
		}
		return vecs, nil
	}

	a.mu.Lock()
	a.primaryUp = false
	a.retryAt = time.Now().Add(a.cooldown)
	shouldWarn := !a.warned
	a.warned = true
	a.mu.Unlock()
	if shouldWarn {
		log.Printf("embedding provider unavailable (%v); falling back to built-in lexical embeddings", err)
	}
	return a.fallback.Embed(ctx, texts)
}

// IsLexical reports whether the active provider is the lexical fallback. The
// memory service uses this to calibrate recall/dedupe/conflict thresholds.
func (a *AutoEmbedder) IsLexical() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return !a.primaryUp
}
