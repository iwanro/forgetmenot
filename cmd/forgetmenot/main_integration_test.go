package main

import (
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// fakeEmbedServer serves OpenAI-compatible /v1/embeddings with deterministic
// bag-of-words vectors, so the integration test needs no Ollama. Overlapping
// words produce overlapping vectors, which is enough to exercise recall.
func fakeEmbedServer() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/embeddings" {
			http.NotFound(w, r)
			return
		}
		var req struct {
			Model string   `json:"model"`
			Input []string `json:"input"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		data := make([]map[string]any, 0, len(req.Input))
		for _, text := range req.Input {
			data = append(data, map[string]any{"embedding": bowVector(text)})
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"data": data})
	}))
}

// bowVector builds a 64-dim count vector hashing each word, then normalizes.
func bowVector(text string) []float64 {
	const dim = 64
	v := make([]float64, dim)
	for _, w := range strings.Fields(strings.ToLower(text)) {
		h := fnv.New32a()
		h.Write([]byte(w))
		v[h.Sum32()%dim]++
	}
	var norm float64
	for _, x := range v {
		norm += x * x
	}
	norm = math.Sqrt(norm)
	if norm == 0 {
		return v
	}
	for i := range v {
		v[i] /= norm
	}
	return v
}

func TestMCPServerEndToEnd(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in -short mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	// Compile the real binary once.
	tmp := t.TempDir()
	bin := filepath.Join(tmp, "forgetmenot")
	build := exec.Command("go", "build", "-o", bin, "./cmd/forgetmenot")
	build.Dir = "../.."
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}

	emb := fakeEmbedServer()
	defer emb.Close()

	db := filepath.Join(tmp, "mem.db")
	cmd := exec.Command(bin, "-db", db,
		"-embed", "openai", "-embed-url", emb.URL+"/v1", "-embed-model", "test-model", "-embed-api-key", "test")
	cmd.Env = append(os.Environ(), "XDG_DATA_HOME="+tmp)

	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "v1"}, nil)
	sess, err := client.Connect(ctx, &mcp.CommandTransport{Command: cmd}, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer sess.Close()

	// 1. List tools.
	tools, err := sess.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	if len(tools.Tools) != 9 {
		t.Fatalf("want 9 tools, got %d: %+v", len(tools.Tools), tools.Tools)
	}

	// 2. Remember.
	res, err := sess.CallTool(ctx, &mcp.CallToolParams{
		Name: "memory.remember",
		Arguments: map[string]any{
			"content": "backend is FastAPI on Python 3.12",
			"type":    "fact",
			"project": "repo-a",
		},
	})
	if err != nil {
		t.Fatalf("remember: %v", err)
	}
	if res.IsError {
		t.Fatalf("remember returned error: %s", resultText(res))
	}
	var remOut struct {
		ID    string `json:"id"`
		IsNew bool   `json:"is_new"`
	}
	if err := decodeStructured(res, &remOut); err != nil {
		t.Fatalf("decode remember output: %v (raw=%s)", err, resultText(res))
	}
	if !remOut.IsNew || remOut.ID == "" {
		t.Fatalf("expected new memory, got %+v", remOut)
	}

	// 3. Recall should find it.
	res, err = sess.CallTool(ctx, &mcp.CallToolParams{
		Name: "memory.recall",
		Arguments: map[string]any{
			"query":   "backend is FastAPI",
			"project": "repo-a",
			"limit":   5,
		},
	})
	if err != nil {
		t.Fatalf("recall: %v", err)
	}
	if res.IsError {
		t.Fatalf("recall returned error: %s", resultText(res))
	}
	var recOut struct {
		Hits []struct {
			ID      string  `json:"id"`
			Content string  `json:"content"`
			Score   float64 `json:"score"`
		} `json:"hits"`
	}
	if err := decodeStructured(res, &recOut); err != nil {
		t.Fatalf("decode recall output: %v (raw=%s)", err, resultText(res))
	}
	if len(recOut.Hits) == 0 {
		t.Fatalf("expected hits, got none (raw=%s)", resultText(res))
	}
	if !strings.Contains(recOut.Hits[0].Content, "FastAPI") {
		t.Fatalf("top hit should mention FastAPI, got %q", recOut.Hits[0].Content)
	}

	// 4. Stats.
	res, err = sess.CallTool(ctx, &mcp.CallToolParams{Name: "memory.stats", Arguments: map[string]any{}})
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	var statsOut struct {
		Count int `json:"count"`
	}
	if err := decodeStructured(res, &statsOut); err != nil {
		t.Fatalf("decode stats: %v", err)
	}
	if statsOut.Count != 1 {
		t.Fatalf("count = %d, want 1", statsOut.Count)
	}

	// 5. Forget, then recall again -> no hits.
	_, err = sess.CallTool(ctx, &mcp.CallToolParams{
		Name: "memory.forget", Arguments: map[string]any{"id": remOut.ID},
	})
	if err != nil {
		t.Fatalf("forget: %v", err)
	}
	res, err = sess.CallTool(ctx, &mcp.CallToolParams{
		Name:      "memory.recall",
		Arguments: map[string]any{"query": "backend is FastAPI", "project": "repo-a", "limit": 5},
	})
	if err != nil {
		t.Fatalf("recall after forget: %v", err)
	}
	if err := decodeStructured(res, &recOut); err != nil {
		t.Fatalf("decode recall: %v", err)
	}
	if len(recOut.Hits) != 0 {
		t.Fatalf("expected 0 hits after forget, got %d", len(recOut.Hits))
	}
}

func resultText(res *mcp.CallToolResult) string {
	var sb strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			sb.WriteString(tc.Text)
		}
	}
	return sb.String()
}

// decodeStructured prefers StructuredContent; falls back to TextContent JSON.
func decodeStructured(res *mcp.CallToolResult, out any) error {
	if res.StructuredContent != nil {
		b, err := json.Marshal(res.StructuredContent)
		if err != nil {
			return err
		}
		return json.Unmarshal(b, out)
	}
	text := strings.TrimSpace(resultText(res))
	if text == "" {
		return fmt.Errorf("no content in result")
	}
	return json.Unmarshal([]byte(text), out)
}
