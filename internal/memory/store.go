package memory

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	_ "modernc.org/sqlite" // pure-Go SQLite: keeps the binary static and cgo-free
)

const schema = `
CREATE TABLE IF NOT EXISTS memories (
    id               TEXT PRIMARY KEY,
    type             TEXT NOT NULL,
    content          TEXT NOT NULL,
    project          TEXT NOT NULL DEFAULT 'global',
    importance       REAL NOT NULL DEFAULT 0.5,
    access_count     INTEGER NOT NULL DEFAULT 0,
    last_accessed_at TEXT NOT NULL,
    created_at       TEXT NOT NULL,
    updated_at       TEXT NOT NULL,
    source           TEXT NOT NULL DEFAULT '',
    metadata         TEXT NOT NULL DEFAULT '{}',
    embedding        TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_memories_project ON memories(project);
CREATE INDEX IF NOT EXISTS idx_memories_type ON memories(type);

CREATE TABLE IF NOT EXISTS relations (
    id         TEXT PRIMARY KEY,
    from_id    TEXT NOT NULL,
    to_id      TEXT NOT NULL,
    kind       TEXT NOT NULL,
    created_at TEXT NOT NULL,
    FOREIGN KEY (from_id) REFERENCES memories(id) ON DELETE CASCADE,
    FOREIGN KEY (to_id) REFERENCES memories(id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_relations_from ON relations(from_id);
CREATE INDEX IF NOT EXISTS idx_relations_to ON relations(to_id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_relations_unique ON relations(from_id, to_id, kind);

CREATE TABLE IF NOT EXISTS conflicts (
    id          TEXT PRIMARY KEY,
    memory_a    TEXT NOT NULL,
    memory_b    TEXT NOT NULL,
    status      TEXT NOT NULL DEFAULT 'open',
    winner      TEXT NOT NULL DEFAULT '',
    created_at  TEXT NOT NULL,
    resolved_at TEXT,
    FOREIGN KEY (memory_a) REFERENCES memories(id) ON DELETE CASCADE,
    FOREIGN KEY (memory_b) REFERENCES memories(id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_conflicts_status ON conflicts(status);
`

// SQLiteStore is the default persistent Store, backed by a single SQLite file.
type SQLiteStore struct {
	db *sql.DB
}

// NewSQLiteStore opens (or creates) the database at path and runs migrations.
func NewSQLiteStore(path string) (*SQLiteStore, error) {
	dsn := fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)", path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	// SQLite is single-writer; a small pool avoids contention errors.
	db.SetMaxOpenConns(1)
	if _, err := db.ExecContext(context.Background(), schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return &SQLiteStore{db: db}, nil
}

// Close releases the underlying database.
func (s *SQLiteStore) Close() error { return s.db.Close() }

// NewID returns a random hex identifier (32 chars).
func NewID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(fmt.Sprintf("crypto/rand failed: %v", err)) // /dev/urandom unavailable
	}
	return hex.EncodeToString(b)
}

func encodeEmbedding(v []float64) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", fmt.Errorf("encode embedding: %w", err)
	}
	return string(b), nil
}

func decodeEmbedding(s string) ([]float64, error) {
	var v []float64
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		return nil, fmt.Errorf("decode embedding: %w", err)
	}
	return v, nil
}

func ts(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

func (s *SQLiteStore) Insert(ctx context.Context, m *Memory, embedding []float64) error {
	emb, err := encodeEmbedding(embedding)
	if err != nil {
		return err
	}
	meta, err := json.Marshal(m.Metadata)
	if err != nil {
		return fmt.Errorf("encode metadata: %w", err)
	}
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO memories (id, type, content, project, importance, access_count,
			last_accessed_at, created_at, updated_at, source, metadata, embedding)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		m.ID, string(m.Type), m.Content, m.Project, m.Importance, m.AccessCount,
		ts(m.LastAccessedAt), ts(m.CreatedAt), ts(m.UpdatedAt), m.Source,
		string(meta), emb)
	if err != nil {
		return fmt.Errorf("insert memory: %w", err)
	}
	return nil
}

func (s *SQLiteStore) Update(ctx context.Context, id string, patch UpdatePatch) error {
	// Read-modify-write keeps the schema simple for M0 and gives us access
	// counts/metadata merging for free.
	m, _, err := s.get(ctx, id)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	if patch.Content != nil {
		m.Content = *patch.Content
	}
	if patch.Type != nil {
		m.Type = *patch.Type
	}
	if patch.Project != nil {
		m.Project = *patch.Project
	}
	if patch.Importance != nil {
		m.Importance = *patch.Importance
	}
	for k, v := range patch.Metadata {
		if m.Metadata == nil {
			m.Metadata = map[string]string{}
		}
		m.Metadata[k] = v
	}
	m.UpdatedAt = now
	if patch.BumpAccess {
		m.AccessCount++
		m.LastAccessedAt = now
	}
	meta, err := json.Marshal(m.Metadata)
	if err != nil {
		return fmt.Errorf("encode metadata: %w", err)
	}
	_, err = s.db.ExecContext(ctx, `
		UPDATE memories SET type=?, content=?, project=?, importance=?,
			access_count=?, last_accessed_at=?, updated_at=?, metadata=?
		WHERE id=?`,
		string(m.Type), m.Content, m.Project, m.Importance, m.AccessCount,
		ts(m.LastAccessedAt), ts(m.UpdatedAt), string(meta), id)
	if err != nil {
		return fmt.Errorf("update memory: %w", err)
	}
	return nil
}

func (s *SQLiteStore) Delete(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM memories WHERE id=?`, id)
	if err != nil {
		return fmt.Errorf("delete memory: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *SQLiteStore) Get(ctx context.Context, id string) (*Memory, []float64, error) {
	return s.get(ctx, id)
}

func (s *SQLiteStore) get(ctx context.Context, id string) (*Memory, []float64, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, type, content, project, importance, access_count,
			last_accessed_at, created_at, updated_at, source, metadata, embedding
		FROM memories WHERE id=?`, id)
	m, emb, err := scanMemory(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, ErrNotFound
	}
	if err != nil {
		return nil, nil, fmt.Errorf("get memory: %w", err)
	}
	return m, emb, nil
}

func (s *SQLiteStore) All(ctx context.Context, project string) ([]*Memory, [][]float64, error) {
	q := `SELECT id, type, content, project, importance, access_count,
		last_accessed_at, created_at, updated_at, source, metadata, embedding
		FROM memories`
	args := []any{}
	if project != "" {
		q += ` WHERE project=?`
		args = append(args, project)
	}
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, nil, fmt.Errorf("query memories: %w", err)
	}
	defer rows.Close()

	var mems []*Memory
	var embs [][]float64
	for rows.Next() {
		m, emb, err := scanMemory(rows)
		if err != nil {
			return nil, nil, err
		}
		mems = append(mems, m)
		embs = append(embs, emb)
	}
	return mems, embs, rows.Err()
}

func (s *SQLiteStore) Count(ctx context.Context, project string) (int, error) {
	q := `SELECT COUNT(*) FROM memories`
	args := []any{}
	if project != "" {
		q += ` WHERE project=?`
		args = append(args, project)
	}
	var n int
	if err := s.db.QueryRowContext(ctx, q, args...).Scan(&n); err != nil {
		return 0, fmt.Errorf("count memories: %w", err)
	}
	return n, nil
}

func (s *SQLiteStore) CountProjects(ctx context.Context) (int, error) {
	var n int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(DISTINCT project) FROM memories`).Scan(&n); err != nil {
		return 0, fmt.Errorf("count projects: %w", err)
	}
	return n, nil
}

// --- relations --------------------------------------------------------------

func (s *SQLiteStore) AddRelation(ctx context.Context, r *Relation) error {
	if _, _, err := s.get(ctx, r.FromID); err != nil {
		return fmt.Errorf("relation source: %w", err)
	}
	if _, _, err := s.get(ctx, r.ToID); err != nil {
		return fmt.Errorf("relation target: %w", err)
	}
	if r.ID == "" {
		r.ID = NewID()
	}
	if r.Kind == "" {
		r.Kind = RelationRelated
	}
	if r.CreatedAt.IsZero() {
		r.CreatedAt = time.Now().UTC()
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO relations (id, from_id, to_id, kind, created_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(from_id, to_id, kind) DO NOTHING`,
		r.ID, r.FromID, r.ToID, string(r.Kind), ts(r.CreatedAt))
	if err != nil {
		return fmt.Errorf("insert relation: %w", err)
	}
	return nil
}

func (s *SQLiteStore) RelationsFrom(ctx context.Context, memoryID string) ([]Relation, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, from_id, to_id, kind, created_at FROM relations WHERE from_id=?`, memoryID)
	if err != nil {
		return nil, fmt.Errorf("query relations: %w", err)
	}
	defer rows.Close()
	var out []Relation
	for rows.Next() {
		var r Relation
		var kind, created string
		if err := rows.Scan(&r.ID, &r.FromID, &r.ToID, &kind, &created); err != nil {
			return nil, err
		}
		r.Kind = RelationKind(kind)
		r.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *SQLiteStore) Superseding(ctx context.Context, memoryID string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT from_id FROM relations WHERE to_id=? AND kind=?`, memoryID, string(RelationSupersedes))
	if err != nil {
		return nil, fmt.Errorf("query superseding: %w", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// --- conflicts --------------------------------------------------------------

func (s *SQLiteStore) CreateConflict(ctx context.Context, a, b string) (string, error) {
	for _, m := range [][2]string{{a, b}, {b, a}} {
		var id string
		err := s.db.QueryRowContext(ctx, `
			SELECT id FROM conflicts
			WHERE ((memory_a=? AND memory_b=?) OR (memory_a=? AND memory_b=?))
			  AND status='open'`,
			m[0], m[1], m[1], m[0]).Scan(&id)
		if err == nil {
			return id, nil // already tracked
		}
		if err != sql.ErrNoRows {
			return "", fmt.Errorf("query conflict: %w", err)
		}
	}
	c := &Conflict{
		ID:        NewID(),
		MemoryA:   a,
		MemoryB:   b,
		Status:    ConflictOpen,
		CreatedAt: time.Now().UTC(),
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO conflicts (id, memory_a, memory_b, status, winner, created_at, resolved_at)
		VALUES (?, ?, ?, ?, '', ?, NULL)`,
		c.ID, c.MemoryA, c.MemoryB, string(c.Status), ts(c.CreatedAt))
	if err != nil {
		return "", fmt.Errorf("insert conflict: %w", err)
	}
	return c.ID, nil
}

func (s *SQLiteStore) OpenConflicts(ctx context.Context) ([]Conflict, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, memory_a, memory_b, status, winner, created_at, resolved_at
		FROM conflicts WHERE status='open' ORDER BY created_at`)
	if err != nil {
		return nil, fmt.Errorf("query conflicts: %w", err)
	}
	defer rows.Close()
	var out []Conflict
	for rows.Next() {
		c, err := scanConflict(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *c)
	}
	return out, rows.Err()
}

func (s *SQLiteStore) ResolveConflict(ctx context.Context, id, winner string) error {
	// Winner must be one of the two memories in the conflict.
	var a, b string
	if err := s.db.QueryRowContext(ctx,
		`SELECT memory_a, memory_b FROM conflicts WHERE id=? AND status='open'`, id).Scan(&a, &b); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return fmt.Errorf("query conflict: %w", err)
	}
	if winner != a && winner != b {
		return fmt.Errorf("winner %q is not part of conflict %s", winner, id)
	}
	now := time.Now().UTC()
	_, err := s.db.ExecContext(ctx, `
		UPDATE conflicts SET status='resolved', winner=?, resolved_at=? WHERE id=?`,
		winner, ts(now), id)
	if err != nil {
		return fmt.Errorf("resolve conflict: %w", err)
	}
	// Record the losing memory as superseded by the winner.
	loser := a
	if winner == a {
		loser = b
	}
	if loser != winner {
		_ = s.AddRelation(ctx, &Relation{
			FromID:    loser,
			ToID:      winner,
			Kind:      RelationSupersedes,
			CreatedAt: now,
		})
	}
	return nil
}

type conflictScanner interface{ Scan(dest ...any) error }

func scanConflict(row conflictScanner) (*Conflict, error) {
	var (
		c              Conflict
		status, winner string
		created        string
		resolved       sql.NullString
	)
	if err := row.Scan(&c.ID, &c.MemoryA, &c.MemoryB, &status, &winner, &created, &resolved); err != nil {
		return nil, err
	}
	c.Status = ConflictStatus(status)
	c.Winner = winner
	c.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	if resolved.Valid {
		t, _ := time.Parse(time.RFC3339Nano, resolved.String)
		c.ResolvedAt = &t
	}
	return &c, nil
}

type scanner interface{ Scan(dest ...any) error }

func scanMemory(row scanner) (*Memory, []float64, error) {
	var (
		m                                                          Memory
		typ, project, lastAcc, created, updated, source, meta, emb string
		accessCount                                                int
	)
	if err := row.Scan(&m.ID, &typ, &m.Content, &project, &m.Importance,
		&accessCount, &lastAcc, &created, &updated, &source, &meta, &emb); err != nil {
		return nil, nil, err
	}
	m.Type = Type(typ)
	m.Project = project
	m.AccessCount = accessCount
	m.Source = source
	m.LastAccessedAt, _ = time.Parse(time.RFC3339Nano, lastAcc)
	m.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	m.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated)
	if err := json.Unmarshal([]byte(meta), &m.Metadata); err != nil {
		return nil, nil, fmt.Errorf("decode metadata: %w", err)
	}
	embedding, err := decodeEmbedding(emb)
	if err != nil {
		return nil, nil, err
	}
	return &m, embedding, nil
}

// ErrNotFound is returned when a memory ID does not exist.
var ErrNotFound = errors.New("memory not found")
