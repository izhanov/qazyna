// Package mcpserver exposes qazyna's search to AI agents over the Model
// Context Protocol (stdio transport). Read-only by design: indexing and
// flushing stay with the human in the terminal.
//
// The server core knows nothing about concrete tools: each tool lives in
// its own file under tools/ as an Option constructor and is registered by
// the caller.
package mcpserver

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const version = "0.1.0"

type Option func(*mcp.Server) error

// Run serves MCP on stdin/stdout until the client disconnects.
func Run(ctx context.Context, opts ...Option) error {
	server := mcp.NewServer(&mcp.Implementation{Name: "qazyna", Version: version}, nil)
	for _, o := range opts {
		if err := o(server); err != nil {
			return err
		}
	}
	return server.Run(ctx, &mcp.StdioTransport{})
}
