package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/iwanro/forgetmenot/internal/memory"
)

// cliProjectContextCmd prints a ready-to-inject project summary. Used by the
// SessionStart hook. PRD §9.1.
func cliProjectContextCmd(args []string) int {
	fs := flag.NewFlagSet("project_context", flag.ExitOnError)
	dbPath := fs.String("db", defaultDBPath(), "path to the SQLite database")
	project := fs.String("project", "global", "project namespace")
	limit := fs.Int("limit", 15, "max memories to include")
	fs.Parse(args)

	store := openStoreOrDie(*dbPath)
	defer store.Close()

	svc := memory.NewService(store, nil)
	ctx := context.Background()
	text, _, err := svc.ProjectContext(ctx, *project, *limit)
	if err != nil {
		fmt.Fprintf(os.Stderr, "project_context: %v\n", err)
		return 1
	}
	fmt.Print(text)
	return 0
}

// cliCaptureCmd stores a session summary as an episode. Reads the summary
// from stdin (so the hook can pipe it) or from -summary. Used by the
// Stop/SessionEnd hook. PRD §9.1.
func cliCaptureCmd(args []string) int {
	return cliCaptureFrom(os.Stdin, args)
}

func cliCaptureFrom(in io.Reader, args []string) int {
	fs := flag.NewFlagSet("capture", flag.ExitOnError)
	dbPath := fs.String("db", defaultDBPath(), "path to the SQLite database")
	project := fs.String("project", "global", "project namespace")
	source := fs.String("source", "session", "source label (e.g. claude-code)")
	summaryFlag := fs.String("summary", "", "summary text (alternative to stdin)")
	fs.Parse(args)

	summary := *summaryFlag
	if summary == "" {
		b, err := io.ReadAll(in)
		if err != nil {
			fmt.Fprintf(os.Stderr, "capture: read stdin: %v\n", err)
			return 1
		}
		summary = string(b)
	}
	if strings.TrimSpace(summary) == "" {
		// Hooks fire even when there is nothing meaningful to capture; a
		// no-op keeps the hook green instead of failing the session.
		fmt.Println("capture: nothing to capture")
		return 0
	}

	store := openStoreOrDie(*dbPath)
	defer store.Close()

	svc := memory.NewService(store, nil)
	id, err := svc.CaptureSummary(context.Background(), *project, summary, *source)
	if err != nil {
		fmt.Fprintf(os.Stderr, "capture: %v\n", err)
		return 1
	}
	fmt.Printf("captured %s\n", id)
	return 0
}

// cliMaintainCmd runs decay (and in M3: compression). Safe to run from cron
// or a timer. PRD §9.1.
func cliMaintainCmd(args []string) int {
	fs := flag.NewFlagSet("maintain", flag.ExitOnError)
	dbPath := fs.String("db", defaultDBPath(), "path to the SQLite database")
	olderThan := fs.Duration("older-than", 14*24*time.Hour, "only decay memories not accessed for this long")
	minImp := fs.Float64("min-importance", 0.1, "floor for decayed importance")
	fs.Parse(args)

	store := openStoreOrDie(*dbPath)
	defer store.Close()

	svc := memory.NewService(store, nil)
	touched, err := svc.Decay(context.Background(), *olderThan, *minImp)
	if err != nil {
		fmt.Fprintf(os.Stderr, "maintain: %v\n", err)
		return 1
	}
	fmt.Printf("maintain: decayed %d memories\n", touched)
	return 0
}

// cliSetupCmd writes a ready-to-use Claude Code hooks config into the current
// project's .claude/settings.json (merging with existing content if present).
// PRD §9.1: SessionStart injects context, Stop captures a summary.
func cliSetupCmd(args []string) int {
	return cliSetupTo(args)
}

func cliSetupTo(args []string) int {
	fs := flag.NewFlagSet("setup", flag.ExitOnError)
	dbPath := fs.String("db", defaultDBPath(), "path to the SQLite memory database (written into the hook commands)")
	project := fs.String("project", "", "project namespace for hooks; empty = derive from git? use global")
	target := fs.String("out", ".claude/settings.json", "path to write the Claude Code settings")
	fs.Parse(args)

	bin, err := os.Executable()
	if err != nil {
		fmt.Fprintf(os.Stderr, "setup: %v\n", err)
		return 1
	}
	absBin, err := filepath.Abs(bin)
	if err != nil {
		absBin = bin
	}

	proj := *project
	if proj == "" {
		proj = "global"
	}

	// Shell-quote every interpolated value: paths and names may contain
	// spaces, which would otherwise break the hook command line.
	cfg := claudeSettings{
		Hooks: claudeHooks{
			SessionStart: []claudeHookCmd{{Command: shellJoin(absBin, "project_context", "-db", *dbPath, "-project", proj)}},
			Stop:         []claudeHookCmd{{Command: shellJoin(absBin, "capture", "-db", *dbPath, "-project", proj, "-source", "claude-code")}},
		},
	}

	if err := os.MkdirAll(filepath.Dir(*target), 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "setup: %v\n", err)
		return 1
	}
	if err := writeClaudeSettings(*target, cfg); err != nil {
		fmt.Fprintf(os.Stderr, "setup: %v\n", err)
		return 1
	}
	fmt.Printf("wrote %s\n", *target)
	return 0
}

// shellJoin quotes each argument for /bin/sh and joins them with spaces.
func shellJoin(args ...string) string {
	quoted := make([]string, 0, len(args))
	for _, a := range args {
		quoted = append(quoted, "'"+strings.ReplaceAll(a, "'", "'\\''")+"'")
	}
	return strings.Join(quoted, " ")
}

// claudeSettings mirrors the Claude Code settings.json hooks shape.
type claudeSettings struct {
	Hooks claudeHooks `json:"hooks"`
}

type claudeHooks struct {
	SessionStart []claudeHookCmd `json:"SessionStart,omitempty"`
	Stop         []claudeHookCmd `json:"Stop,omitempty"`
}

type claudeHookCmd struct {
	Command string `json:"command"`
}

// writeClaudeSettings writes settings JSON, preserving unknown top-level
// fields and MERGING with existing hooks (e.g. a user's PreToolUse hook must
// survive a second `forgetmenot setup` run).
func writeClaudeSettings(path string, cfg claudeSettings) error {
	existing := map[string]any{}
	if b, err := os.ReadFile(path); err == nil {
		if err := json.Unmarshal(b, &existing); err != nil {
			return fmt.Errorf("existing settings not valid JSON: %w", err)
		}
	}

	// Keep every existing hook event; append our commands to SessionStart
	// and Stop, and preserve all other hook types untouched.
	merged := map[string]any{}
	if cur, ok := existing["hooks"].(map[string]any); ok {
		for k, v := range cur {
			merged[k] = v
		}
	}
	merged["SessionStart"] = appendHookCmds(existing, "SessionStart", cfg.Hooks.SessionStart...)
	merged["Stop"] = appendHookCmds(existing, "Stop", cfg.Hooks.Stop...)

	existing["hooks"] = merged
	b, err := json.MarshalIndent(existing, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o644)
}

// appendHookCmds returns the existing commands for a hook event plus extra
// ones, preserving order.
func appendHookCmds(settings map[string]any, event string, extra ...claudeHookCmd) []claudeHookCmd {
	var out []claudeHookCmd
	if cur, ok := settings["hooks"].(map[string]any); ok {
		if v, ok := cur[event].([]any); ok {
			for _, c := range v {
				if m, ok := c.(map[string]any); ok {
					if s, ok := m["command"].(string); ok {
						out = append(out, claudeHookCmd{Command: s})
					}
				}
			}
		}
	}
	return append(out, extra...)
}
