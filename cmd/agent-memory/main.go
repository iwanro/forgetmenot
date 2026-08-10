// Command agent-memory is an MCP server exposing persistent, structured,
// semantically searchable memory to any MCP-capable agent (Claude Code,
// Cursor, etc.). See PRD.md in the repository root.
package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/iwan/agent-memory/internal/embed"
	"github.com/iwan/agent-memory/internal/mcpserver"
	"github.com/iwan/agent-memory/internal/memory"
)

func main() {
	var (
		dbPath      = flag.String("db", defaultDBPath(), "path to the SQLite memory database")
		embedKind   = flag.String("embed", "ollama", "embedding provider: ollama | openai")
		embedURL    = flag.String("embed-url", "", "embedding endpoint base URL (default: Ollama localhost:11434)")
		embedModel  = flag.String("embed-model", "", "embedding model name (default: nomic-embed-text)")
		embedAPIKey = flag.String("embed-api-key", "", "API key for the openai provider")
		version     = flag.Bool("version", false, "print version and exit")
	)
	flag.Parse()

	if *version {
		log.Printf("agent-memory %s", "v0.1.0")
		return
	}

	store, err := memory.NewSQLiteStore(*dbPath)
	if err != nil {
		log.Fatalf("open store: %v", err)
	}
	defer store.Close()

	var em memory.Embedder
	switch *embedKind {
	case "ollama":
		em = embed.NewOllama(*embedURL, *embedModel)
	case "openai":
		em = embed.NewOpenAICompat(*embedURL, *embedAPIKey, *embedModel)
	default:
		log.Fatalf("unknown embed provider %q (want ollama or openai)", *embedKind)
	}

	svc := memory.NewService(store, em)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := mcpserver.Run(ctx, svc, mcpserver.Options{Name: "agent-memory", Version: "v0.1.0"}); err != nil {
		log.Fatalf("mcp server: %v", err)
	}
}

// defaultDBPath resolves a stable, user-writable location for the database:
// $XDG_DATA_HOME/agent-memory/memory.db, falling back to ~/.local/share.
func defaultDBPath() string {
	base := os.Getenv("XDG_DATA_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "agent-memory.db"
		}
		base = filepath.Join(home, ".local", "share")
	}
	dir := filepath.Join(base, "agent-memory")
	if err := os.MkdirAll(dir, 0o700); err == nil {
		return filepath.Join(dir, "memory.db")
	}
	return "agent-memory.db"
}
