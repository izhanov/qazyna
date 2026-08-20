package tools

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"qazyna/internal/mcpserver"
	"qazyna/internal/search"
	"qazyna/internal/store"
)

// IndexStatus registers the index_status tool: a cheap health check of the
// search index.
func IndexStatus(st store.Store) mcpserver.Option {
	return func(server *mcp.Server) error {
		mcp.AddTool(server, &mcp.Tool{
			Name: "index_status",
			Description: "Report the state of the search index: how many chunks are stored and " +
				"which embedding model built it. Useful to check the index is alive before searching.",
		}, func(ctx context.Context, req *mcp.CallToolRequest, args struct{}) (*mcp.CallToolResult, any, error) {
			n, err := st.Count(ctx)
			if err != nil {
				return nil, nil, err
			}
			meta, err := st.Meta(ctx)
			if err != nil {
				return nil, nil, err
			}
			status := fmt.Sprintf(`{"chunks": %d, "embedder": %q}`, n, meta[search.MetaEmbedderKey])
			return &mcp.CallToolResult{
				Content: []mcp.Content{&mcp.TextContent{Text: status}},
			}, nil, nil
		})
		return nil
	}
}
