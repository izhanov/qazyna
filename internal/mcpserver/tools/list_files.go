package tools

import (
	"context"
	"encoding/json"
	"sort"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"qazyna/internal/mcpserver"
	"qazyna/internal/store"
)

type indexedFile struct {
	Path     string `json:"path"`
	Modified string `json:"modified"`
}

// ListFiles registers the list_files tool: an inventory of what the index
// actually covers.
func ListFiles(st store.Store) mcpserver.Option {
	return func(server *mcp.Server) error {
		mcp.AddTool(server, &mcp.Tool{
			Name: "list_files",
			Description: "List every indexed file with its last-modified time. " +
				"Use to discover what is searchable before searching, or to check " +
				"whether a specific file made it into the index.",
		}, func(ctx context.Context, req *mcp.CallToolRequest, args struct{}) (*mcp.CallToolResult, any, error) {
			paths, err := st.Paths(ctx)
			if err != nil {
				return nil, nil, err
			}

			files := make([]indexedFile, 0, len(paths))
			for path, mtime := range paths {
				files = append(files, indexedFile{
					Path:     path,
					Modified: time.Unix(mtime, 0).UTC().Format(time.RFC3339),
				})
			}
			sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })

			payload, err := json.Marshal(files)
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
