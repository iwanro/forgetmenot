// Package memory: llm.go wires the optional LLM client to memory features:
// auto topic extraction at remember time and project summarization (memory
// compression). All features degrade gracefully when no LLM is configured.
package memory

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/iwanro/forgetmenot/internal/llm"
)

// AutoTopics extracts topic labels for a memory using the configured LLM.
// Returns nil (no error) when no LLM is configured, so callers treat it as
// "no topics auto-detected". PRD v0.3.
func (s *Service) AutoTopics(ctx context.Context, content, project string) ([]string, error) {
	if s.LLM == nil {
		return nil, nil
	}
	var out struct {
		Topics []string `json:"topics"`
	}
	system := "You extract short topic labels for a memory system. " +
		"Return JSON: {\"topics\": [\"label1\", \"label2\"]} with 1-3 lowercase labels, " +
		"max 2 words each. Use existing labels when the topic matches."
	user := fmt.Sprintf("Project: %s\nMemory: %s", project, content)
	if err := llm.ChatJSON(ctx, s.LLM, system, user, &out); err != nil {
		return nil, fmt.Errorf("auto topics: %w", err)
	}
	seen := map[string]bool{}
	var topics []string
	for _, t := range out.Topics {
		t = strings.ToLower(strings.TrimSpace(t))
		if t != "" && !seen[t] {
			seen[t] = true
			topics = append(topics, t)
		}
	}
	return topics, nil
}

// SummarizeProject compresses old episode memories into a single context
// summary using the LLM. Episodes older than olderThan that have low recent
// access are the candidates. The summary is stored as a `context` memory
// tagged with the project's topics. Returns the summary text.
func (s *Service) SummarizeProject(ctx context.Context, project string, olderThan time.Duration) (string, error) {
	if s.LLM == nil {
		return "", fmt.Errorf("no LLM configured (use -llm ollama or -llm openai)")
	}
	if olderThan <= 0 {
		olderThan = 7 * 24 * time.Hour
	}
	mems, _, err := s.Store.All(ctx, project)
	if err != nil {
		return "", err
	}
	now := time.Now().UTC()
	// Collect stale episodes + recent decisions for context.
	type note struct {
		content string
		at      time.Time
	}
	var stale []note
	decisions := []string{}
	for _, m := range mems {
		if m.Type == TypeDecision && now.Sub(m.CreatedAt) < 90*24*time.Hour {
			decisions = append(decisions, "- "+m.Content)
		}
		if m.Type == TypeEpisode && now.Sub(m.LastAccessedAt) > olderThan {
			stale = append(stale, note{content: m.Content, at: m.CreatedAt})
		}
	}
	if len(stale) == 0 {
		return "", fmt.Errorf("no stale episodes to summarize in project %q", project)
	}
	// Newest first, keep the input bounded.
	sort.SliceStable(stale, func(i, j int) bool { return stale[i].at.After(stale[j].at) })
	if len(stale) > 40 {
		stale = stale[:40]
	}
	lines := make([]string, 0, len(stale))
	for _, n := range stale {
		lines = append(lines, "- "+n.content)
	}

	var out struct {
		Summary string `json:"summary"`
	}
	system := "You compress a list of old session notes into one concise, factual summary " +
		"for an AI agent's memory. Keep concrete facts, decisions and numbers. " +
		"Return JSON: {\"summary\": \"...\"} (max 150 words, complete sentences)."
	user := fmt.Sprintf("Project: %s\n\nOld notes:\n%s\n\nRecent decisions:\n%s",
		project, strings.Join(lines, "\n"), strings.Join(decisions, "\n"))
	if err := llm.ChatJSON(ctx, s.LLM, system, user, &out); err != nil {
		return "", fmt.Errorf("summarize: %w", err)
	}
	out.Summary = strings.TrimSpace(out.Summary)
	if out.Summary == "" {
		return "", fmt.Errorf("summarize: empty summary from LLM")
	}

	// Store the summary as a context memory with a marker.
	_, _, err = s.Remember(ctx, RememberInput{
		Content:    out.Summary,
		Type:       TypeContext,
		Project:    project,
		Importance: 0.9,
		Source:     "summarize",
		Trust:      TrustHigh,
		Metadata: map[string]string{
			"kind": "project_summary",
		},
	})
	if err != nil {
		return "", err
	}
	return out.Summary, nil
}
