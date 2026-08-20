package tools

import (
	"context"
	"encoding/json"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"qazyna/internal/embed"
	"qazyna/internal/mcpserver"
	"qazyna/internal/search"
	"qazyna/internal/store"
)

type searchNotesArgs struct {
	Query string `json:"query" jsonschema:"What to look for. Full questions work well (semantic search), exact identifiers too (keyword search)."`
	Limit int    `json:"limit,omitempty" jsonschema:"Maximum results to return, 1-50. Default 5."`
	Mode  string `json:"mode,omitempty" jsonschema:"hybrid (default, recommended), vector (meaning only) or text (exact words only)."`
}

// SearchNotes registers the search_notes tool: hybrid search over the
// user's file index.
func SearchNotes(st store.Store, emb embed.Embedder) mcpserver.Option {
	return func(server *mcp.Server) error {
		mcp.AddTool(server, &mcp.Tool{
			Name: "search_notes",
			Description: "Search the user's personal file index (notes, articles, PDFs, spreadsheets) " +
				"by meaning and by exact words. Returns matching chunks with the file path, section, " +
				"text and a relevance score (1.0 = strongest). To read more context, open the returned " +
				"path; ordinal is the chunk's position within that file.",
		}, func(ctx context.Context, req *mcp.CallToolRequest, args searchNotesArgs) (*mcp.CallToolResult, any, error) {
			if args.Limit <= 0 {
				args.Limit = 5
			}
			args.Limit = min(args.Limit, 50)

			results, err := search.Search(ctx, st, emb, args.Query, search.Options{
				Mode:  args.Mode,
				Limit: args.Limit,
			})
			if err != nil {
				return nil, nil, err
			}

			payload, err := json.Marshal(results)
			if err != nil {
				return nil, nil, err
			}
			return &mcp.CallToolResult{
				Content: []mcp.Content{&mcp.TextContent{Text: string(payload)}},
			}, nil, nil
		})
		return nil
	}
}
