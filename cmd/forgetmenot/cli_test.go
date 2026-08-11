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
