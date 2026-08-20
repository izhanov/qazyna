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
	"log/slog"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const version = "0.1.0"

type Option func(*mcp.Server) error

// Run serves MCP on stdin/stdout until the client disconnects.
// All logging goes to stderr: stdout belongs to the JSON-RPC protocol.
func Run(ctx context.Context, opts ...Option) error {
	server := mcp.NewServer(&mcp.Implementation{Name: "qazyna", Version: version}, nil)
	server.AddReceivingMiddleware(logging)
	for _, o := range opts {
		if err := o(server); err != nil {
			return err
		}
	}

	slog.Info("mcp server started", "version", version)
	err := server.Run(ctx, &mcp.StdioTransport{})
	slog.Info("mcp server stopped", "error", err)
	return err
}

// logging records every incoming MCP call with its duration and outcome —
// one place instead of every tool. Request payloads are deliberately not
// logged: notes content may be sensitive and these logs land on disk.
func logging(next mcp.MethodHandler) mcp.MethodHandler {
	return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
		start := time.Now()
		result, err := next(ctx, method, req)

		attrs := []any{"method", method, "took", time.Since(start).Round(time.Millisecond)}
		ctr, isToolCall := req.(*mcp.CallToolRequest)
		if isToolCall {
			attrs = append(attrs, "tool", ctr.Params.Name)
		}
		switch {
		case err != nil:
			slog.Error("mcp call failed", append(attrs, "error", err)...)
		case isToolCall:
			// Tool calls are the events that matter — always visible.
			slog.Info("tool called", attrs...)
		default:
			// Protocol chatter (initialize, ping, notifications).
			slog.Debug("mcp call", attrs...)
		}
		return result, err
	}
}
