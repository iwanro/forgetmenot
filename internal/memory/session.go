// Package memory: session.go implements session lifecycle and cross-session
// topic correlation (PRD M4): Timeline, current-session state file, and topic
// assignment at remember time.
package memory

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// SessionStoreFile is the default location for the current-session marker
// used by hooks (SessionStart writes it, Stop reads it via capture).
const SessionStoreFile = "current_session.json"

// CurrentSession holds the marker written by `session start` and consumed by
// `capture` / `remember` so memories attach to the right session.
type CurrentSession struct {
	ID      string `json:"id"`
	Project string `json:"project"`
}

// SessionStatePath resolves the state file path under the same base as the DB
// directory (default XDG data dir) so hooks can find it.
func SessionStatePath(dbPath string) string {
	if dbPath == "" {
		dbPath = "forgetmenot.db"
	}
	dir := filepath.Dir(dbPath)
	return filepath.Join(dir, SessionStoreFile)
}

// StartSession creates a session, persists the current-session marker, and
// returns the session.
func (s *Service) StartSession(ctx context.Context, project string) (*Session, error) {
	if project == "" {
		project = DefaultProject
	}
	sess := &Session{Project: project, StartedAt: time.Now().UTC()}
	if err := s.Store.CreateSession(ctx, sess); err != nil {
		return nil, err
	}
	if err := s.writeCurrentSession(CurrentSession{ID: sess.ID, Project: project}); err != nil {
		return nil, err
	}
	return sess, nil
}

// EndSession ends the current session (by marker) or the given id. An
// optional summary (e.g. the session capture) is stored on the session.
func (s *Service) EndSession(ctx context.Context, id, summary string) error {
	if id == "" {
		cur, err := s.readCurrentSession()
		if err != nil {
			return err
		}
		id = cur.ID
	}
	if err := s.Store.EndSessionWithSummary(ctx, id, summary); err != nil {
		return err
	}
	_ = s.clearCurrentSession()
	return nil
}

// CurrentSessionID returns the active session id, if any.
func (s *Service) CurrentSessionID() string {
	cur, err := s.readCurrentSession()
	if err != nil {
		return ""
	}
	return cur.ID
}

// Timeline returns the memories about a topic (or all project memories when
// topic is empty) across sessions, oldest first, with session context.
func (s *Service) Timeline(ctx context.Context, project, topic string, limit int) ([]TimelineEntry, error) {
	if limit <= 0 {
		limit = 50
	}
	var mems []*Memory
	var err error
	if topic != "" {
		mems, err = s.Store.MemoriesByTopic(ctx, topic, project)
	} else {
		all, _, err2 := s.Store.All(ctx, project)
		if err2 != nil {
			return nil, err2
		}
		mems = all
	}
	if err != nil {
		return nil, err
	}

	// Sort by created_at ascending so the timeline reads oldest -> newest.
	// Stable + ID tiebreaker keeps order deterministic even when two
	// memories share a timestamp.
	sort.SliceStable(mems, func(i, j int) bool {
		if !mems[i].CreatedAt.Equal(mems[j].CreatedAt) {
			return mems[i].CreatedAt.Before(mems[j].CreatedAt)
		}
		return mems[i].ID < mems[j].ID
	})

	sessions := map[string]*Session{}
	out := make([]TimelineEntry, 0, len(mems))
	for _, m := range mems {
		var sess *Session
		if m.SessionID != "" {
			if cached, ok := sessions[m.SessionID]; ok {
				sess = cached
			} else if got, err := s.Store.GetSession(ctx, m.SessionID); err == nil {
				sessions[m.SessionID] = got
				sess = got
			}
		}
		out = append(out, TimelineEntry{Memory: m, Session: sess})
		if len(out) >= limit {
			break
		}
	}
	return out, nil
}

// AssignTopics attaches topic labels to a memory. Topics come from the
// remember input; lowercased and trimmed.
func (s *Service) AssignTopics(ctx context.Context, memoryID, project string, topics []string) error {
	seen := map[string]bool{}
	for _, name := range topics {
		name = strings.ToLower(strings.TrimSpace(name))
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		t := &Topic{Name: name, Project: project}
		if err := s.Store.AddTopic(ctx, t); err != nil {
			return err
		}
		if err := s.Store.AssignTopic(ctx, memoryID, t.ID); err != nil {
			return err
		}
	}
	return nil
}

// --- state file -------------------------------------------------------------

func (s *Service) writeCurrentSession(cur CurrentSession) error {
	b, err := json.Marshal(cur)
	if err != nil {
		return err
	}
	path := SessionStatePath(s.dbPath())
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o600)
}

func (s *Service) readCurrentSession() (CurrentSession, error) {
	var cur CurrentSession
	b, err := os.ReadFile(SessionStatePath(s.dbPath()))
	if err != nil {
		return cur, err
	}
	if err := json.Unmarshal(b, &cur); err != nil {
		return cur, err
	}
	return cur, nil
}

func (s *Service) clearCurrentSession() error {
	path := SessionStatePath(s.dbPath())
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// dbPath is only used to place the session marker next to the DB. It is set
// by Service.SetDBPath (main) and defaults to "forgetmenot.db" otherwise.
func (s *Service) dbPath() string {
	if s.dbPathOverride != "" {
		return s.dbPathOverride
	}
	return "forgetmenot.db"
}
