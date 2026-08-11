// Package webui serves a local browser UI and JSON API for browsing,
// editing and resolving memories, conflicts and timelines. Embedded into the
// single binary via go:embed. PRD M5.
package webui

import (
	"embed"
	"encoding/json"
	"io/fs"
	"net/http"

	"github.com/iwanro/forgetmenot/internal/memory"
)

//go:embed index.html
var assets embed.FS

// Server wraps the memory service behind an HTTP handler.
type Server struct {
	svc *memory.Service
	mux *http.ServeMux
}

// New creates the HTTP handler with all routes.
func New(svc *memory.Service) http.Handler {
	s := &Server{svc: svc, mux: http.NewServeMux()}
	s.routes()
	return s.mux
}

func (s *Server) routes() {
	s.mux.HandleFunc("GET /api/memories", s.handleMemories)
	s.mux.HandleFunc("GET /api/timeline", s.handleTimeline)
	s.mux.HandleFunc("GET /api/conflicts", s.handleConflicts)
	s.mux.HandleFunc("POST /api/conflicts/{id}/resolve", s.handleResolveConflict)
	s.mux.HandleFunc("DELETE /api/memories/{id}", s.handleDeleteMemory)
	s.mux.HandleFunc("PATCH /api/memories/{id}", s.handleUpdateMemory)
	s.mux.HandleFunc("GET /api/sessions", s.handleSessions)
	s.mux.HandleFunc("GET /api/stats", s.handleStats)

	// Serve the embedded page at /.
	page, _ := fs.Sub(assets, ".")
	s.mux.Handle("/", http.FileServer(http.FS(page)))
}

func (s *Server) writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func (s *Server) writeErr(w http.ResponseWriter, code int, msg string) {
	s.writeJSON(w, code, map[string]string{"error": msg})
}

type memoryView struct {
	ID         string            `json:"id"`
	Type       string            `json:"type"`
	Content    string            `json:"content"`
	Project    string            `json:"project"`
	Importance float64           `json:"importance"`
	Source     string            `json:"source"`
	Trust      string            `json:"trust"`
	SessionID  string            `json:"session_id"`
	Topics     []string          `json:"topics"`
	CreatedAt  string            `json:"created_at"`
	Metadata   map[string]string `json:"metadata"`
}

func (s *Server) view(m *memory.Memory, topics []memory.Topic) memoryView {
	names := make([]string, 0, len(topics))
	for _, t := range topics {
		names = append(names, t.Name)
	}
	return memoryView{
		ID:         m.ID,
		Type:       string(m.Type),
		Content:    m.Content,
		Project:    m.Project,
		Importance: m.Importance,
		Source:     m.Source,
		Trust:      string(m.Trust),
		SessionID:  m.SessionID,
		Topics:     names,
		CreatedAt:  m.CreatedAt.UTC().Format("2006-01-02T15:04:05Z"),
		Metadata:   m.Metadata,
	}
}

func (s *Server) handleMemories(w http.ResponseWriter, r *http.Request) {
	project := r.URL.Query().Get("project")
	mems, _, err := s.svc.Store.All(r.Context(), project)
	if err != nil {
		s.writeErr(w, 500, err.Error())
		return
	}
	ids := make([]string, 0, len(mems))
	for _, m := range mems {
		ids = append(ids, m.ID)
	}
	topicsByID, err := s.svc.Store.TopicsForMemories(r.Context(), ids)
	if err != nil {
		s.writeErr(w, 500, err.Error())
		return
	}
	out := make([]memoryView, 0, len(mems))
	for _, m := range mems {
		out = append(out, s.view(m, topicsByID[m.ID]))
	}
	s.writeJSON(w, 200, map[string]any{"memories": out})
}

func (s *Server) handleTimeline(w http.ResponseWriter, r *http.Request) {
	project := r.URL.Query().Get("project")
	topic := r.URL.Query().Get("topic")
	entries, err := s.svc.Timeline(r.Context(), project, topic, 200)
	if err != nil {
		s.writeErr(w, 500, err.Error())
		return
	}
	type entryView struct {
		Content   string   `json:"content"`
		Type      string   `json:"type"`
		CreatedAt string   `json:"created_at"`
		Source    string   `json:"source"`
		Trust     string   `json:"trust"`
		SessionID string   `json:"session_id"`
		Topics    []string `json:"topics,omitempty"`
	}
	ids := make([]string, 0, len(entries))
	for _, e := range entries {
		ids = append(ids, e.Memory.ID)
	}
	topicsByID, err := s.svc.Store.TopicsForMemories(r.Context(), ids)
	if err != nil {
		s.writeErr(w, 500, err.Error())
		return
	}
	out := make([]entryView, 0, len(entries))
	for _, e := range entries {
		names := make([]string, 0, len(topicsByID[e.Memory.ID]))
		for _, t := range topicsByID[e.Memory.ID] {
			names = append(names, t.Name)
		}
		out = append(out, entryView{
			Content:   e.Memory.Content,
			Type:      string(e.Memory.Type),
			CreatedAt: e.Memory.CreatedAt.UTC().Format("2006-01-02T15:04:05Z"),
			Source:    e.Memory.Source,
			Trust:     string(e.Memory.Trust),
			SessionID: e.Memory.SessionID,
			Topics:    names,
		})
	}
	s.writeJSON(w, 200, map[string]any{"entries": out})
}

func (s *Server) handleConflicts(w http.ResponseWriter, r *http.Request) {
	conflicts, err := s.svc.Conflicts(r.Context())
	if err != nil {
		s.writeErr(w, 500, err.Error())
		return
	}
	type conflictView struct {
		ID       string `json:"id"`
		MemoryA  string `json:"memory_a"`
		MemoryB  string `json:"memory_b"`
		ContentA string `json:"content_a"`
		ContentB string `json:"content_b"`
	}
	content := map[string]string{}
	if mems, _, err := s.svc.Store.All(r.Context(), ""); err == nil {
		for _, m := range mems {
			content[m.ID] = m.Content
		}
	}
	out := make([]conflictView, 0, len(conflicts))
	for _, c := range conflicts {
		out = append(out, conflictView{
			ID:       c.ID,
			MemoryA:  c.MemoryA,
			MemoryB:  c.MemoryB,
			ContentA: content[c.MemoryA],
			ContentB: content[c.MemoryB],
		})
	}
	s.writeJSON(w, 200, map[string]any{"conflicts": out})
}

func (s *Server) handleResolveConflict(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var body struct {
		Winner string `json:"winner"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Winner == "" {
		s.writeErr(w, 400, "winner is required")
		return
	}
	if err := s.svc.ResolveConflict(r.Context(), id, body.Winner); err != nil {
		if err == memory.ErrNotFound {
			s.writeErr(w, 404, err.Error())
			return
		}
		s.writeErr(w, 500, err.Error())
		return
	}
	s.writeJSON(w, 200, map[string]bool{"resolved": true})
}

func (s *Server) handleDeleteMemory(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.svc.Forget(r.Context(), id); err != nil {
		if err == memory.ErrNotFound {
			s.writeErr(w, 404, err.Error())
			return
		}
		s.writeErr(w, 500, err.Error())
		return
	}
	s.writeJSON(w, 200, map[string]bool{"deleted": true})
}

func (s *Server) handleUpdateMemory(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var body struct {
		Content *string `json:"content"`
		Trust   *string `json:"trust"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		s.writeErr(w, 400, "bad body")
		return
	}
	patch := memory.UpdatePatch{}
	if body.Content != nil {
		patch.Content = body.Content
	}
	if body.Trust != nil {
		t := memory.Trust(*body.Trust)
		patch.Trust = &t
	}
	if err := s.svc.Update(r.Context(), id, patch); err != nil {
		s.writeErr(w, 500, err.Error())
		return
	}
	s.writeJSON(w, 200, map[string]bool{"updated": true})
}

func (s *Server) handleSessions(w http.ResponseWriter, r *http.Request) {
	project := r.URL.Query().Get("project")
	if project == "" {
		project = memory.DefaultProject
	}
	sessions, err := s.svc.Store.SessionsForProject(r.Context(), project)
	if err != nil {
		s.writeErr(w, 500, err.Error())
		return
	}
	type sessionView struct {
		ID        string `json:"id"`
		Project   string `json:"project"`
		StartedAt string `json:"started_at"`
		EndedAt   string `json:"ended_at,omitempty"`
		Summary   string `json:"summary"`
	}
	out := make([]sessionView, 0, len(sessions))
	for _, sess := range sessions {
		v := sessionView{ID: sess.ID, Project: sess.Project,
			StartedAt: sess.StartedAt.UTC().Format("2006-01-02T15:04:05Z"), Summary: sess.Summary}
		if sess.EndedAt != nil {
			v.EndedAt = sess.EndedAt.UTC().Format("2006-01-02T15:04:05Z")
		}
		out = append(out, v)
	}
	s.writeJSON(w, 200, map[string]any{"sessions": out})
}

func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	stats, err := s.svc.Stats(r.Context())
	if err != nil {
		s.writeErr(w, 500, err.Error())
		return
	}
	s.writeJSON(w, 200, stats)
}
