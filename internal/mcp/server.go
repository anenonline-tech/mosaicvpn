package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/pupspochta-cpu/mosaicvpn/internal/logx"
	"github.com/pupspochta-cpu/mosaicvpn/internal/state"
	"github.com/pupspochta-cpu/mosaicvpn/internal/store"
)

// ProtocolVersion advertises the MCP protocol revision the daemon
// supports. 2024-11-05 is the published draft as of sing-box 1.13 and
// matches what Claude / Cursor / Continue etc. negotiate today.
const ProtocolVersion = "2024-11-05"

// Fetcher mirrors api.Fetcher; we keep it local to avoid an import cycle
// when the API server starts the MCP server in the same process.
type Fetcher func(ctx context.Context, url string) ([]byte, string, error)

// Server is the embedded MCP server. It binds prefs.MCPAddr and dispatches
// JSON-RPC requests to the registered tools.
type Server struct {
	store   *store.Store
	mgr     *state.Manager
	fetcher Fetcher

	mu    sync.Mutex
	srv   *http.Server
	tools []toolDef
}

// New constructs a Server. The store and manager are the same instances
// the API server uses — there is exactly one source of truth.
func New(s *store.Store, mgr *state.Manager, fetcher Fetcher) *Server {
	srv := &Server{store: s, mgr: mgr, fetcher: fetcher}
	srv.tools = srv.buildTools()
	return srv
}

// Start binds the configured address. If prefs.MCPEnabled is false the
// call is a no-op and Start returns nil. If the bind fails we log and
// return the error so the caller can choose whether to fail startup.
func (s *Server) Start(ctx context.Context) error {
	prefs := s.store.Snapshot().Prefs
	if !prefs.MCPEnabled {
		logx.Info("mcp disabled by prefs")
		return nil
	}
	addr := prefs.MCPAddr
	if addr == "" {
		addr = "127.0.0.1:8731"
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("mcp listen %s: %w", addr, err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handle)
	srv := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	s.mu.Lock()
	s.srv = srv
	s.mu.Unlock()
	go func() {
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logx.Error("mcp server crashed", "err", err)
		}
	}()
	logx.Info("mcp listening", "addr", ln.Addr().String(), "permission", prefs.MCPPermission)
	return nil
}

// Stop tears down the listener.
func (s *Server) Stop(ctx context.Context) error {
	s.mu.Lock()
	srv := s.srv
	s.mu.Unlock()
	if srv == nil {
		return nil
	}
	return srv.Shutdown(ctx)
}

// Handler exposes the dispatcher for tests that don't want a real socket.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handle)
	return mux
}

func (s *Server) handle(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		writeRPCError(w, nil, errInternal, "read body: "+err.Error())
		return
	}
	body = bytes.TrimSpace(body)
	if len(body) == 0 {
		writeRPCError(w, nil, errInvalidRequest, "empty body")
		return
	}

	// MCP allows batched requests as a JSON array. Handle both shapes.
	if body[0] == '[' {
		var batch []jsonRPCRequest
		if err := json.Unmarshal(body, &batch); err != nil {
			writeRPCError(w, nil, errParse, err.Error())
			return
		}
		out := make([]jsonRPCResponse, 0, len(batch))
		for i := range batch {
			if resp, ok := s.dispatch(r.Context(), batch[i]); ok {
				out = append(out, resp)
			}
		}
		writeJSON(w, http.StatusOK, out)
		return
	}

	var req jsonRPCRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeRPCError(w, nil, errParse, err.Error())
		return
	}
	resp, ok := s.dispatch(r.Context(), req)
	if !ok {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

// dispatch returns (response, true) for requests, (zero, false) for
// notifications (no id) where MCP forbids sending a response.
func (s *Server) dispatch(ctx context.Context, req jsonRPCRequest) (jsonRPCResponse, bool) {
	notification := len(req.ID) == 0
	resp := jsonRPCResponse{JSONRPC: "2.0", ID: req.ID}

	switch req.Method {
	case "initialize":
		resp.Result = map[string]any{
			"protocolVersion": ProtocolVersion,
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo": map[string]any{
				"name":    "mosaicvpn",
				"version": s.mgr.Version(),
			},
		}
	case "notifications/initialized":
		// Per MCP spec the client sends this as a notification; no response.
		return resp, false
	case "tools/list":
		// Surface only the tool's externally-visible fields.
		out := make([]map[string]any, 0, len(s.tools))
		for _, t := range s.tools {
			out = append(out, map[string]any{
				"name":        t.Name,
				"description": t.Description,
				"inputSchema": t.InputSchema,
			})
		}
		resp.Result = map[string]any{"tools": out}
	case "tools/call":
		var p struct {
			Name      string         `json:"name"`
			Arguments map[string]any `json:"arguments"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			resp.Error = &jsonRPCError{Code: errInvalidParams, Message: err.Error()}
			break
		}
		resp.Result = s.callTool(ctx, p.Name, p.Arguments)
	default:
		if notification {
			return resp, false
		}
		resp.Error = &jsonRPCError{Code: errMethodNotFound, Message: req.Method}
	}

	if notification {
		return resp, false
	}
	return resp, true
}

func (s *Server) callTool(_ context.Context, name string, args map[string]any) toolResult {
	if args == nil {
		args = map[string]any{}
	}
	prefs := s.store.Snapshot().Prefs
	for _, t := range s.tools {
		if t.Name != name {
			continue
		}
		if !permitted(prefs.MCPPermission, t.requires) {
			return errResult(
				"tool %q requires %q permission; daemon is configured for %q",
				name, t.requires, prefs.MCPPermission,
			)
		}
		if t.confirm && prefs.MCPConfirm {
			confirm, _ := args["confirm"].(bool)
			if !confirm {
				return errResult(
					"tool %q is destructive and prefs.MCPConfirm is on; pass arguments.confirm=true",
					name,
				)
			}
		}
		return asToolResult(t.handler(toolCtx{server: s}, args))
	}
	return errResult("unknown tool: %q", name)
}

// asToolResult lets handlers return either toolResult directly or any
// other type that should be JSON-pretty-printed for the agent to read.
func asToolResult(v any) toolResult {
	if tr, ok := v.(toolResult); ok {
		return tr
	}
	return jsonResult(v)
}

// permitted returns whether a tool requiring `need` is allowed under the
// daemon's `have` setting.
func permitted(have, need string) bool {
	rank := func(p string) int {
		switch p {
		case permRead:
			return 1
		case permConnect:
			return 2
		case permFull:
			return 3
		default:
			return 0
		}
	}
	return rank(have) >= rank(need)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}

func writeRPCError(w http.ResponseWriter, id json.RawMessage, code int, msg string) {
	writeJSON(w, http.StatusOK, jsonRPCResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error:   &jsonRPCError{Code: code, Message: msg},
	})
}

// strArg pulls a string argument by name, falling back to "" if absent.
func strArg(args map[string]any, key string) string {
	v, _ := args[key].(string)
	return v
}
