package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/iwanro/forgetmenot/internal/eval"
	"github.com/iwanro/forgetmenot/internal/memory"
)

// cliExportRecord is one memory in the portable export format. The embedding
// is included so import is a faithful restore without needing an embedder.
type cliExportRecord struct {
	ID         string            `json:"id"`
	Type       string            `json:"type"`
	Content    string            `json:"content"`
	Project    string            `json:"project"`
	Importance float64           `json:"importance"`
	Source     string            `json:"source"`
	Trust      string            `json:"trust"`
	Metadata   map[string]string `json:"metadata"`
	Embedding  []float64         `json:"embedding"`
}

type cliExport struct {
	Version    int               `json:"version"`
	ExportedAt string            `json:"exported_at"`
	Memories   []cliExportRecord `json:"memories"`
}

// runCLI dispatches the subcommands: export, import, stats, list.
// Returns a process exit code.
func runCLI(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: forgetmenot <serve|remember|capture|session|timeline|project_context|maintain|setup|bridge|export-md|export|import|stats|list|eval> [flags]")
		return 2
	}
	switch args[0] {
	case "export":
		return cliExportCmd(args[1:])
	case "import":
		return cliImportCmd(args[1:])
	case "stats":
		return cliStatsCmd(args[1:])
	case "list":
		return cliListCmd(args[1:])
	case "eval":
		return cliEvalCmd(args[1:])
	case "project_context":
		return cliProjectContextCmd(args[1:])
	case "capture":
		return cliCaptureCmd(args[1:])
	case "remember":
		return cliRememberCmd(args[1:])
	case "maintain":
		return cliMaintainCmd(args[1:])
	case "setup":
		return cliSetupCmd(args[1:])
	case "bridge":
		return cliBridgeCmd(args[1:])
	case "session":
		return cliSessionCmd(args[1:])
	case "timeline":
		return cliTimelineCmd(args[1:])
	case "export-md":
		return cliExportMdCmd(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n", args[0])
		return 2
	}
}

// cliEvalCmd seeds the demo project (if empty) and runs the eval dataset
// against the configured embedder, printing recall@k.
func cliEvalCmd(args []string) int {
	fs := flag.NewFlagSet("eval", flag.ExitOnError)
	dbPath := fs.String("db", defaultDBPath(), "path to the SQLite database")
	embedKind := fs.String("embed", "ollama", "embedding provider: ollama | openai")
	embedURL := fs.String("embed-url", "", "embedding endpoint base URL")
	embedModel := fs.String("embed-model", "", "embedding model name")
	embedAPIKey := fs.String("embed-api-key", "", "API key for the openai provider")
	jsonOut := fs.Bool("json", false, "print machine-readable JSON result")
	fs.Parse(args)

	store := openStoreOrDie(*dbPath)
	defer store.Close()

	em, err := buildEmbedder(*embedKind, *embedURL, *embedModel, *embedAPIKey)
	if err != nil {
		fmt.Fprintf(os.Stderr, "eval: %v\n", err)
		return 1
	}
	svc := memory.NewService(store, em)

	ctx := context.Background()
	if n, err := eval.SeedDataset(ctx, svc); err != nil {
		fmt.Fprintf(os.Stderr, "eval: seed: %v\n", err)
		return 1
	} else if n > 0 {
		fmt.Printf("seeded %d demo facts\n", n)
	}
	res := eval.Run(ctx, svc, eval.DefaultDataset)
	if *jsonOut {
		b, err := json.MarshalIndent(res, "", "  ")
		if err != nil {
			fmt.Fprintf(os.Stderr, "eval: %v\n", err)
			return 1
		}
		fmt.Println(string(b))
	} else {
		fmt.Print(res.String())
	}
	if res.RecallAtK < 0.8 {
		return 1 // low score is a failure signal for CI/manual runs
	}
	return 0
}

func openStoreOrDie(dbPath string) *memory.SQLiteStore {
	store, err := memory.NewSQLiteStore(dbPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "open store: %v\n", err)
		os.Exit(1)
	}
	return store
}

func cliExportCmd(args []string) int {
	return cliExportTo(args, os.Stdout)
}

func cliExportTo(args []string, out io.Writer) int {
	fs := flag.NewFlagSet("export", flag.ExitOnError)
	dbPath := fs.String("db", defaultDBPath(), "path to the SQLite database")
	project := fs.String("project", "", "export only this project (empty = all)")
	fs.Parse(args)

	store := openStoreOrDie(*dbPath)
	defer store.Close()

	ctx := context.Background()
	mems, embs, err := store.All(ctx, *project)
	if err != nil {
		fmt.Fprintf(os.Stderr, "export: %v\n", err)
		return 1
	}
	outExport := cliExport{
		Version:    1,
		ExportedAt: time.Now().UTC().Format(time.RFC3339),
		Memories:   make([]cliExportRecord, 0, len(mems)),
	}
	for i, m := range mems {
		outExport.Memories = append(outExport.Memories, cliExportRecord{
			ID:         m.ID,
			Type:       string(m.Type),
			Content:    m.Content,
			Project:    m.Project,
			Importance: m.Importance,
			Source:     m.Source,
			Trust:      string(m.Trust),
			Metadata:   m.Metadata,
			Embedding:  embs[i],
		})
	}
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	if err := enc.Encode(outExport); err != nil {
		fmt.Fprintf(os.Stderr, "export: %v\n", err)
		return 1
	}
	return 0
}

func cliImportCmd(args []string) int {
	return cliImportFrom(args, os.Stdin)
}

func cliImportFrom(args []string, in io.Reader) int {
	fs := flag.NewFlagSet("import", flag.ExitOnError)
	dbPath := fs.String("db", defaultDBPath(), "path to the SQLite database")
	fs.Parse(args)

	store := openStoreOrDie(*dbPath)
	defer store.Close()

	data, err := io.ReadAll(in)
	if err != nil {
		fmt.Fprintf(os.Stderr, "import: read stdin: %v\n", err)
		return 1
	}
	var doc cliExport
	if err := json.Unmarshal(data, &doc); err != nil {
		fmt.Fprintf(os.Stderr, "import: parse: %v\n", err)
		return 1
	}
	ctx := context.Background()
	inserted := 0
	for _, r := range doc.Memories {
		typ := memory.Type(r.Type)
		if !memory.ValidTypes[typ] {
			fmt.Fprintf(os.Stderr, "import: %s: invalid memory type %q\n", r.ID, r.Type)
			return 1
		}
		trust := memory.Trust(r.Trust)
		if trust == "" {
			trust = memory.TrustHigh
		}
		// Sanitize like every write path: control chars stripped, length cap.
		content := memory.Sanitize(r.Content)
		if content == "" {
			fmt.Fprintf(os.Stderr, "import: %s: empty content after sanitize\n", r.ID)
			return 1
		}
		m := &memory.Memory{
			ID:         r.ID,
			Type:       typ,
			Content:    content,
			Project:    r.Project,
			Importance: r.Importance,
			Source:     r.Source,
			Trust:      trust,
			Metadata:   r.Metadata,
		}
		if m.Project == "" {
			m.Project = memory.DefaultProject
		}
		if m.Metadata == nil {
			m.Metadata = map[string]string{}
		}
		now := time.Now().UTC()
		m.CreatedAt, m.UpdatedAt, m.LastAccessedAt = now, now, now
		if err := store.Insert(ctx, m, r.Embedding); err != nil {
			fmt.Fprintf(os.Stderr, "import: insert %s: %v\n", r.ID, err)
			return 1
		}
		inserted++
	}
	fmt.Printf("imported %d memories\n", inserted)
	return 0
}

func cliStatsCmd(args []string) int {
	fs := flag.NewFlagSet("stats", flag.ExitOnError)
	dbPath := fs.String("db", defaultDBPath(), "path to the SQLite database")
	fs.Parse(args)

	store := openStoreOrDie(*dbPath)
	defer store.Close()

	svc := memory.NewService(store, nil)
	s, err := svc.Stats(context.Background())
	if err != nil {
		fmt.Fprintf(os.Stderr, "stats: %v\n", err)
		return 1
	}
	fmt.Printf("memories:  %d\n", s.Count)
	fmt.Printf("projects:  %d\n", s.ProjectCount)
	return 0
}

func cliListCmd(args []string) int {
	fs := flag.NewFlagSet("list", flag.ExitOnError)
	dbPath := fs.String("db", defaultDBPath(), "path to the SQLite database")
	project := fs.String("project", "", "list only this project (empty = all)")
	fs.Parse(args)

	store := openStoreOrDie(*dbPath)
	defer store.Close()

	ctx := context.Background()
	mems, _, err := store.All(ctx, *project)
	if err != nil {
		fmt.Fprintf(os.Stderr, "list: %v\n", err)
		return 1
	}
	for _, m := range mems {
		fmt.Printf("%s  [%s] %s: %s\n", m.ID, m.Type, m.Project, truncate(m.Content, 80))
	}
	return 0
}

func truncate(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n-3]) + "..."
}
