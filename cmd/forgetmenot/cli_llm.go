package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/iwanro/forgetmenot/internal/embed"
	"github.com/iwanro/forgetmenot/internal/llm"
	"github.com/iwanro/forgetmenot/internal/memory"
)

// buildLLM constructs the optional chat client from CLI flags.
func buildLLM(kind, url, model, apiKey string) llm.Client {
	switch kind {
	case "ollama":
		return llm.NewOllama(url, model)
	case "openai":
		return llm.NewOpenAICompat(url, apiKey, model)
	default:
		return nil
	}
}

// cliSummarizeCmd compresses stale episodes into a context summary using the
// configured LLM. PRD v0.3: memory compression.
func cliSummarizeCmd(args []string) int {
	fs := flag.NewFlagSet("summarize", flag.ExitOnError)
	dbPath := fs.String("db", defaultDBPath(), "path to the SQLite database")
	project := fs.String("project", "global", "project namespace")
	olderThan := fs.Duration("older-than", 7*24*time.Hour, "summarize episodes not accessed for this long")
	llmKind := fs.String("llm", "ollama", "chat provider: ollama | openai")
	llmURL := fs.String("llm-url", "", "chat endpoint base URL")
	llmModel := fs.String("llm-model", "", "chat model name")
	llmKey := fs.String("llm-api-key", "", "API key for openai provider")
	fs.Parse(args)

	store := openStoreOrDie(*dbPath)
	defer store.Close()
	svc := memory.NewService(store, nil)
	svc.SetDBPath(*dbPath)
	svc.LLM = buildLLM(*llmKind, *llmURL, *llmModel, *llmKey)
	if svc.LLM == nil {
		fmt.Fprintln(os.Stderr, "summarize: unknown -llm provider (want ollama or openai)")
		return 2
	}

	sum, err := svc.SummarizeProject(context.Background(), *project, *olderThan)
	if err != nil {
		fmt.Fprintf(os.Stderr, "summarize: %v\n", err)
		return 1
	}
	fmt.Printf("summarized %s:\n\n%s\n", *project, sum)
	return 0
}

// cliDoctorCmd diagnoses the local setup: DB, embeddings endpoint, hooks.
func cliDoctorCmd(args []string) int {
	fs := flag.NewFlagSet("doctor", flag.ExitOnError)
	dbPath := fs.String("db", defaultDBPath(), "path to the SQLite database")
	embedURL := fs.String("embed-url", "", "embedding endpoint base URL (default Ollama localhost:11434)")
	fs.Parse(args)

	ok := true
	check := func(name string, err error) {
		if err != nil {
			fmt.Printf("[FAIL] %s: %v\n", name, err)
			ok = false
		} else {
			fmt.Printf("[ok]   %s\n", name)
		}
	}

	// DB opens and migrates.
	store, err := memory.NewSQLiteStore(*dbPath)
	if err != nil {
		check("database "+*dbPath, err)
		fmt.Println("\ndoctor: 1 failure - fix and re-run.")
		return 1
	}
	defer store.Close()
	check("database "+*dbPath, nil)

	// Embeddings endpoint reachable. We only probe reachability, not a
	// specific model: Ollama's /api/tags answers with the installed models
	// even when nomic-embed-text is not pulled, so this avoids false FAILs.
	check("embedding endpoint", checkEmbedReachable(*embedURL))

	// Current session marker.
	svc := memory.NewService(store, nil)
	svc.SetDBPath(*dbPath)
	if sid := svc.CurrentSessionID(); sid != "" {
		fmt.Printf("[info] active session: %s\n", sid)
	} else {
		fmt.Println("[info] no active session (hooks will start one)")
	}

	// Hooks config presence (Claude Code).
	_, err = os.Stat(".claude/settings.json")
	if err == nil {
		fmt.Println("[ok]   .claude/settings.json present (hooks configured)")
	} else {
		fmt.Println("[warn] .claude/settings.json missing - run `forgetmenot setup`")
	}

	if !ok {
		fmt.Println("\ndoctor: some checks failed.")
		return 1
	}
	fmt.Println("\ndoctor: all checks passed.")
	return 0
}

// checkEmbedReachable probes the Ollama /api/tags endpoint to confirm the
// embedding service is up, independent of which models are installed.
func checkEmbedReachable(baseURL string) error {
	em := embed.NewOllama(baseURL, "unused-model") // model irrelevant for reachability
	req, err := http.NewRequest(http.MethodGet, em.BaseURL+"/api/tags", nil)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	resp, err := http.DefaultClient.Do(req.WithContext(ctx))
	if err != nil {
		return fmt.Errorf("endpoint not reachable: %w", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("endpoint returned status %d", resp.StatusCode)
	}
	return nil
}
