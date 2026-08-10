// Package memory: automation.go implements the G7 automatic-operation
// features: project context injection (project_context), session capture
// (capture --summary) and intelligent decay (maintain). See PRD §9.1.
package memory

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
)

// ProjectContext returns a concise, ready-to-inject summary of what we know
// about a project: the most relevant memories ranked by importance, recency
// and access. Used by the SessionStart hook so an agent starts a session
// already knowing the project. PRD §9.1.
func (s *Service) ProjectContext(ctx context.Context, project string, limit int) (string, []Memory, error) {
	if project == "" {
		project = DefaultProject
	}
	if limit <= 0 {
		limit = 15
	}
	mems, _, err := s.Store.All(ctx, project)
	if err != nil {
		return "", nil, err
	}
	now := time.Now().UTC()

	// Filter out superseded memories, then rank by relevance.
	superseded := map[string]bool{}
	for _, m := range mems {
		ids, err := s.Store.Superseding(ctx, m.ID)
		if err != nil {
			return "", nil, err
		}
		for _, id := range ids {
			superseded[id] = true
		}
	}

	type scored struct {
		m    *Memory
		rank float64
	}
	var ranked []scored
	for _, m := range mems {
		if superseded[m.ID] {
			continue
		}
		ageDays := now.Sub(m.LastAccessedAt).Hours() / 24
		if ageDays < 0 {
			ageDays = 0
		}
		// Importance dominates; access gives a boost; recency decays with a
		// ~30 day half-life.
		recency := math.Exp(-ageDays / 30.0)
		rank := m.Importance * (1 + 0.5*math.Log1p(float64(m.AccessCount))) * recency
		ranked = append(ranked, scored{m: m, rank: rank})
	}
	sort.Slice(ranked, func(i, j int) bool { return ranked[i].rank > ranked[j].rank })
	if len(ranked) > limit {
		ranked = ranked[:limit]
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "# Project context: %s\n", project)
	grouped := map[Type][]*Memory{}
	var order []Type
	for _, r := range ranked {
		if _, ok := grouped[r.m.Type]; !ok {
			grouped[r.m.Type] = nil
			order = append(order, r.m.Type)
		}
		grouped[r.m.Type] = append(grouped[r.m.Type], r.m)
	}
	for _, t := range order {
		ms := grouped[t]
		fmt.Fprintf(&sb, "\n## %s (%d)\n", t, len(ms))
		for _, m := range ms {
			line := fmt.Sprintf("- %s", m.Content)
			if m.Importance > 0.8 {
				line += " (high importance)"
			}
			if m.Source != "" {
				line += fmt.Sprintf(" [%s]", m.Source)
			}
			sb.WriteString(line)
			sb.WriteString("\n")
		}
	}

	// Return the memories too, for callers that want structured access.
	out := make([]Memory, 0, len(ranked))
	for _, r := range ranked {
		out = append(out, *r.m)
	}
	return sb.String(), out, nil
}

// CaptureSummary stores a session summary as an episode memory. This is what
// the Stop/SessionEnd hook calls so that "what happened" is persisted without
// the user doing anything. If no embedder is configured (hooks run without
// Ollama), the summary is stored with an empty vector: it stays visible in
// project_context and list, just not in semantic recall. PRD §9.1.
func (s *Service) CaptureSummary(ctx context.Context, project, summary, source string) (string, error) {
	if project == "" {
		project = DefaultProject
	}
	if strings.TrimSpace(summary) == "" {
		return "", fmt.Errorf("summary is required")
	}
	if source == "" {
		source = "session"
	}

	if s.Embedder != nil {
		id, _, err := s.Remember(ctx, RememberInput{
			Content:    summary,
			Type:       TypeEpisode,
			Project:    project,
			Importance: 0.7,
			Source:     source,
			Metadata: map[string]string{
				"kind":     "session_summary",
				"captured": "auto",
			},
		})
		return id, err
	}

	// No embedder: insert directly with an empty embedding.
	m := &Memory{
		ID:             NewID(),
		Type:           TypeEpisode,
		Content:        summary,
		Project:        project,
		Importance:     0.7,
		AccessCount:    1,
		LastAccessedAt: time.Now().UTC(),
		CreatedAt:      time.Now().UTC(),
		UpdatedAt:      time.Now().UTC(),
		Source:         source,
		Metadata: map[string]string{
			"kind":     "session_summary",
			"captured": "auto",
		},
	}
	if err := s.Store.Insert(ctx, m, nil); err != nil {
		return "", err
	}
	return m.ID, nil
}

// Decay lowers the importance of stale, rarely-accessed episode/context
// memories so they stop dominating recall. It is safe to run periodically
// (maintain). Returns how many memories were touched. PRD §9.1, §10.2.
func (s *Service) Decay(ctx context.Context, olderThan time.Duration, minImportance float64) (int, error) {
	if olderThan <= 0 {
		olderThan = 14 * 24 * time.Hour
	}
	if minImportance < 0 || minImportance > 1 {
		minImportance = 0.1
	}
	mems, _, err := s.Store.All(ctx, "")
	if err != nil {
		return 0, err
	}
	now := time.Now().UTC()
	touched := 0
	for _, m := range mems {
		if m.Type != TypeEpisode && m.Type != TypeContext {
			continue
		}
		if now.Sub(m.LastAccessedAt) < olderThan {
			continue
		}
		// Halve importance per 30 days of no access, floor at minImportance.
		weeks := now.Sub(m.LastAccessedAt).Hours() / (24 * 7)
		newImp := m.Importance * math.Pow(0.8, weeks)
		if newImp < minImportance {
			newImp = minImportance
		}
		if math.Abs(newImp-m.Importance) < 0.001 {
			continue
		}
		imp := newImp
		if err := s.Store.Update(ctx, m.ID, UpdatePatch{Importance: &imp}); err != nil {
			return touched, err
		}
		touched++
	}
	return touched, nil
}
