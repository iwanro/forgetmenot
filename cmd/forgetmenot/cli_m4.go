package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/iwanro/forgetmenot/internal/memory"
	"github.com/iwanro/forgetmenot/internal/webui"
)

// --- session ---------------------------------------------------------------

func cliSessionCmd(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: forgetmenot session <start|end|list> [flags]")
		return 2
	}
	switch args[0] {
	case "start":
		return cliSessionStart(args[1:])
	case "end":
		return cliSessionEnd(args[1:])
	case "list":
		return cliSessionList(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "unknown session command %q\n", args[0])
		return 2
	}
}

func cliSessionStart(args []string) int {
	fs := flag.NewFlagSet("session start", flag.ExitOnError)
	dbPath := fs.String("db", defaultDBPath(), "path to the SQLite database")
	project := fs.String("project", "global", "project namespace")
	fs.Parse(args)

	store := openStoreOrDie(*dbPath)
	defer store.Close()
	svc := memory.NewService(store, nil)
	svc.SetDBPath(*dbPath)

	sess, err := svc.StartSession(context.Background(), *project)
	if err != nil {
		fmt.Fprintf(os.Stderr, "session start: %v\n", err)
		return 1
	}
	fmt.Printf("session started %s\n", sess.ID)
	return 0
}

func cliSessionEnd(args []string) int {
	fs := flag.NewFlagSet("session end", flag.ExitOnError)
	dbPath := fs.String("db", defaultDBPath(), "path to the SQLite database")
	fs.Parse(args)

	store := openStoreOrDie(*dbPath)
	defer store.Close()
	svc := memory.NewService(store, nil)
	svc.SetDBPath(*dbPath)

	if err := svc.EndSession(context.Background(), ""); err != nil {
		fmt.Fprintf(os.Stderr, "session end: %v\n", err)
		return 1
	}
	fmt.Println("session ended")
	return 0
}

func cliSessionList(args []string) int {
	fs := flag.NewFlagSet("session list", flag.ExitOnError)
	dbPath := fs.String("db", defaultDBPath(), "path to the SQLite database")
	project := fs.String("project", "global", "project namespace")
	fs.Parse(args)

	store := openStoreOrDie(*dbPath)
	defer store.Close()
	svc := memory.NewService(store, nil)
	svc.SetDBPath(*dbPath)

	sessions, err := svc.Store.SessionsForProject(context.Background(), *project)
	if err != nil {
		fmt.Fprintf(os.Stderr, "session list: %v\n", err)
		return 1
	}
	for _, s := range sessions {
		state := "open"
		if s.EndedAt != nil {
			state = s.EndedAt.UTC().Format("2006-01-02 15:04")
		}
		fmt.Printf("%s  %s  started %s  %s\n",
			s.ID, state, s.StartedAt.UTC().Format("2006-01-02 15:04"), truncate(s.Summary, 40))
	}
	return 0
}

// --- timeline ---------------------------------------------------------------

func cliTimelineCmd(args []string) int {
	fs := flag.NewFlagSet("timeline", flag.ExitOnError)
	dbPath := fs.String("db", defaultDBPath(), "path to the SQLite database")
	project := fs.String("project", "global", "project namespace")
	topic := fs.String("topic", "", "topic to trace across sessions (empty = all memories)")
	limit := fs.Int("limit", 50, "max timeline entries")
	fs.Parse(args)

	store := openStoreOrDie(*dbPath)
	defer store.Close()
	svc := memory.NewService(store, nil)
	svc.SetDBPath(*dbPath)

	entries, err := svc.Timeline(context.Background(), *project, *topic, *limit)
	if err != nil {
		fmt.Fprintf(os.Stderr, "timeline: %v\n", err)
		return 1
	}
	fmt.Printf("# Timeline%s%s (%d entries)\n\n",
		map[bool]string{true: " for topic: " + *topic, false: ""}[*topic != ""],
		map[bool]string{true: " in " + *project, false: ""}[*project != ""], len(entries))
	for _, e := range entries {
		when := e.Memory.CreatedAt.UTC().Format("2006-01-02 15:04")
		sess := ""
		if e.Session != nil {
			sess = " (session " + e.Session.ID[:8] + ")"
		}
		line := fmt.Sprintf("- [%s] %s%s", when, e.Memory.Content, sess)
		if e.Memory.Type == memory.TypeDecision {
			line += " [decision]"
		}
		if e.Memory.Trust == memory.TrustLow {
			line += " [UNTRUSTED]"
		}
		fmt.Println(line)
	}
	return 0
}

// --- export-md --------------------------------------------------------------

// cliExportMdCmd writes a human/AI-readable Markdown file per project.
// Format: headers per memory type, bullets with content, source, trust and
// session reference. Easy to read by both humans and agents directly.
func cliExportMdCmd(args []string) int {
	fs := flag.NewFlagSet("export-md", flag.ExitOnError)
	dbPath := fs.String("db", defaultDBPath(), "path to the SQLite database")
	project := fs.String("project", "global", "project namespace")
	out := fs.String("out", "", "output file (default: <project>.md in cwd)")
	fs.Parse(args)

	store := openStoreOrDie(*dbPath)
	defer store.Close()
	svc := memory.NewService(store, nil)
	svc.SetDBPath(*dbPath)

	ctx := context.Background()
	text, err := exportMarkdown(ctx, svc, *project)
	if err != nil {
		fmt.Fprintf(os.Stderr, "export-md: %v\n", err)
		return 1
	}
	path := *out
	if path == "" {
		path = *project + ".md"
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "export-md: %v\n", err)
		return 1
	}
	fmt.Printf("export-md: wrote %s (%d bytes)\n", path, len(text))
	return 0
}

func exportMarkdown(ctx context.Context, svc *memory.Service, project string) (string, error) {
	mems, _, err := svc.Store.All(ctx, project)
	if err != nil {
		return "", err
	}
	// Group by type, ordered by creation.
	groups := map[memory.Type][]*memory.Memory{}
	var order []memory.Type
	for _, m := range mems {
		if _, ok := groups[m.Type]; !ok {
			order = append(order, m.Type)
		}
		groups[m.Type] = append(groups[m.Type], m)
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "# %s\n\n", project)
	fmt.Fprintf(&sb, "_Generated by forgetmenot. Edit freely; `bridge import` can re-ingest facts._\n\n")
	for _, t := range order {
		ms := groups[t]
		fmt.Fprintf(&sb, "## %s (%d)\n\n", t, len(ms))
		for _, m := range ms {
			when := m.CreatedAt.UTC().Format("2006-01-02")
			meta := fmt.Sprintf("_%s_", when)
			if m.Source != "" {
				meta += " · " + m.Source
			}
			if m.Trust == memory.TrustLow {
				meta += " · **UNTRUSTED**"
			}
			if m.SessionID != "" {
				meta += " · session `" + m.SessionID[:8] + "`"
			}
			fmt.Fprintf(&sb, "- %s\n  %s\n", m.Content, meta)
		}
		sb.WriteString("\n")
	}
	return sb.String(), nil
}

// cliWebCmd serves the local browser UI (embedded) plus a JSON API.
func cliWebCmd(args []string) int {
	fs := flag.NewFlagSet("web", flag.ExitOnError)
	dbPath := fs.String("db", defaultDBPath(), "path to the SQLite database")
	addr := fs.String("addr", "127.0.0.1:8090", "listen address")
	fs.Parse(args)

	store := openStoreOrDie(*dbPath)
	defer store.Close()
	svc := memory.NewService(store, nil)
	svc.SetDBPath(*dbPath)

	handler := webui.New(svc)
	srv := &http.Server{Addr: *addr, Handler: handler}
	fmt.Printf("forgetmenot web UI: http://%s\n", *addr)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		fmt.Fprintf(os.Stderr, "web: %v\n", err)
		return 1
	}
	return 0
}
