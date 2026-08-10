// Package memory: service.go implements the core memory operations used by
// the MCP layer: Remember (with dedupe), Recall (semantic search), Forget,
// Update and Stats. All policy lives here, not in the MCP handlers.
package memory

import (
	"context"
	"fmt"
	"math"
	"sort"
	"time"
)

// Embedder turns text into embedding vectors. Implementations: OllamaEmbedder
// (local) and OpenAICompatEmbedder (remote fallback) in internal/embed.
type Embedder interface {
	Embed(ctx context.Context, texts []string) ([][]float64, error)
}

// Service is the core memory engine. It owns the store and the embedder and
// exposes the operations the MCP tools call.
type Service struct {
	Store    Store
	Embedder Embedder

	// DedupeThreshold: two memories with cosine similarity >= this value in
	// the same project are considered duplicates; the new write reinforces
	// the existing entry instead of inserting a copy. PRD §7.3.
	DedupeThreshold float64
	// MinRecallScore: recall results below this similarity are dropped.
	MinRecallScore float64
}

// NewService returns a Service with sensible defaults.
func NewService(store Store, emb Embedder) *Service {
	return &Service{
		Store:           store,
		Embedder:        emb,
		DedupeThreshold: 0.92,
		MinRecallScore:  0.30,
	}
}

// RememberInput is the validated request for storing a memory.
type RememberInput struct {
	Content    string
	Type       Type
	Project    string
	Importance float64
	Source     string
	Metadata   map[string]string
}

// Remember stores a memory. If a near-duplicate already exists in the same
// project, the existing entry's access is bumped instead (reinforcement).
// Returns the ID that ended up holding the memory and whether it was a new
// insert or a reinforcement of an existing entry.
func (s *Service) Remember(ctx context.Context, in RememberInput) (string, bool, error) {
	if in.Project == "" {
		in.Project = DefaultProject
	}
	if !ValidTypes[in.Type] {
		return "", false, fmt.Errorf("invalid memory type %q", in.Type)
	}
	if in.Content == "" {
		return "", false, fmt.Errorf("content is required")
	}
	vec, err := s.embedOne(ctx, in.Content)
	if err != nil {
		return "", false, fmt.Errorf("embed content: %w", err)
	}

	// Dedupe against existing memories in the same project.
	existing, embs, err := s.Store.All(ctx, in.Project)
	if err != nil {
		return "", false, err
	}
	for i, m := range existing {
		if m.Type != in.Type {
			continue
		}
		if sim := Cosine(vec, embs[i]); sim >= s.DedupeThreshold {
			// Reinforce: bump access + refresh timestamp, keep the original.
			if err := s.Store.Update(ctx, m.ID, UpdatePatch{BumpAccess: true}); err != nil {
				return "", false, err
			}
			return m.ID, false, nil
		}
	}

	m := &Memory{
		ID:             NewID(),
		Type:           in.Type,
		Content:        in.Content,
		Project:        in.Project,
		Importance:     in.Importance,
		AccessCount:    1,
		LastAccessedAt: time.Now().UTC(),
		CreatedAt:      time.Now().UTC(),
		UpdatedAt:      time.Now().UTC(),
		Source:         in.Source,
		Metadata:       in.Metadata,
	}
	if m.Importance <= 0 {
		m.Importance = 0.5
	}
	if m.Metadata == nil {
		m.Metadata = map[string]string{}
	}
	if err := s.Store.Insert(ctx, m, vec); err != nil {
		return "", false, err
	}
	return m.ID, true, nil
}

// RecallInput is the validated request for semantic search.
type RecallInput struct {
	Query   string
	Project string // "" = search everything
	Type    Type   // "" = any type
	Limit   int
}

// Recall finds the top-K memories most similar to query, filtered by project
// and type, sorted by score descending. Brute-force for M0 (fine up to tens
// of thousands of memories); the vector index arrives in M1.
func (s *Service) Recall(ctx context.Context, in RecallInput) ([]SearchResult, error) {
	if in.Query == "" {
		return nil, fmt.Errorf("query is required")
	}
	if in.Limit <= 0 {
		in.Limit = 10
	}
	qvec, err := s.embedOne(ctx, in.Query)
	if err != nil {
		return nil, fmt.Errorf("embed query: %w", err)
	}

	mems, embs, err := s.Store.All(ctx, in.Project)
	if err != nil {
		return nil, err
	}
	results := make([]SearchResult, 0, len(mems))
	for i, m := range mems {
		if in.Type != "" && m.Type != in.Type {
			continue
		}
		score := Cosine(qvec, embs[i])
		if score < s.MinRecallScore {
			continue
		}
		results = append(results, SearchResult{Memory: m, Score: score})
	}
	sort.Slice(results, func(i, j int) bool { return results[i].Score > results[j].Score })
	if len(results) > in.Limit {
		results = results[:in.Limit]
	}

	// Reinforce whatever was actually retrieved.
	for _, r := range results {
		_ = s.Store.Update(ctx, r.Memory.ID, UpdatePatch{BumpAccess: true})
	}
	return results, nil
}

// Forget deletes a memory by ID.
func (s *Service) Forget(ctx context.Context, id string) error { return s.Store.Delete(ctx, id) }

// Update applies a patch to a memory by ID.
func (s *Service) Update(ctx context.Context, id string, patch UpdatePatch) error {
	return s.Store.Update(ctx, id, patch)
}

// Stats returns simple health metrics for the memory.
type Stats struct {
	Count        int `json:"count"`
	ProjectCount int `json:"project_count"`
}

func (s *Service) Stats(ctx context.Context) (Stats, error) {
	total, err := s.Store.Count(ctx, "")
	if err != nil {
		return Stats{}, err
	}
	return Stats{Count: total, ProjectCount: total}, nil
}

func (s *Service) embedOne(ctx context.Context, text string) ([]float64, error) {
	vecs, err := s.Embedder.Embed(ctx, []string{text})
	if err != nil {
		return nil, err
	}
	if len(vecs) != 1 {
		return nil, fmt.Errorf("expected 1 embedding, got %d", len(vecs))
	}
	return vecs[0], nil
}

// Cosine returns cosine similarity between two vectors. Zero-length vectors
// score 0. Vectors may be unnormalized.
func Cosine(a, b []float64) float64 {
	if len(a) == 0 || len(b) == 0 || len(a) != len(b) {
		return 0
	}
	var dot, na, nb float64
	for i := range a {
		dot += a[i] * b[i]
		na += a[i] * a[i]
		nb += b[i] * b[i]
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}
