package memory

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
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
    trust            TEXT NOT NULL DEFAULT 'high',
    session_id       TEXT NOT NULL DEFAULT '',
    metadata         TEXT NOT NULL DEFAULT '{}',
    embedding        TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_memories_project ON memories(project);
CREATE INDEX IF NOT EXISTS idx_memories_type ON memories(type);

CREATE TABLE IF NOT EXISTS sessions (
    id         TEXT PRIMARY KEY,
    project    TEXT NOT NULL,
    started_at TEXT NOT NULL,
    ended_at   TEXT,
    summary    TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_sessions_project ON sessions(project);

CREATE TABLE IF NOT EXISTS topics (
    id      TEXT PRIMARY KEY,
    name    TEXT NOT NULL,
    project TEXT NOT NULL,
    UNIQUE(name, project)
);
CREATE INDEX IF NOT EXISTS idx_topics_project ON topics(project);

CREATE TABLE IF NOT EXISTS memory_topics (
    memory_id TEXT NOT NULL,
    topic_id  TEXT NOT NULL,
    PRIMARY KEY (memory_id, topic_id),
    FOREIGN KEY (memory_id) REFERENCES memories(id) ON DELETE CASCADE,
    FOREIGN KEY (topic_id) REFERENCES topics(id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_memory_topics_topic ON memory_topics(topic_id);

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
	if err := migrateColumns(db); err != nil {
		db.Close()
		return nil, err
	}
	// Indexes on migrated columns must be created after the ALTERs above.
	if _, err := db.ExecContext(context.Background(),
		`CREATE INDEX IF NOT EXISTS idx_memories_session ON memories(session_id)`); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: session index: %w", err)
	}
	return &SQLiteStore{db: db}, nil
}

// migrateColumns adds columns introduced after the initial schema to existing
// databases. CREATE TABLE IF NOT EXISTS does not alter existing tables, so we
// check PRAGMA table_info and ALTER TABLE when a column is missing.
func migrateColumns(db *sql.DB) error {
	ctx := context.Background()
	have := map[string]bool{}
	rows, err := db.QueryContext(ctx, `PRAGMA table_info(memories)`)
	if err != nil {
		return fmt.Errorf("migrate: pragma: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var (
			cid       int
			name, typ string
			notNull   int
			dflt      any
			pk        int
		)
		if err := rows.Scan(&cid, &name, &typ, &notNull, &dflt, &pk); err != nil {
			return fmt.Errorf("migrate: pragma scan: %w", err)
		}
		have[name] = true
	}
	if err := rows.Err(); err != nil {
		return err
	}
	additions := []struct {
		col, ddl string
	}{
		{"trust", `ALTER TABLE memories ADD COLUMN trust TEXT NOT NULL DEFAULT 'high'`},
		{"session_id", `ALTER TABLE memories ADD COLUMN session_id TEXT NOT NULL DEFAULT ''`},
	}
	for _, a := range additions {
		if have[a.col] {
			continue
		}
		if _, err := db.ExecContext(ctx, a.ddl); err != nil {
			return fmt.Errorf("migrate: add %s column: %w", a.col, err)
		}
	}
	return nil
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

func encodeEmbedding(v []float64) ([]byte, error) {
	if len(v) == 0 {
		// Column is NOT NULL; store a 1-byte empty marker instead of NULL.
		return []byte{0x00}, nil
	}
	// Binary float32 BLOB: 4 bytes per value. Compact and fast to decode.
	// A leading magic byte disambiguates from legacy JSON text.
	buf := make([]byte, 1+4*len(v))
	buf[0] = 0x01
	for i, x := range v {
		binary.LittleEndian.PutUint32(buf[1+4*i:], math.Float32bits(float32(x)))
	}
	return buf, nil
}

func decodeEmbedding(b []byte) ([]float64, error) {
	if len(b) == 0 || (len(b) == 1 && b[0] == 0x00) {
		return nil, nil
	}
	if b[0] == 0x01 {
		if (len(b)-1)%4 != 0 {
			return nil, fmt.Errorf("decode embedding: bad binary length %d", len(b))
		}
		out := make([]float64, (len(b)-1)/4)
		for i := range out {
			out[i] = float64(math.Float32frombits(binary.LittleEndian.Uint32(b[1+4*i:])))
		}
		return out, nil
	}
	// Legacy JSON text from M0-M2 databases.
	var v []float64
	if err := json.Unmarshal(b, &v); err != nil {
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
			last_accessed_at, created_at, updated_at, source, trust, session_id, metadata, embedding)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		m.ID, string(m.Type), m.Content, m.Project, m.Importance, m.AccessCount,
		ts(m.LastAccessedAt), ts(m.CreatedAt), ts(m.UpdatedAt), m.Source,
		string(m.Trust), m.SessionID, string(meta), emb)
	if err != nil {
		return fmt.Errorf("insert memory: %w", err)
	}
	return nil
}

func (s *SQLiteStore) Update(ctx context.Context, id string, patch UpdatePatch) error {
	// Fast path: pure access bump is a single atomic UPDATE, avoiding lost
	// updates when recalls run concurrently.
	if patch.BumpAccess && patch.Content == nil && patch.Type == nil &&
		patch.Project == nil && patch.Importance == nil && patch.Trust == nil &&
		patch.SessionID == nil && len(patch.Metadata) == 0 {
		now := time.Now().UTC()
		res, err := s.db.ExecContext(ctx, `
			UPDATE memories SET access_count = access_count + 1,
				last_accessed_at = ?, updated_at = ? WHERE id=?`,
			ts(now), ts(now), id)
		if err != nil {
			return fmt.Errorf("bump access: %w", err)
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return ErrNotFound
		}
		return nil
	}

	// Read-modify-write keeps metadata merging simple; a single writer
	// connection makes this safe in practice for non-bump patches.
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
	if patch.Trust != nil {
		m.Trust = *patch.Trust
	}
	if patch.SessionID != nil {
		m.SessionID = *patch.SessionID
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
		UPDATE memories SET type=?, content=?, project=?, importance=?, trust=?,
			session_id=?, access_count=?, last_accessed_at=?, updated_at=?, metadata=?
		WHERE id=?`,
		string(m.Type), m.Content, m.Project, m.Importance, string(m.Trust),
		m.SessionID, m.AccessCount, ts(m.LastAccessedAt), ts(m.UpdatedAt), string(meta), id)
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
			last_accessed_at, created_at, updated_at, source, trust, session_id, metadata, embedding
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
		last_accessed_at, created_at, updated_at, source, trust, session_id, metadata, embedding
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

func (s *SQLiteStore) SupersededIDs(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT from_id FROM relations WHERE kind=?`, string(RelationSupersedes))
	if err != nil {
		return nil, fmt.Errorf("query superseded: %w", err)
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

// --- sessions --------------------------------------------------------------

func (s *SQLiteStore) CreateSession(ctx context.Context, sess *Session) error {
	if sess.ID == "" {
		sess.ID = NewID()
	}
	if sess.StartedAt.IsZero() {
		sess.StartedAt = time.Now().UTC()
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO sessions (id, project, started_at, ended_at, summary)
		VALUES (?, ?, ?, NULL, ?)`,
		sess.ID, sess.Project, ts(sess.StartedAt), sess.Summary)
	if err != nil {
		return fmt.Errorf("insert session: %w", err)
	}
	return nil
}

func (s *SQLiteStore) EndSession(ctx context.Context, id string) error {
	now := time.Now().UTC()
	res, err := s.db.ExecContext(ctx, `UPDATE sessions SET ended_at=? WHERE id=? AND ended_at IS NULL`, ts(now), id)
	if err != nil {
		return fmt.Errorf("end session: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *SQLiteStore) GetSession(ctx context.Context, id string) (*Session, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, project, started_at, ended_at, summary FROM sessions WHERE id=?`, id)
	sess, err := scanSession(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get session: %w", err)
	}
	return sess, nil
}

func (s *SQLiteStore) SessionsForProject(ctx context.Context, project string) ([]Session, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, project, started_at, ended_at, summary FROM sessions
		WHERE project=? ORDER BY started_at DESC`, project)
	if err != nil {
		return nil, fmt.Errorf("query sessions: %w", err)
	}
	defer rows.Close()
	var out []Session
	for rows.Next() {
		sess, err := scanSession(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *sess)
	}
	return out, rows.Err()
}

type sessionScanner interface{ Scan(dest ...any) error }

func scanSession(row sessionScanner) (*Session, error) {
	var (
		sess      Session
		started   string
		endedNull sql.NullString
		summ      string
	)
	if err := row.Scan(&sess.ID, &sess.Project, &started, &endedNull, &summ); err != nil {
		return nil, err
	}
	sess.StartedAt, _ = time.Parse(time.RFC3339Nano, started)
	if endedNull.Valid {
		t, _ := time.Parse(time.RFC3339Nano, endedNull.String)
		sess.EndedAt = &t
	}
	sess.Summary = summ
	return &sess, nil
}

// --- topics ----------------------------------------------------------------

func (s *SQLiteStore) AddTopic(ctx context.Context, t *Topic) error {
	if t.ID == "" {
		t.ID = NewID()
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO topics (id, name, project) VALUES (?, ?, ?)
		ON CONFLICT(name, project) DO NOTHING`, t.ID, t.Name, t.Project)
	if err != nil {
		return fmt.Errorf("insert topic: %w", err)
	}
	// Return the canonical id (ours, or the existing row's).
	err = s.db.QueryRowContext(ctx,
		`SELECT id FROM topics WHERE name=? AND project=?`, t.Name, t.Project).Scan(&t.ID)
	if err != nil {
		return fmt.Errorf("topic lookup: %w", err)
	}
	return nil
}

func (s *SQLiteStore) AssignTopic(ctx context.Context, memoryID, topicID string) error {
	if _, _, err := s.get(ctx, memoryID); err != nil {
		return fmt.Errorf("topic memory: %w", err)
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO memory_topics (memory_id, topic_id) VALUES (?, ?)
		ON CONFLICT(memory_id, topic_id) DO NOTHING`, memoryID, topicID)
	if err != nil {
		return fmt.Errorf("assign topic: %w", err)
	}
	return nil
}

func (s *SQLiteStore) TopicsForMemory(ctx context.Context, memoryID string) ([]Topic, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT t.id, t.name, t.project FROM topics t
		JOIN memory_topics mt ON mt.topic_id = t.id
		WHERE mt.memory_id = ? ORDER BY t.name`, memoryID)
	if err != nil {
		return nil, fmt.Errorf("query topics: %w", err)
	}
	defer rows.Close()
	var out []Topic
	for rows.Next() {
		var t Topic
		if err := rows.Scan(&t.ID, &t.Name, &t.Project); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *SQLiteStore) MemoriesByTopic(ctx context.Context, topicName, project string) ([]*Memory, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT m.id, m.type, m.content, m.project, m.importance, m.access_count,
			m.last_accessed_at, m.created_at, m.updated_at, m.source, m.trust,
			m.session_id, m.metadata, m.embedding
		FROM memories m
		JOIN memory_topics mt ON mt.memory_id = m.id
		JOIN topics t ON t.id = mt.topic_id
		WHERE t.name = ? AND t.project = ?
		ORDER BY m.created_at`, topicName, project)
	if err != nil {
		return nil, fmt.Errorf("query by topic: %w", err)
	}
	defer rows.Close()
	var out []*Memory
	for rows.Next() {
		m, _, err := scanMemory(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

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
		m                                                                       Memory
		typ, project, lastAcc, created, updated, source, trust, sessionID, meta string
		emb                                                                     []byte
		accessCount                                                             int
	)
	if err := row.Scan(&m.ID, &typ, &m.Content, &project, &m.Importance,
		&accessCount, &lastAcc, &created, &updated, &source, &trust, &sessionID, &meta, &emb); err != nil {
		return nil, nil, err
	}
	m.Type = Type(typ)
	m.Project = project
	m.AccessCount = accessCount
	m.Source = source
	m.Trust = Trust(trust)
	if m.Trust == "" {
		m.Trust = TrustHigh // pre-M3 rows default to high trust
	}
	m.SessionID = sessionID
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
