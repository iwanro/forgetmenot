// Package mcpserver wires the core memory Service to the MCP protocol: it
// registers the memory.* tools and runs the server over stdio.
package mcpserver

import (
	"context"
	"log"

	"github.com/iwan/agent-memory/internal/memory"
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
		opts.Name = "agent-memory"
	}
	if opts.Version == "" {
		opts.Version = "v0.1.0"
	}
	if opts.DefaultType == "" {
		opts.DefaultType = string(memory.TypeFact)
	}

	server := mcp.NewServer(&mcp.Implementation{Name: opts.Name, Version: opts.Version}, nil)

	addRememberTool(server, svc, opts)
	addRecallTool(server, svc, opts)
	addForgetTool(server, svc)
	addUpdateTool(server, svc)
	addStatsTool(server, svc)

	log.Printf("%s %s: %d memory tools registered", opts.Name, opts.Version, 5)
	return server.Run(ctx, &mcp.StdioTransport{})
}

// --- memory.remember ------------------------------------------------------

type rememberIn struct {
	Content    string            `json:"content" jsonschema:"the memory content to store"`
	Type       string            `json:"type,omitempty" jsonschema:"one of: fact, preference, decision, entity, context, episode"`
	Project    string            `json:"project,omitempty" jsonschema:"project namespace; defaults to global"`
	Importance float64           `json:"importance,omitempty" jsonschema:"importance 0-1; defaults to 0.5"`
	Source     string            `json:"source,omitempty" jsonschema:"where this memory came from"`
	Metadata   map[string]string `json:"metadata,omitempty" jsonschema:"free-form key/value metadata"`
}

type rememberOut struct {
	ID           string `json:"id"`
	Type         string `json:"type"`
	Project      string `json:"project"`
	IsNew        bool   `json:"is_new"`
	ReinforcedID string `json:"reinforced_id,omitempty"`
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
			Metadata:   in.Metadata,
		})
		if err != nil {
			return nil, rememberOut{}, err
		}
		out := rememberOut{ID: id, Type: typ, Project: in.Project, IsNew: isNew}
		if !isNew {
			out.ReinforcedID = id
			return &mcp.CallToolResult{}, out, nil
		}
		return nil, out, nil
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
	Score      float64 `json:"score"`
}

type recallOut struct {
	Query string      `json:"query"`
	Hits  []recallHit `json:"hits"`
}

func addRecallTool(server *mcp.Server, svc *memory.Service, opts Options) {
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
	Metadata   map[string]string `json:"metadata,omitempty" jsonschema:"metadata entries to merge"`
}

type updateOut struct {
	ID      string `json:"id"`
	Updated bool   `json:"updated"`
}

func addUpdateTool(server *mcp.Server, svc *memory.Service) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "memory.update",
		Description: "Update a memory's content, type, project, importance or metadata.",
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

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
