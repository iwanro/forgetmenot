// Package mcpserver wires the core memory Service to the MCP protocol: it
// registers the memory.* tools and runs the server over stdio.
package mcpserver

import (
	"context"
	"log"

	"github.com/iwanro/forgetmenot/internal/memory"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Options configures the MCP server.
type Options struct {
	Name        string
	Version     string
	DefaultType string
	DefaultSrc  string
}

// Run starts an MCP server over stdio with the given memory service and
// blocks until the client disconnects.
func Run(ctx context.Context, svc *memory.Service, opts Options) error {
	if opts.Name == "" {
		opts.Name = "forgetmenot"
	}
	if opts.Version == "" {
		opts.Version = "v0.1.0"
	}
	if opts.DefaultType == "" {
		opts.DefaultType = string(memory.TypeFact)
	}

	server := mcp.NewServer(&mcp.Implementation{Name: opts.Name, Version: opts.Version}, nil)

	addRememberTool(server, svc, opts)
	addRecallTool(server, svc)
	addForgetTool(server, svc)
	addUpdateTool(server, svc)
	addLinkTool(server, svc)
	addConflictsTool(server, svc)
	addResolveConflictTool(server, svc)
	addStatsTool(server, svc)
	addTimelineTool(server, svc)

	log.Printf("%s %s: %d memory tools registered", opts.Name, opts.Version, 9)
	return server.Run(ctx, &mcp.StdioTransport{})
}

// --- memory.remember ------------------------------------------------------

type rememberIn struct {
	Content    string            `json:"content" jsonschema:"the memory content to store"`
	Type       string            `json:"type,omitempty" jsonschema:"one of: fact, preference, decision, entity, context, episode"`
	Project    string            `json:"project,omitempty" jsonschema:"project namespace; defaults to global"`
	Importance float64           `json:"importance,omitempty" jsonschema:"importance 0-1; defaults to 0.5"`
	Source     string            `json:"source,omitempty" jsonschema:"where this memory came from"`
	Trust      string            `json:"trust,omitempty" jsonschema:"trust level: high (default) or low for untrusted/external content"`
	SessionID  string            `json:"session_id,omitempty" jsonschema:"session to attach this memory to; defaults to current session"`
	Topics     []string          `json:"topics,omitempty" jsonschema:"topic labels for cross-session correlation"`
	Metadata   map[string]string `json:"metadata,omitempty" jsonschema:"free-form key/value metadata"`
}

type rememberOut struct {
	ID      string `json:"id"`
	Type    string `json:"type"`
	Project string `json:"project"`
	IsNew   bool   `json:"is_new"`
}

func addRememberTool(server *mcp.Server, svc *memory.Service, opts Options) {
	mcp.AddTool(server, &mcp.Tool{
		Name: "memory.remember",
		Description: "Store a memory that the agent should remember across sessions. " +
			"Use for facts, decisions, preferences, entities, project context and episodes. " +
			"If a near-duplicate already exists in the same project, the existing memory is reinforced instead of duplicated.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in rememberIn) (*mcp.CallToolResult, rememberOut, error) {
		typ := opts.DefaultType
		if in.Type != "" {
			typ = in.Type
		}
		id, isNew, err := svc.Remember(ctx, memory.RememberInput{
			Content:    in.Content,
			Type:       memory.Type(typ),
			Project:    in.Project,
			Importance: in.Importance,
			Source:     firstNonEmpty(in.Source, opts.DefaultSrc),
			Trust:      memory.Trust(in.Trust),
			SessionID:  in.SessionID,
			Topics:     in.Topics,
			Metadata:   in.Metadata,
		})
		if err != nil {
			return nil, rememberOut{}, err
		}
		return nil, rememberOut{ID: id, Type: typ, Project: in.Project, IsNew: isNew}, nil
	})
}

// --- memory.recall --------------------------------------------------------

type recallIn struct {
	Query   string `json:"query" jsonschema:"what to search for"`
	Project string `json:"project,omitempty" jsonschema:"restrict to a project; empty searches everything"`
	Type    string `json:"type,omitempty" jsonschema:"restrict to a memory type"`
	Limit   int    `json:"limit,omitempty" jsonschema:"max results; defaults to 10"`
}

type recallHit struct {
	ID         string  `json:"id"`
	Content    string  `json:"content"`
	Type       string  `json:"type"`
	Project    string  `json:"project"`
	Importance float64 `json:"importance"`
	Source     string  `json:"source,omitempty"`
	Trust      string  `json:"trust,omitempty"`
	Score      float64 `json:"score"`
}

type recallOut struct {
	Query string      `json:"query"`
	Hits  []recallHit `json:"hits"`
}

func addRecallTool(server *mcp.Server, svc *memory.Service) {
	mcp.AddTool(server, &mcp.Tool{
		Name: "memory.recall",
		Description: "Search memory semantically. Returns the most relevant memories for the query, " +
			"with their similarity score. Use this before asking the user to re-explain context.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in recallIn) (*mcp.CallToolResult, recallOut, error) {
		results, err := svc.Recall(ctx, memory.RecallInput{
			Query:   in.Query,
			Project: in.Project,
			Type:    memory.Type(in.Type),
			Limit:   in.Limit,
		})
		if err != nil {
			return nil, recallOut{}, err
		}
		out := recallOut{Query: in.Query, Hits: make([]recallHit, 0, len(results))}
		for _, r := range results {
			out.Hits = append(out.Hits, recallHit{
				ID:         r.Memory.ID,
				Content:    r.Memory.Content,
				Type:       string(r.Memory.Type),
				Project:    r.Memory.Project,
				Importance: r.Memory.Importance,
				Source:     r.Memory.Source,
				Trust:      string(r.Memory.Trust),
				Score:      r.Score,
			})
		}
		return nil, out, nil
	})
}

// --- memory.forget --------------------------------------------------------

type forgetIn struct {
	ID string `json:"id" jsonschema:"the memory id to delete"`
}

type forgetOut struct {
	Deleted bool   `json:"deleted"`
	ID      string `json:"id"`
}

func addForgetTool(server *mcp.Server, svc *memory.Service) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "memory.forget",
		Description: "Permanently delete a memory by id.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in forgetIn) (*mcp.CallToolResult, forgetOut, error) {
		if err := svc.Forget(ctx, in.ID); err != nil {
			if err == memory.ErrNotFound {
				return nil, forgetOut{Deleted: false, ID: in.ID}, nil
			}
			return nil, forgetOut{}, err
		}
		return nil, forgetOut{Deleted: true, ID: in.ID}, nil
	})
}

// --- memory.update --------------------------------------------------------

type updateIn struct {
	ID         string            `json:"id" jsonschema:"the memory id to update"`
	Content    *string           `json:"content,omitempty" jsonschema:"new content"`
	Type       *string           `json:"type,omitempty" jsonschema:"new memory type"`
	Project    *string           `json:"project,omitempty" jsonschema:"new project namespace"`
	Importance *float64          `json:"importance,omitempty" jsonschema:"new importance 0-1"`
	Trust      *string           `json:"trust,omitempty" jsonschema:"new trust level: high or low"`
	Metadata   map[string]string `json:"metadata,omitempty" jsonschema:"metadata entries to merge"`
}

type updateOut struct {
	ID      string `json:"id"`
	Updated bool   `json:"updated"`
}

func addUpdateTool(server *mcp.Server, svc *memory.Service) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "memory.update",
		Description: "Update a memory's content, type, project, importance, trust or metadata.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in updateIn) (*mcp.CallToolResult, updateOut, error) {
		patch := memory.UpdatePatch{Metadata: in.Metadata}
		if in.Content != nil {
			patch.Content = in.Content
		}
		if in.Type != nil {
			t := memory.Type(*in.Type)
			patch.Type = &t
		}
		if in.Project != nil {
			patch.Project = in.Project
		}
		if in.Importance != nil {
			patch.Importance = in.Importance
		}
		if in.Trust != nil {
			t := memory.Trust(*in.Trust)
			patch.Trust = &t
		}
		if err := svc.Update(ctx, in.ID, patch); err != nil {
			if err == memory.ErrNotFound {
				return nil, updateOut{ID: in.ID, Updated: false}, nil
			}
			return nil, updateOut{}, err
		}
		return nil, updateOut{ID: in.ID, Updated: true}, nil
	})
}

// --- memory.stats ---------------------------------------------------------

type statsOut struct {
	Count int `json:"count"`
}

func addStatsTool(server *mcp.Server, svc *memory.Service) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "memory.stats",
		Description: "Return basic memory statistics.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ any) (*mcp.CallToolResult, statsOut, error) {
		s, err := svc.Stats(ctx)
		if err != nil {
			return nil, statsOut{}, err
		}
		return nil, statsOut{Count: s.Count}, nil
	})
}

// --- memory.link -----------------------------------------------------------

type linkIn struct {
	FromID string `json:"from_id" jsonschema:"the source memory id"`
	ToID   string `json:"to_id" jsonschema:"the target memory id"`
	Kind   string `json:"kind,omitempty" jsonschema:"one of: related, supersedes, part_of; defaults to related"`
}

type linkOut struct {
	Linked bool   `json:"linked"`
	FromID string `json:"from_id"`
	ToID   string `json:"to_id"`
	Kind   string `json:"kind"`
}

func addLinkTool(server *mcp.Server, svc *memory.Service) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "memory.link",
		Description: "Create a relation between two memories: related, supersedes (from is replaced by to) or part_of.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in linkIn) (*mcp.CallToolResult, linkOut, error) {
		kind := in.Kind
		if kind == "" {
			kind = string(memory.RelationRelated)
		}
		if err := svc.Link(ctx, in.FromID, in.ToID, kind); err != nil {
			return nil, linkOut{}, err
		}
		return nil, linkOut{Linked: true, FromID: in.FromID, ToID: in.ToID, Kind: kind}, nil
	})
}

// --- memory.conflicts ------------------------------------------------------

type conflictHit struct {
	ID        string `json:"id"`
	MemoryA   string `json:"memory_a"`
	MemoryB   string `json:"memory_b"`
	ContentA  string `json:"content_a"`
	ContentB  string `json:"content_b"`
	CreatedAt string `json:"created_at"`
}

type conflictsOut struct {
	Conflicts []conflictHit `json:"conflicts"`
}

func addConflictsTool(server *mcp.Server, svc *memory.Service) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "memory.conflicts",
		Description: "List open memory conflicts (contradictory facts detected at remember time). Resolve them with memory.resolve_conflict.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ any) (*mcp.CallToolResult, conflictsOut, error) {
		conflicts, err := svc.Conflicts(ctx)
		if err != nil {
			return nil, conflictsOut{}, err
		}
		// Load all memories once to avoid N+1 lookups per conflict.
		content := map[string]string{}
		if mems, _, err := svc.Store.All(ctx, ""); err == nil {
			for _, m := range mems {
				content[m.ID] = m.Content
			}
		}
		out := conflictsOut{Conflicts: make([]conflictHit, 0, len(conflicts))}
		for _, c := range conflicts {
			out.Conflicts = append(out.Conflicts, conflictHit{
				ID:        c.ID,
				MemoryA:   c.MemoryA,
				MemoryB:   c.MemoryB,
				ContentA:  content[c.MemoryA],
				ContentB:  content[c.MemoryB],
				CreatedAt: c.CreatedAt.UTC().Format("2006-01-02T15:04:05Z"),
			})
		}
		return nil, out, nil
	})
}

// --- memory.resolve_conflict -----------------------------------------------

type resolveIn struct {
	ConflictID string `json:"conflict_id" jsonschema:"the conflict id to resolve"`
	WinnerID   string `json:"winner_id" jsonschema:"the memory id that wins; the loser is marked superseded"`
}

type resolveOut struct {
	Resolved bool   `json:"resolved"`
	WinnerID string `json:"winner_id"`
}

func addResolveConflictTool(server *mcp.Server, svc *memory.Service) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "memory.resolve_conflict",
		Description: "Resolve an open memory conflict by choosing the winning memory. The loser is automatically marked as superseded by the winner.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in resolveIn) (*mcp.CallToolResult, resolveOut, error) {
		if err := svc.ResolveConflict(ctx, in.ConflictID, in.WinnerID); err != nil {
			if err == memory.ErrNotFound {
				return nil, resolveOut{Resolved: false, WinnerID: in.WinnerID}, nil
			}
			return nil, resolveOut{}, err
		}
		return nil, resolveOut{Resolved: true, WinnerID: in.WinnerID}, nil
	})
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// --- memory.timeline -------------------------------------------------------

type timelineIn struct {
	Project string `json:"project,omitempty" jsonschema:"project namespace; defaults to global"`
	Topic   string `json:"topic,omitempty" jsonschema:"topic to trace across sessions; empty = all memories"`
	Limit   int    `json:"limit,omitempty" jsonschema:"max entries; defaults to 50"`
}

type timelineEntry struct {
	Content   string `json:"content"`
	Type      string `json:"type"`
	CreatedAt string `json:"created_at"`
	Source    string `json:"source,omitempty"`
	Trust     string `json:"trust,omitempty"`
	SessionID string `json:"session_id,omitempty"`
	SessionAt string `json:"session_started_at,omitempty"`
}

type timelineOut struct {
	Entries []timelineEntry `json:"entries"`
}

func addTimelineTool(server *mcp.Server, svc *memory.Service) {
	mcp.AddTool(server, &mcp.Tool{
		Name: "memory.timeline",
		Description: "Show how a topic evolved across sessions: all memories about it, oldest first, with session context. " +
			"Use this to correlate what was discussed or decided about a subject over time.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in timelineIn) (*mcp.CallToolResult, timelineOut, error) {
		project := in.Project
		if project == "" {
			project = memory.DefaultProject
		}
		entries, err := svc.Timeline(ctx, project, in.Topic, in.Limit)
		if err != nil {
			return nil, timelineOut{}, err
		}
		out := timelineOut{Entries: make([]timelineEntry, 0, len(entries))}
		for _, e := range entries {
			t := timelineEntry{
				Content:   e.Memory.Content,
				Type:      string(e.Memory.Type),
				CreatedAt: e.Memory.CreatedAt.UTC().Format("2006-01-02T15:04:05Z"),
				Source:    e.Memory.Source,
				Trust:     string(e.Memory.Trust),
				SessionID: e.Memory.SessionID,
			}
			if e.Session != nil {
				t.SessionAt = e.Session.StartedAt.UTC().Format("2006-01-02T15:04:05Z")
			}
			out.Entries = append(out.Entries, t)
		}
		return nil, out, nil
	})
}
