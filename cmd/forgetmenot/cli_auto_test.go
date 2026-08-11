package main

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/iwanro/forgetmenot/internal/memory"
)

// captureStdout runs f while capturing writes to stdout.
func captureStdout(t *testing.T, f func()) string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	f()
	w.Close()
	os.Stdout = old
	b, _ := io.ReadAll(r)
	return string(b)
}

// TestCLIProjectContext prints a summary for a seeded project.
func TestCLIProjectContext(t *testing.T) {
	dir := t.TempDir()
	db := filepath.Join(dir, "pc.db")
	store, err := memory.NewSQLiteStore(db)
	if err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()
	_ = store.Insert(ctx, &memory.Memory{
		ID: "pc1", Type: memory.TypeFact, Content: "backend is FastAPI", Project: "p",
	}, []float64{1, 0})
	_ = store.Insert(ctx, &memory.Memory{
		ID: "pc2", Type: memory.TypeDecision, Content: "use JWT for auth", Project: "p",
	}, []float64{0, 1})
	store.Close()

	out := captureStdout(t, func() {
		if code := runCLI([]string{"project_context", "-db", db, "-project", "p"}); code != 0 {
			t.Fatalf("project_context exit %d", code)
		}
	})
	if !strings.Contains(out, "# Project context: p") || !strings.Contains(out, "FastAPI") {
		t.Fatalf("unexpected output:\n%s", out)
	}
	if strings.Contains(out, "JWT") == false {
		t.Fatalf("decision missing from context:\n%s", out)
	}
}

// TestCLICapture reads a summary from stdin and stores an episode.
func TestCLICapture(t *testing.T) {
	dir := t.TempDir()
	db := filepath.Join(dir, "cap.db")

	out := captureStdout(t, func() {
		if code := cliCaptureFrom(strings.NewReader("refactored auth, decided JWT"), []string{"-db", db, "-project", "p"}); code != 0 {
			t.Fatalf("capture exit %d", code)
		}
	})
	if !strings.Contains(out, "captured ") {
		t.Fatalf("capture output: %q", out)
	}

	store, err := memory.NewSQLiteStore(db)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	n, _ := store.Count(t.Context(), "p")
	if n != 1 {
		t.Fatalf("count = %d, want 1", n)
	}
}

// TestCLISetupWritesHooks writes a Claude Code settings file with hooks.
func TestCLISetupWritesHooks(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, ".claude", "settings.json")

	if code := cliSetupTo([]string{"-db", "/tmp/x.db", "-out", out}); code != 0 {
		t.Fatalf("setup exit %d", code)
	}
	b, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	var cfg map[string]any
	if err := json.Unmarshal(b, &cfg); err != nil {
		t.Fatalf("settings not valid JSON: %v\n%s", err, b)
	}
	hooks, ok := cfg["hooks"].(map[string]any)
	if !ok {
		t.Fatalf("hooks missing: %s", b)
	}
	if _, ok := hooks["SessionStart"]; !ok {
		t.Fatalf("SessionStart hook missing: %s", b)
	}
	if _, ok := hooks["Stop"]; !ok {
		t.Fatalf("Stop hook missing: %s", b)
	}
	if !strings.Contains(string(b), "project_context") || !strings.Contains(string(b), "capture") {
		t.Fatalf("hook commands missing:\n%s", b)
	}
}

// TestCLISetupPreservesExistingSettings merges, not clobbers: unknown
// top-level fields AND existing hooks (e.g. PreToolUse) survive.
func TestCLISetupPreservesExistingSettings(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "settings.json")
	_ = os.WriteFile(out, []byte(`{
  "permissions": {"allow": ["Bash"]},
  "hooks": {"PreToolUse": [{"command": "echo hi"}]}
}`), 0o644)

	if code := cliSetupTo([]string{"-db", "/tmp/x.db", "-out", out}); code != 0 {
		t.Fatalf("setup exit %d", code)
	}
	b, _ := os.ReadFile(out)
	var cfg map[string]any
	_ = json.Unmarshal(b, &cfg)
	if _, ok := cfg["permissions"]; !ok {
		t.Fatalf("existing permissions clobbered:\n%s", b)
	}
	hooks, ok := cfg["hooks"].(map[string]any)
	if !ok {
		t.Fatalf("hooks missing:\n%s", b)
	}
	if _, ok := hooks["PreToolUse"]; !ok {
		t.Fatalf("existing PreToolUse hook clobbered:\n%s", b)
	}
	if _, ok := hooks["SessionStart"]; !ok {
		t.Fatalf("SessionStart hook missing after merge:\n%s", b)
	}
}

// TestCLICaptureNoopOnEmptyInput keeps the hook green when stdin is empty.
func TestCLICaptureNoopOnEmptyInput(t *testing.T) {
	dir := t.TempDir()
	db := filepath.Join(dir, "cap.db")
	out := captureStdout(t, func() {
		if code := cliCaptureFrom(strings.NewReader("   \n"), []string{"-db", db, "-project", "p"}); code != 0 {
			t.Fatalf("capture exit %d on empty input", code)
		}
	})
	if !strings.Contains(out, "nothing to capture") {
		t.Fatalf("expected no-op message, got: %q", out)
	}
}

// TestShellJoin quotes arguments with spaces and single quotes.
func TestShellJoin(t *testing.T) {
	got := shellJoin("/path with space/bin", "project_context", "-db", "/x/y.db")
	want := "'/path with space/bin' 'project_context' '-db' '/x/y.db'"
	if got != want {
		t.Fatalf("shellJoin = %q, want %q", got, want)
	}
}

// TestCLISetupWritesMCPConfig verifies `setup -mcp` writes a generic
// .mcp.json for any MCP client using the ABSOLUTE binary path, so agents work
// even when forgetmenot is not on $PATH (the opencode failure mode).
func TestCLISetupWritesMCPConfig(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, ".mcp.json")

	if code := cliSetupTo([]string{"-db", "/tmp/x.db", "-out", filepath.Join(dir, ".claude", "settings.json"), "-mcp", out}); code != 0 {
		t.Fatalf("setup exit %d", code)
	}
	b, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	var cfg struct {
		Servers map[string]struct {
			Command string   `json:"command"`
			Args    []string `json:"args"`
		} `json:"mcpServers"`
	}
	if err := json.Unmarshal(b, &cfg); err != nil {
		t.Fatalf(".mcp.json not valid JSON: %v\n%s", err, b)
	}
	srv, ok := cfg.Servers["forgetmenot"]
	if !ok {
		t.Fatalf("forgetmenot server missing:\n%s", b)
	}
	if !filepath.IsAbs(srv.Command) {
		t.Fatalf("command %q is not absolute (this is the whole point):\n%s", srv.Command, b)
	}
	if len(srv.Args) != 0 {
		t.Fatalf("args = %v, want []", srv.Args)
	}
}

// TestCLISetupMCPPreservesOtherServers merges, not clobbers.
func TestCLISetupMCPPreservesOtherServers(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, ".mcp.json")
	_ = os.WriteFile(out, []byte(`{
  "mcpServers": {
    "github": {"command": "gh-mcp", "args": []}
  }
}`), 0o644)

	if code := cliSetupTo([]string{"-db", "/tmp/x.db", "-out", filepath.Join(dir, ".claude", "settings.json"), "-mcp", out}); code != 0 {
		t.Fatalf("setup exit %d", code)
	}
	b, _ := os.ReadFile(out)
	var cfg map[string]any
	_ = json.Unmarshal(b, &cfg)
	servers, ok := cfg["mcpServers"].(map[string]any)
	if !ok {
		t.Fatalf("mcpServers missing:\n%s", b)
	}
	if _, ok := servers["github"]; !ok {
		t.Fatalf("existing github server clobbered:\n%s", b)
	}
	if _, ok := servers["forgetmenot"]; !ok {
		t.Fatalf("forgetmenot missing after merge:\n%s", b)
	}
}

// TestWriteMCPSettingsInvalidJSON surfaces corrupt config instead of silently
// overwriting the user's file.
func TestWriteMCPSettingsInvalidJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".mcp.json")
	_ = os.WriteFile(path, []byte("not json"), 0o644)
	if err := writeMCPSettings(path, "/abs/bin/forgetmenot"); err == nil {
		t.Fatal("expected error for invalid existing .mcp.json")
	}
}
