// Package eval provides a small evaluation harness for the memory service:
// a dataset of queries with expected answers and a runner that measures
// recall@k. See PRD §11 and M1 milestone.
package eval

import (
	"context"
	"fmt"
	"strings"

	"github.com/iwanro/forgetmenot/internal/memory"
)

// Case is one evaluation question: recall(query) should surface the memory
// whose content contains Expected.
type Case struct {
	Query    string
	Project  string
	Expected string // substring expected to appear in the top-k results
	K        int    // top-k considered; 0 defaults to 3
}

// Result aggregates a run over the dataset.
type Result struct {
	Total     int
	Passed    int
	RecallAtK float64 // passed/total
	Failures  []Failure
	Errors    []string // recall errors encountered during the run
}

// Failure records a single missed case.
type Failure struct {
	Query    string
	Expected string
	Got      []string
}

// Run evaluates the service against the dataset. It does not mutate state
// beyond what recall does (access bumps).
func Run(ctx context.Context, svc *memory.Service, cases []Case) Result {
	var res Result
	for _, c := range cases {
		k := c.K
		if k <= 0 {
			k = 3
		}
		hits, err := svc.Recall(ctx, memory.RecallInput{
			Query:   c.Query,
			Project: c.Project,
			Limit:   k,
		})
		res.Total++
		if err != nil {
			// A recall error is a real failure signal, not a silent miss.
			res.Errors = append(res.Errors, fmt.Sprintf("%s: %v", c.Query, err))
			continue
		}
		ok := false
		got := make([]string, 0, len(hits))
		for _, h := range hits {
			got = append(got, h.Memory.Content)
			if strings.Contains(strings.ToLower(h.Memory.Content), strings.ToLower(c.Expected)) {
				ok = true
			}
		}
		if ok {
			res.Passed++
		} else {
			res.Failures = append(res.Failures, Failure{Query: c.Query, Expected: c.Expected, Got: got})
		}
	}
	if res.Total > 0 {
		res.RecallAtK = float64(res.Passed) / float64(res.Total)
	}
	return res
}

// String renders the result as a human-readable summary.
func (r Result) String() string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "recall@k: %.1f%% (%d/%d)\n", r.RecallAtK*100, r.Passed, r.Total)
	if len(r.Failures) > 0 {
		sb.WriteString("failures:\n")
		for _, f := range r.Failures {
			fmt.Fprintf(&sb, "  - query: %q\n    expected: %q\n    got: %v\n", f.Query, f.Expected, f.Got)
		}
	}
	return sb.String()
}

// DefaultDataset is a small, self-contained set of 20 queries about a
// fictional project, used by `forgetmenot eval` and tests. Queries share
// keyword overlap with the seeded facts so that a decent embedder (including
// the deterministic bag-of-words one used in tests) can retrieve them.
var DefaultDataset = []Case{
	{Query: "what framework is the backend built on", Expected: "FastAPI", Project: "demo"},
	{Query: "which database postgres version", Expected: "Postgres", Project: "demo"},
	{Query: "how do tests run with pytest", Expected: "pytest", Project: "demo"},
	{Query: "api container base image python", Expected: "python:3.12-slim", Project: "demo"},
	{Query: "who owns the mobile app alex", Expected: "Alex", Project: "demo"},
	{Query: "is the mobile app flutter", Expected: "Flutter", Project: "demo"},
	{Query: "where do api secrets live environment variables", Expected: "environment variables", Project: "demo"},
	{Query: "which tool deploys github actions", Expected: "GitHub Actions", Project: "demo"},
	{Query: "redis cache layer", Expected: "Redis", Project: "demo"},
	{Query: "staging environment url example.com", Expected: "staging.example.com", Project: "demo"},
	{Query: "python linter ruff", Expected: "ruff", Project: "demo"},
	{Query: "database migrations alembic", Expected: "alembic", Project: "demo"},
	{Query: "background jobs celery queue", Expected: "Celery", Project: "demo"},
	{Query: "frontend react typescript", Expected: "React", Project: "demo"},
	{Query: "design system owner maria", Expected: "Maria", Project: "demo"},
	{Query: "api versioning semantic versioning", Expected: "semantic versioning", Project: "demo"},
	{Query: "bugs tracked github issues", Expected: "GitHub Issues", Project: "demo"},
	{Query: "default branch main", Expected: "main", Project: "demo"},
	{Query: "node package manager pnpm", Expected: "pnpm", Project: "demo"},
	{Query: "logs collected opentelemetry", Expected: "OpenTelemetry", Project: "demo"},
}

// SeedDataset populates a store with the facts the DefaultDataset queries
// reference. Idempotent per project: if the demo project already has
// memories, seeding is skipped. Returns the number of inserted memories.
func SeedDataset(ctx context.Context, svc *memory.Service) (int, error) {
	if n, err := svc.Store.Count(ctx, "demo"); err != nil {
		return 0, err
	} else if n > 0 {
		return 0, nil
	}
	facts := []string{
		"backend is FastAPI on Python 3.12",
		"database is Postgres 16",
		"tests run with pytest",
		"api container base image is python:3.12-slim",
		"mobile app is owned by Alex",
		"mobile app is built with Flutter",
		"API secrets live in environment variables",
		"deployment is automated with GitHub Actions",
		"cache layer is Redis",
		"staging environment is at staging.example.com",
		"python linter is ruff",
		"database migrations use alembic",
		"background jobs run on Celery",
		"frontend is React with TypeScript",
		"design system is owned by Maria",
		"api versioning uses semantic versioning",
		"bugs are tracked in GitHub Issues",
		"default branch is main",
		"node packages are managed with pnpm",
		"logs are collected with OpenTelemetry",
	}
	for _, f := range facts {
		_, _, err := svc.Remember(ctx, memory.RememberInput{
			Content: f, Type: memory.TypeFact, Project: "demo",
		})
		if err != nil {
			return 0, err
		}
	}
	return len(facts), nil
}
