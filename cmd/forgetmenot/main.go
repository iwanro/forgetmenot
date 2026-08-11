// Command forgetmenot is an MCP server exposing persistent, structured,
// semantically searchable memory to any MCP-capable agent (Claude Code,
// Cursor, etc.). See PRD.md in the repository root.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/iwanro/forgetmenot/internal/embed"
	"github.com/iwanro/forgetmenot/internal/mcpserver"
	"github.com/iwanro/forgetmenot/internal/memory"
)

// version is injected at build time by goreleaser (-X main.version=...).
var version = "dev"

func main() {
	// Subcommands (CLI) dispatch to runCLI. Anything else is the MCP server
	// (serve is the default).
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "export", "import", "stats", "list", "eval", "project_context", "capture", "maintain", "setup", "bridge", "remember", "session", "timeline", "export-md", "web", "summarize", "doctor":
			os.Exit(runCLI(os.Args[1:]))
		}
	}

	var (
		dbPath      = flag.String("db", defaultDBPath(), "path to the SQLite memory database")
		embedKind   = flag.String("embed", "ollama", "embedding provider: ollama | openai")
		embedURL    = flag.String("embed-url", "", "embedding endpoint base URL (default: Ollama localhost:11434)")
		embedModel  = flag.String("embed-model", "", "embedding model name (default: nomic-embed-text)")
		embedAPIKey = flag.String("embed-api-key", "", "API key for the openai provider")
		llmKind     = flag.String("llm", "", "chat provider for auto-topics/summarize: ollama | openai (empty disables)")
		llmURL      = flag.String("llm-url", "", "chat endpoint base URL")
		llmModel    = flag.String("llm-model", "", "chat model name")
		llmAPIKey   = flag.String("llm-api-key", "", "API key for the openai chat provider")
		showVersion = flag.Bool("version", false, "print version and exit")
	)
	flag.Parse()

	if *showVersion {
		log.Printf("forgetmenot %s", version)
		return
	}

	store, err := memory.NewSQLiteStore(*dbPath)
	if err != nil {
		log.Fatalf("open store: %v", err)
	}
	defer store.Close()

	em, err := buildEmbedder(*embedKind, *embedURL, *embedModel, *embedAPIKey)
	if err != nil {
		log.Fatal(err)
	}

	svc := memory.NewService(store, em)
	svc.SetDBPath(*dbPath)
	svc.LLM = buildLLM(*llmKind, *llmURL, *llmModel, *llmAPIKey)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := mcpserver.Run(ctx, svc, mcpserver.Options{Name: "forgetmenot", Version: version}); err != nil {
		log.Fatalf("mcp server: %v", err)
	}
}

// buildEmbedder constructs the embedding provider from CLI flags.
func buildEmbedder(kind, url, model, apiKey string) (memory.Embedder, error) {
	switch kind {
	case "ollama":
		return embed.NewOllama(url, model), nil
	case "openai":
		return embed.NewOpenAICompat(url, apiKey, model), nil
	default:
		return nil, fmt.Errorf("unknown embed provider %q (want ollama or openai)", kind)
	}
}

// defaultDBPath resolves a stable, user-writable location for the database:
// $XDG_DATA_HOME/forgetmenot/memory.db, falling back to ~/.local/share.
func defaultDBPath() string {
	base := os.Getenv("XDG_DATA_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "forgetmenot.db"
		}
		base = filepath.Join(home, ".local", "share")
	}
	dir := filepath.Join(base, "forgetmenot")
	if err := os.MkdirAll(dir, 0o700); err == nil {
		return filepath.Join(dir, "memory.db")
	}
	return "forgetmenot.db"
}
