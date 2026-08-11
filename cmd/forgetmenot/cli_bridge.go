package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/iwanro/forgetmenot/internal/memory"
)

// CLAUDE.md bridge markers. The export section is machine-managed; the
// import section is a place where a user (or agent) can drop bullet facts
// that get ingested into memory.
const (
	ctxOpen  = "<!-- forgetmenot:context -->"
	ctxClose = "<!-- /forgetmenot:context -->"
	fctOpen  = "<!-- forgetmenot:facts -->"
	fctClose = "<!-- /forgetmenot:facts -->"
)

// cliBridgeExportCmd writes (or refreshes) the project context section in
// CLAUDE.md so the agent always sees current memory at session start, even
// before MCP tools are available. PRD M3: bridge with native memory.
func cliBridgeExportCmd(args []string) int {
	fs := flag.NewFlagSet("bridge export", flag.ExitOnError)
	dbPath := fs.String("db", defaultDBPath(), "path to the SQLite database")
	path := fs.String("path", "CLAUDE.md", "CLAUDE.md file to update")
	project := fs.String("project", "global", "project namespace")
	limit := fs.Int("limit", 20, "max memories to include")
	budget := fs.Int("budget", 0, "max characters of output; 0 = unlimited")
	fs.Parse(args)

	store := openStoreOrDie(*dbPath)
	defer store.Close()

	svc := memory.NewService(store, nil)
	text, _, err := svc.ProjectContext(context.Background(), *project, *limit, *budget)
	if err != nil {
		fmt.Fprintf(os.Stderr, "bridge export: %v\n", err)
		return 1
	}

	block := ctxOpen + "\n" + text + "\n" + ctxClose
	if err := replaceMarkedSection(*path, ctxOpen, ctxClose, block); err != nil {
		fmt.Fprintf(os.Stderr, "bridge export: %v\n", err)
		return 1
	}
	fmt.Printf("bridge export: updated %s\n", *path)
	return 0
}

// cliBridgeImportCmd reads bullet facts from the <!-- forgetmenot:facts -->
// section of CLAUDE.md and stores them as facts (idempotent via dedupe).
func cliBridgeImportCmd(args []string) int {
	fs := flag.NewFlagSet("bridge import", flag.ExitOnError)
	dbPath := fs.String("db", defaultDBPath(), "path to the SQLite database")
	path := fs.String("path", "CLAUDE.md", "CLAUDE.md file to read")
	project := fs.String("project", "global", "project namespace")
	fs.Parse(args)

	content, err := os.ReadFile(*path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "bridge import: %v\n", err)
		return 1
	}
	section, ok := extractMarkedSection(string(content), fctOpen, fctClose)
	if !ok {
		fmt.Println("bridge import: no <!-- forgetmenot:facts --> section found")
		return 0
	}

	store := openStoreOrDie(*dbPath)
	defer store.Close()

	ctx := context.Background()
	imported := 0
	for _, line := range strings.Split(section, "\n") {
		line = strings.TrimSpace(line)
		line = strings.TrimPrefix(line, "-")
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// No embedder guaranteed in hooks; store as high-trust fact with an
		// empty vector (visible in context/list, not semantic recall).
		m := &memory.Memory{
			ID:             memory.NewID(),
			Type:           memory.TypeFact,
			Content:        line,
			Project:        *project,
			Importance:     0.6,
			AccessCount:    1,
			LastAccessedAt: time.Now().UTC(),
			CreatedAt:      time.Now().UTC(),
			UpdatedAt:      time.Now().UTC(),
			Source:         "CLAUDE.md",
			Trust:          memory.TrustHigh,
			Metadata:       map[string]string{},
		}
		if err := store.Insert(ctx, m, nil); err != nil {
			fmt.Fprintf(os.Stderr, "bridge import: %v\n", err)
			return 1
		}
		imported++
	}
	fmt.Printf("bridge import: stored %d facts from %s\n", imported, *path)
	return 0
}

// replaceMarkedSection replaces the region between open and close markers
// (inclusive) with the new block. If the markers are absent, the block is
// appended at the end of the file.
func replaceMarkedSection(path, open, close, block string) error {
	content, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	text := string(content)
	start := strings.Index(text, open)
	if start < 0 {
		// Append a new section with a blank line separator.
		if text != "" && !strings.HasSuffix(text, "\n\n") {
			text += "\n"
		}
		text += block + "\n"
	} else {
		end := strings.Index(text[start:], close)
		if end < 0 {
			return fmt.Errorf("found %s but missing %s", open, close)
		}
		end = start + end + len(close)
		text = text[:start] + block + text[end:]
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(text), 0o644)
}

// extractMarkedSection returns the text between open and close markers.
func extractMarkedSection(text, open, close string) (string, bool) {
	start := strings.Index(text, open)
	if start < 0 {
		return "", false
	}
	start += len(open)
	end := strings.Index(text[start:], close)
	if end < 0 {
		return "", false
	}
	return strings.TrimSpace(text[start : start+end]), true
}

// cliBridgeCmd dispatches bridge subcommands: export, import.
func cliBridgeCmd(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: forgetmenot bridge <export|import> [flags]")
		return 2
	}
	switch args[0] {
	case "export":
		return cliBridgeExportCmd(args[1:])
	case "import":
		return cliBridgeImportCmd(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "unknown bridge command %q\n", args[0])
		return 2
	}
}
