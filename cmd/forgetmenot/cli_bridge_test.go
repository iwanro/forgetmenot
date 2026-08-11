package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/iwanro/forgetmenot/internal/memory"
)

func newTestSQLiteStore(path string) (*memory.SQLiteStore, error) {
	return memory.NewSQLiteStore(path)
}

func TestReplaceMarkedSectionAppends(t *testing.T) {
	path := filepath.Join(t.TempDir(), "CLAUDE.md")
	block := ctxOpen + "\ncontext\n" + ctxClose
	if err := replaceMarkedSection(path, ctxOpen, ctxClose, block); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	if !strings.Contains(string(b), "context") {
		t.Fatalf("block not appended:\n%s", b)
	}
}

func TestReplaceMarkedSectionReplaces(t *testing.T) {
	path := filepath.Join(t.TempDir(), "CLAUDE.md")
	_ = os.WriteFile(path, []byte("old stuff\n"+ctxOpen+"\nstale context\n"+ctxClose+"\nkeep me"), 0o644)
	block := ctxOpen + "\nfresh context\n" + ctxClose
	if err := replaceMarkedSection(path, ctxOpen, ctxClose, block); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	s := string(b)
	if strings.Contains(s, "stale context") {
		t.Fatalf("stale content not replaced:\n%s", s)
	}
	if !strings.Contains(s, "fresh context") {
		t.Fatalf("fresh content missing:\n%s", s)
	}
	if !strings.Contains(s, "keep me") {
		t.Fatalf("unrelated content lost:\n%s", s)
	}
}

func TestExtractMarkedSection(t *testing.T) {
	text := "x\n" + fctOpen + "\n- fact one\n- fact two\n" + fctClose + "\ny"
	got, ok := extractMarkedSection(text, fctOpen, fctClose)
	if !ok {
		t.Fatal("section not found")
	}
	if !strings.Contains(got, "fact one") || !strings.Contains(got, "fact two") {
		t.Fatalf("section content wrong: %q", got)
	}
}

func TestCLIBridgeImport(t *testing.T) {
	dir := t.TempDir()
	md := filepath.Join(dir, "CLAUDE.md")
	_ = os.WriteFile(md, []byte(fctOpen+`
- backend is FastAPI on Python 3.12
- the database is Postgres 16
`+fctClose), 0o644)
	db := filepath.Join(dir, "mem.db")

	if code := runCLI([]string{"bridge", "import", "-db", db, "-path", md, "-project", "p"}); code != 0 {
		t.Fatalf("bridge import exit %d", code)
	}
	// Second run must be idempotent: no duplicate facts.
	if code := runCLI([]string{"bridge", "import", "-db", db, "-path", md, "-project", "p"}); code != 0 {
		t.Fatalf("bridge import (2nd) exit %d", code)
	}

	store := openStoreOrDie(db)
	defer store.Close()
	n, _ := store.Count(t.Context(), "p")
	if n != 2 {
		t.Fatalf("imported %d facts after 2 runs, want 2 (idempotent)", n)
	}
}

func TestCLIBridgeImportSanitizes(t *testing.T) {
	dir := t.TempDir()
	md := filepath.Join(dir, "CLAUDE.md")
	// Interpreted string so \x00 is a real NUL byte.
	section := fctOpen + "\n- evil\x00fact\x01with control chars\n" + fctClose
	_ = os.WriteFile(md, []byte(section), 0o644)
	db := filepath.Join(dir, "mem.db")

	if code := runCLI([]string{"bridge", "import", "-db", db, "-path", md, "-project", "p"}); code != 0 {
		t.Fatalf("bridge import exit %d", code)
	}
	store := openStoreOrDie(db)
	defer store.Close()
	mems, _, _ := store.All(t.Context(), "p")
	if len(mems) != 1 {
		t.Fatalf("imported %d facts, want 1", len(mems))
	}
	if strings.Contains(mems[0].Content, "\x00") || strings.Contains(mems[0].Content, "\x01") {
		t.Fatalf("control chars not stripped: %q", mems[0].Content)
	}
}

func TestProjectContextBudget(t *testing.T) {
	// Budget trims output while keeping the header.
	dir := t.TempDir()
	db := filepath.Join(dir, "mem.db")
	store, err := newTestSQLiteStore(db)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	// Insert a couple of memories directly (no embedder needed).
	store.Insert(t.Context(), &memory.Memory{ID: "b1", Type: memory.TypeFact, Content: "backend is FastAPI on Python 3.12", Project: "p"}, []float64{1})
	store.Insert(t.Context(), &memory.Memory{ID: "b2", Type: memory.TypeFact, Content: "the database is Postgres 16", Project: "p"}, []float64{1})

	svc := memory.NewService(store, nil)
	full, _, err := svc.ProjectContext(t.Context(), "p", 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	small, _, err := svc.ProjectContext(t.Context(), "p", 10, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(small) >= len(full) {
		t.Fatalf("budget did not trim: small=%d full=%d", len(small), len(full))
	}
	if !strings.Contains(small, "Project context") {
		t.Fatalf("header lost under budget:\n%s", small)
	}
}

func TestCLIRememberStoresTypedMemory(t *testing.T) {
	dir := t.TempDir()
	db := filepath.Join(dir, "rem.db")
	if code := runCLI([]string{"remember", "-db", db, "-content", "we chose JWT for sessions", "-type", "decision", "-project", "p", "-source", "agent"}); code != 0 {
		t.Fatalf("remember exit %d", code)
	}
	store := openStoreOrDie(db)
	defer store.Close()
	mems, _, _ := store.All(t.Context(), "p")
	if len(mems) != 1 {
		t.Fatalf("stored %d memories, want 1", len(mems))
	}
	if mems[0].Type != memory.TypeDecision {
		t.Fatalf("type = %s, want decision", mems[0].Type)
	}
	if mems[0].Source != "agent" {
		t.Fatalf("source = %q, want agent", mems[0].Source)
	}
}

func TestCLIRememberRequiresContent(t *testing.T) {
	dir := t.TempDir()
	db := filepath.Join(dir, "rem2.db")
	if code := runCLI([]string{"remember", "-db", db, "-content", "  "}); code == 0 {
		t.Fatal("expected non-zero exit for empty content")
	}
}
