// Package memory defines the core domain model and storage interfaces for
// forgetmenot: persistent, structured, semantically searchable memory.
package memory

import (
	"context"
	"time"
)

// Type identifies the kind of a memory entry. See PRD §6.1.
type Type string

const (
	TypeFact       Type = "fact"
	TypePreference Type = "preference"
	TypeDecision   Type = "decision"
	TypeEntity     Type = "entity"
	TypeContext    Type = "context"
	TypeEpisode    Type = "episode"
)

// ValidTypes is the canonical set of memory types.
var ValidTypes = map[Type]bool{
	TypeFact:       true,
	TypePreference: true,
	TypeDecision:   true,
	TypeEntity:     true,
	TypeContext:    true,
	TypeEpisode:    true,
}

// DefaultProject is used when no explicit project namespace is given.
const DefaultProject = "global"

// Trust marks how much a memory should be trusted when its content is fed to
// an LLM. PRD M3: memories can be a prompt-injection vector, so low-trust
// content must be visibly flagged in recall and injection points.
type Trust string

const (
	TrustHigh Trust = "high" // explicit user/agent input; safe to treat as instructions-adjacent
	TrustLow  Trust = "low"  // auto-captured or external content; treat as data, not instructions
)

// MaxContentLen caps how much content a single memory can hold, both to keep
// memories focused and to bound context-injection surface.
const MaxContentLen = 4000

// Memory is a single stored memory entry. Embeddings are kept out of the
// struct on purpose: they live on the storage side and are managed by the
// core service, not exposed to tool handlers.
type Memory struct {
	ID             string            `json:"id"`
	Type           Type              `json:"type"`
	Content        string            `json:"content"`
	Project        string            `json:"project"`
	Importance     float64           `json:"importance"`
	AccessCount    int               `json:"access_count"`
	LastAccessedAt time.Time         `json:"last_accessed_at"`
	CreatedAt      time.Time         `json:"created_at"`
	UpdatedAt      time.Time         `json:"updated_at"`
	Source         string            `json:"source"`
	Trust          Trust             `json:"trust"`
	SessionID      string            `json:"session_id,omitempty"`
	Metadata       map[string]string `json:"metadata"`
}

// Session groups memories captured during one agent session, enabling
// cross-session topic correlation. PRD M4.
type Session struct {
	ID        string     `json:"id"`
	Project   string     `json:"project"`
	StartedAt time.Time  `json:"started_at"`
	EndedAt   *time.Time `json:"ended_at,omitempty"`
	Summary   string     `json:"summary,omitempty"`
}

// Topic is a subject label attached to memories, used to correlate content
// across sessions. PRD M4.
type Topic struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Project string `json:"project"`
}

// TimelineEntry is one step in a topic's evolution across sessions.
type TimelineEntry struct {
	Memory  *Memory
	Session *Session // nil if the memory is not linked to a session
}

// UpdatePatch describes the fields to change in an existing memory.
type UpdatePatch struct {
	Content    *string
	Type       *Type
	Project    *string
	Importance *float64
	Trust      *Trust
	SessionID  *string
	Metadata   map[string]string // merged into existing metadata
	// BumpAccess signals recall: access_count+1 and last_accessed_at=now.
	BumpAccess bool
}

// SearchResult pairs a memory with its similarity score.
type SearchResult struct {
	Memory *Memory
	Score  float64
	Topics []Topic
}

// RelationKind describes how two memories relate. See PRD §6.3.
type RelationKind string

const (
	// RelationRelated is a loose semantic link (same topic, complement).
	RelationRelated RelationKind = "related"
	// RelationSupersedes means FromID is replaced by ToID. When recalling,
	// superseded memories are not suggested by default.
	RelationSupersedes RelationKind = "supersedes"
	// RelationPartOf means FromID is a component of ToID (entity -> entity).
	RelationPartOf RelationKind = "part_of"
)

// Relation links two memories.
type Relation struct {
	ID        string       `json:"id"`
	FromID    string       `json:"from_id"`
	ToID      string       `json:"to_id"`
	Kind      RelationKind `json:"kind"`
	CreatedAt time.Time    `json:"created_at"`
}

// ConflictStatus tracks a contradiction between two memories.
type ConflictStatus string

const (
	ConflictOpen     ConflictStatus = "open"
	ConflictResolved ConflictStatus = "resolved"
)

// Conflict records a contradiction between two memories. The winner is set
// when the conflict is resolved.
type Conflict struct {
	ID         string         `json:"id"`
	MemoryA    string         `json:"memory_a"`
	MemoryB    string         `json:"memory_b"`
	Status     ConflictStatus `json:"status"`
	Winner     string         `json:"winner,omitempty"`
	CreatedAt  time.Time      `json:"created_at"`
	ResolvedAt *time.Time     `json:"resolved_at,omitempty"`
}

// Store is the persistence contract. The SQLite implementation lives in
// store.go; tests and future backends implement this interface.
type Store interface {
	Insert(ctx context.Context, m *Memory, embedding []float64) error
	Update(ctx context.Context, id string, patch UpdatePatch) error
	Delete(ctx context.Context, id string) error
	Get(ctx context.Context, id string) (*Memory, []float64, error)
	// All returns every memory in a project ("" = all projects) together
	// with its embedding. Used for brute-force semantic search in M0.
	All(ctx context.Context, project string) ([]*Memory, [][]float64, error)
	Count(ctx context.Context, project string) (int, error)
	CountProjects(ctx context.Context) (int, error)

	// Relations.
	AddRelation(ctx context.Context, r *Relation) error
	RelationsFrom(ctx context.Context, memoryID string) ([]Relation, error)
	// SupersededIDs returns ALL memory IDs that are superseded by something.
	// Single query; avoids N+1 lookups in recall/context paths.
	SupersededIDs(ctx context.Context) ([]string, error)

	// Conflicts.
	// CreateConflict records a contradiction. If an open conflict between the
	// two memories already exists, it returns the existing conflict's ID.
	CreateConflict(ctx context.Context, a, b string) (string, error)
	OpenConflicts(ctx context.Context) ([]Conflict, error)
	ResolveConflict(ctx context.Context, id, winner string) error

	// Sessions (PRD M4): group memories per agent session for cross-session
	// topic correlation.
	CreateSession(ctx context.Context, s *Session) error
	EndSession(ctx context.Context, id string) error
	GetSession(ctx context.Context, id string) (*Session, error)
	SessionsForProject(ctx context.Context, project string) ([]Session, error)

	// Topics (PRD M4): subject labels for cross-session correlation.
	AddTopic(ctx context.Context, t *Topic) error
	AssignTopic(ctx context.Context, memoryID, topicID string) error
	TopicsForMemory(ctx context.Context, memoryID string) ([]Topic, error)
	MemoriesByTopic(ctx context.Context, topicName, project string) ([]*Memory, error)
	// TopicsForMemories returns topic labels keyed by memory id, in one query.
	// Avoids N+1 lookups in list/timeline paths.
	TopicsForMemories(ctx context.Context, memoryIDs []string) (map[string][]Topic, error)

	Close() error
}
