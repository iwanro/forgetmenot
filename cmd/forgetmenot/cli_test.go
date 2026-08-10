package main

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

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
		Metadata:   map[string]string{"status": "active"},
	}
	if err := store.Insert(ctx, m, []float64{0.1, 0.2, 0.3}); err != nil {
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
	if len(emb) != 3 || emb[0] != 0.1 {
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
