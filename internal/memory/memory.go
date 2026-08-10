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
	Metadata       map[string]string `json:"metadata"`
}

// UpdatePatch describes the fields to change in an existing memory.
type UpdatePatch struct {
	Content    *string
	Type       *Type
	Project    *string
	Importance *float64
	Metadata   map[string]string // merged into existing metadata
	// BumpAccess signals recall: access_count+1 and last_accessed_at=now.
	BumpAccess bool
}

// SearchResult pairs a memory with its similarity score.
type SearchResult struct {
	Memory *Memory
	Score  float64
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
	Close() error
}
