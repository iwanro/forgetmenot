// Package memory: service.go implements the core memory operations used by
// the MCP layer: Remember (with dedupe), Recall (semantic search), Forget,
// Update and Stats. All policy lives here, not in the MCP handlers.
package memory

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
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
	// ConflictThreshold: a new memory whose similarity to an existing one of
	// the same type/project falls in [ConflictThreshold, DedupeThreshold) is
	// treated as a possible contradiction and opens a conflict. PRD §7.3.
	ConflictThreshold float64
}

// NewService returns a Service with sensible defaults.
func NewService(store Store, emb Embedder) *Service {
	return &Service{
		Store:             store,
		Embedder:          emb,
		DedupeThreshold:   0.92,
		MinRecallScore:    0.30,
		ConflictThreshold: 0.60,
	}
}

// RememberInput is the validated request for storing a memory.
type RememberInput struct {
	Content    string
	Type       Type
	Project    string
	Importance float64
	Source     string
	Trust      Trust // defaults to TrustHigh
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
	in.Content = sanitizeContent(in.Content)
	if in.Content == "" {
		return "", false, fmt.Errorf("content is required")
	}
	if in.Importance < 0 || in.Importance > 1 {
		return "", false, fmt.Errorf("importance must be in [0,1], got %v", in.Importance)
	}
	if in.Trust == "" {
		in.Trust = TrustHigh
	}
	if in.Trust != TrustHigh && in.Trust != TrustLow {
		return "", false, fmt.Errorf("invalid trust level %q", in.Trust)
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

	// Conflict detection: similar but not identical content in the same
	// project/type may contradict the existing memory. Done AFTER insert so
	// the foreign keys resolve. PRD §7.3, §10.2.
	conflictIDs := []string{}
	for i, m := range existing {
		if m.Type != in.Type {
			continue
		}
		if sim := Cosine(vec, embs[i]); sim >= s.ConflictThreshold {
			conflictIDs = append(conflictIDs, m.ID)
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
		Trust:          in.Trust,
		Metadata:       in.Metadata,
	}
	if m.Importance == 0 {
		m.Importance = 0.5
	}
	if m.Metadata == nil {
		m.Metadata = map[string]string{}
	}
	if err := s.Store.Insert(ctx, m, vec); err != nil {
		return "", false, err
	}
	for _, other := range conflictIDs {
		_, _ = s.Store.CreateConflict(ctx, other, m.ID)
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
	// Collect superseded memory IDs to hide them from results (PRD §6.3).
	superseded := map[string]bool{}
	ids, err := s.Store.SupersededIDs(ctx)
	if err != nil {
		return nil, err
	}
	for _, id := range ids {
		superseded[id] = true
	}
	results := make([]SearchResult, 0, len(mems))
	for i, m := range mems {
		if in.Type != "" && m.Type != in.Type {
			continue
		}
		if superseded[m.ID] {
			continue // superseded memories are not suggested by default
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
	if patch.Type != nil && !ValidTypes[*patch.Type] {
		return fmt.Errorf("invalid memory type %q", *patch.Type)
	}
	if patch.Trust != nil && *patch.Trust != TrustHigh && *patch.Trust != TrustLow {
		return fmt.Errorf("invalid trust level %q", *patch.Trust)
	}
	if patch.Content != nil {
		c := sanitizeContent(*patch.Content)
		if c == "" {
			return fmt.Errorf("content is required")
		}
		patch.Content = &c
	}
	return s.Store.Update(ctx, id, patch)
}

// Link creates a relation between two memories. If a matching relation
// already exists, it is a no-op.
func (s *Service) Link(ctx context.Context, fromID, toID, kind string) error {
	k := RelationKind(kind)
	switch k {
	case RelationRelated, RelationSupersedes, RelationPartOf:
	case "":
		k = RelationRelated
	default:
		return fmt.Errorf("invalid relation kind %q", kind)
	}
	return s.Store.AddRelation(ctx, &Relation{FromID: fromID, ToID: toID, Kind: k})
}

// Relations returns all relations originating from a memory.
func (s *Service) Relations(ctx context.Context, memoryID string) ([]Relation, error) {
	return s.Store.RelationsFrom(ctx, memoryID)
}

// Conflicts lists all open conflicts.
func (s *Service) Conflicts(ctx context.Context) ([]Conflict, error) {
	return s.Store.OpenConflicts(ctx)
}

// ResolveConflict marks a conflict resolved with the given winner, and records
// the losing memory as superseded by the winner.
func (s *Service) ResolveConflict(ctx context.Context, conflictID, winnerID string) error {
	return s.Store.ResolveConflict(ctx, conflictID, winnerID)
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
	projects, err := s.Store.CountProjects(ctx)
	if err != nil {
		return Stats{}, err
	}
	return Stats{Count: total, ProjectCount: projects}, nil
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

// sanitizeContent strips control characters (keeping \n and \t), collapses
// stray null bytes, and caps length. This limits both context-injection
// surface and junk bytes in embeddings.
func sanitizeContent(s string) string {
	if len(s) > MaxContentLen {
		s = s[:MaxContentLen]
	}
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch r {
		case '\n', '\t':
			b.WriteRune(r)
		default:
			if r < 0x20 || r == 0x7f {
				continue // drop control chars
			}
			b.WriteRune(r)
		}
	}
	return strings.TrimSpace(b.String())
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
