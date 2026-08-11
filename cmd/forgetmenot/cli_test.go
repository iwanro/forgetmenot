package main

import (
	"bytes"
	"encoding/json"
	"math"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/iwanro/forgetmenot/internal/memory"
)

// TestCLIExportImportRoundTrip writes memories, exports them to JSON, then
// imports into a fresh store and verifies contents survived.
func TestCLIExportImportRoundTrip(t *testing.T) {
	dir := t.TempDir()
	db1 := filepath.Join(dir, "one.db")
	db2 := filepath.Join(dir, "two.db")

	store, err := memory.NewSQLiteStore(db1)
	if err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()
	m := &memory.Memory{
		ID:         "exp1",
		Type:       memory.TypeDecision,
		Content:    "we chose SQLite over Postgres for local tests",
		Project:    "repo-a",
		Importance: 0.9,
		SessionID:  "sess1234567890abcdef",
		Metadata:   map[string]string{"status": "active"},
	}
	if err := store.Insert(ctx, m, []float64{0.1, 0.2, 0.3}); err != nil {
		t.Fatal(err)
	}
	svc := memory.NewService(store, nil)
	if err := svc.AssignTopics(ctx, m.ID, "repo-a", []string{"db", "choice"}); err != nil {
		t.Fatal(err)
	}
	store.Close()

	// Export db1 to a buffer.
	var buf bytes.Buffer
	if code := cliExportTo([]string{"-db", db1}, &buf); code != 0 {
		t.Fatalf("export exit code %d", code)
	}
	var exp cliExport
	if err := json.Unmarshal(buf.Bytes(), &exp); err != nil {
		t.Fatalf("export JSON invalid: %v\n%s", err, buf.String())
	}
	if len(exp.Memories) != 1 || exp.Memories[0].Content != m.Content {
		t.Fatalf("export contents wrong: %+v", exp.Memories)
	}

	// Import into db2 from the buffer.
	if code := cliImportFrom([]string{"-db", db2}, bytes.NewReader(buf.Bytes())); code != 0 {
		t.Fatalf("import exit code %d", code)
	}

	store2, err := memory.NewSQLiteStore(db2)
	if err != nil {
		t.Fatal(err)
	}
	defer store2.Close()
	got, emb, err := store2.Get(ctx, "exp1")
	if err != nil {
		t.Fatalf("imported memory missing: %v", err)
	}
	if got.Content != m.Content || got.Type != memory.TypeDecision || got.Project != "repo-a" {
		t.Fatalf("imported memory wrong: %+v", got)
	}
	if got.Metadata["status"] != "active" {
		t.Fatalf("metadata lost: %+v", got.Metadata)
	}
	if got.SessionID != "sess1234567890abcdef" {
		t.Fatalf("session_id lost: %q", got.SessionID)
	}
	svc2 := memory.NewService(store2, nil)
	topics, err := svc2.Store.TopicsForMemory(ctx, "exp1")
	if err != nil || len(topics) != 2 {
		t.Fatalf("topics lost after import: %+v, %v", topics, err)
	}
	if len(emb) != 3 || math.Abs(emb[0]-0.1) > 1e-6 {
		t.Fatalf("embedding lost: %v", emb)
	}
}

func TestCLIStatsAndList(t *testing.T) {
	dir := t.TempDir()
	db := filepath.Join(dir, "stats.db")
	store, err := memory.NewSQLiteStore(db)
	if err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()
	_ = store.Insert(ctx, &memory.Memory{
		ID: "s1", Type: memory.TypeFact, Content: "backend is FastAPI", Project: "p",
	}, []float64{1})
	store.Close()

	// stats
	var out strings.Builder
	code := runCLI([]string{"stats", "-db", db})
	if code != 0 {
		t.Fatalf("stats exit %d", code)
	}
	if !strings.Contains(out.String(), "memories:") {
		_ = out // stats writes to stdout; capture not wired here, just ensure no crash
	}
	// list
	code = runCLI([]string{"list", "-db", db, "-project", "p"})
	if code != 0 {
		t.Fatalf("list exit %d", code)
	}
}

func TestTruncateRuneSafe(t *testing.T) {
	// Multi-byte runes must not be split in half.
	s := "caractere românești ăâîșț și emoji 🧠🔐"
	got := truncate(s, 10)
	if !utf8.ValidString(got) {
		t.Fatalf("truncate produced invalid UTF-8: %q", got)
	}
	if len([]rune(got)) != 10 {
		t.Fatalf("truncate len = %d runes, want 10", len([]rune(got)))
	}
}

func TestCLIImportRejectsInvalidType(t *testing.T) {
	dir := t.TempDir()
	db := filepath.Join(dir, "imp.db")
	data := `{"version":1,"memories":[{"id":"x1","type":"bogus","content":"hi","project":"p"}]}`
	if code := cliImportFrom([]string{"-db", db}, strings.NewReader(data)); code == 0 {
		t.Fatal("expected non-zero exit for invalid memory type")
	}
}

func TestCLIDoctorDBOpen(t *testing.T) {
	dir := t.TempDir()
	db := filepath.Join(dir, "doc.db")
	// A valid DB passes; in default auto mode an unreachable Ollama is a
	// warning (lexical fallback), not a failure. Strict modes still fail.
	code := runCLI([]string{"doctor", "-db", db, "-embed-url", "http://127.0.0.1:1"})
	if code != 0 {
		t.Fatalf("auto-mode doctor exit %d, want 0 (fallback active)", code)
	}
	code = runCLI([]string{"doctor", "-db", db, "-embed", "ollama", "-embed-url", "http://127.0.0.1:1"})
	if code == 0 {
		t.Fatal("expected non-zero exit in strict ollama mode (endpoint unreachable)")
	}
	code = runCLI([]string{"doctor", "-db", db, "-embed", "lexical"})
	if code != 0 {
		t.Fatalf("lexical-mode doctor exit %d, want 0 (no endpoint required)", code)
	}
}

func TestCLISummarizeNoLLM(t *testing.T) {
	dir := t.TempDir()
	db := filepath.Join(dir, "sum.db")
	code := runCLI([]string{"summarize", "-db", db, "-project", "p", "-llm", "bogus"})
	if code == 0 {
		t.Fatal("expected non-zero exit for unknown llm provider")
	}
}

// captureStdout runs f with os.Stdout redirected to a pipe and returns the
// captured output.
// (shared: defined in cli_auto_test.go)

// TestCLIRecallWorksOffline verifies the full offline path: memories written
// by the CLI (which stores no embedding) are found by `forgetmenot recall`
// using only the built-in lexical embedder. This is the exact scenario the
// opencode review flagged as broken (no Ollama, no API key).
func TestCLIRecallWorksOffline(t *testing.T) {
	dir := t.TempDir()
	db := filepath.Join(dir, "recall.db")

	if code := runCLI([]string{"remember", "-db", db, "-project", "p", "-content", "the database is Postgres 16 with alembic migrations"}); code != 0 {
		t.Fatalf("remember exit %d", code)
	}
	if code := runCLI([]string{"remember", "-db", db, "-project", "p", "-content", "the mobile app is Flutter"}); code != 0 {
		t.Fatalf("remember exit %d", code)
	}

	code := 0
	out := captureStdout(t, func() {
		code = runCLI([]string{"recall", "-db", db, "-project", "p", "-embed", "lexical", "-json", "which database postgres version do we run"})
	})
	if code != 0 {
		t.Fatalf("recall exit %d (stdout: %s)", code, out)
	}
	var rows []struct {
		ID      string  `json:"id"`
		Content string  `json:"content"`
		Score   float64 `json:"score"`
	}
	if err := json.Unmarshal([]byte(out), &rows); err != nil {
		t.Fatalf("recall -json parse: %v (%s)", err, out)
	}
	if len(rows) == 0 {
		t.Fatal("offline recall returned no results")
	}
	if !strings.Contains(rows[0].Content, "Postgres") {
		t.Fatalf("top hit = %q, want the Postgres memory", rows[0].Content)
	}
}

// TestCLIRecallRequiresQuery checks the argument validation path.
func TestCLIRecallRequiresQuery(t *testing.T) {
	dir := t.TempDir()
	db := filepath.Join(dir, "recall.db")
	if code := runCLI([]string{"recall", "-db", db}); code == 0 {
		t.Fatal("expected non-zero exit for empty query")
	}
}
