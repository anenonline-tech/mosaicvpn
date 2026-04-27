// Package mcp embeds a Model Context Protocol server inside the Mosaic
// daemon so an AI agent can drive Mosaic the same way a human can drive
// the CLI: connect / disconnect, manage subscriptions and rules, set
// preferences, run diagnostics. The transport is the smallest subset of
// MCP that real clients use today — JSON-RPC 2.0 over a single HTTP POST
// endpoint, methods initialize / tools/list / tools/call.
//
// The server is gated by prefs.MCPEnabled and prefs.MCPPermission so a
// user can install an AI client without giving it carte blanche.
//   - "read"    : read-only tools (status, lists, diag)
//   - "connect" : "read" + connect / disconnect / refresh subscriptions
//   - "full"    : "connect" + mutate prefs / rules / subscriptions
//
// Destructive tools require the caller to set arguments.confirm = true
// when prefs.MCPConfirm is on, mirroring the user-confirmation hook the
// Tauri UI surfaces for the same actions.
package mcp

import (
	"encoding/json"
	"fmt"
)

// permission levels in increasing order of trust.
const (
	permRead    = "read"
	permConnect = "connect"
	permFull    = "full"
)

// jsonRPCRequest is the wire format the daemon accepts on POST /.
type jsonRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type jsonRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *jsonRPCError   `json:"error,omitempty"`
}

type jsonRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// MCP error codes — superset of JSON-RPC, plus an MCP-tool-error sentinel.
const (
	errParse          = -32700
	errInvalidRequest = -32600
	errMethodNotFound = -32601
	errInvalidParams  = -32602
	errInternal       = -32603
)

// toolDef describes a tool exposed over MCP.
type toolDef struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`

	// internal:
	requires string                                     // permRead / permConnect / permFull
	confirm  bool                                       // requires confirm=true when prefs.MCPConfirm
	handler  func(ctx toolCtx, args map[string]any) any // returns the structured result
}

// toolCtx is what each tool handler receives.
type toolCtx struct {
	server *Server
}

// content is one entry in a tools/call result.content slice. MCP requires
// a list of typed content blocks even for trivially short results.
type content struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// toolResult is the MCP response shape for tools/call.
type toolResult struct {
	Content []content `json:"content"`
	IsError bool      `json:"isError,omitempty"`
}

func textResult(format string, args ...any) toolResult {
	return toolResult{Content: []content{{Type: "text", Text: fmt.Sprintf(format, args...)}}}
}

func errResult(format string, args ...any) toolResult {
	return toolResult{
		Content: []content{{Type: "text", Text: fmt.Sprintf(format, args...)}},
		IsError: true,
	}
}

func jsonResult(v any) toolResult {
	raw, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return errResult("internal: marshal: %v", err)
	}
	return toolResult{Content: []content{{Type: "text", Text: string(raw)}}}
}
