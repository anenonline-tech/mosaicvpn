package mcp_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pupspochta-cpu/mosaicvpn/internal/mcp"
	"github.com/pupspochta-cpu/mosaicvpn/internal/proto"
	"github.com/pupspochta-cpu/mosaicvpn/internal/state"
	"github.com/pupspochta-cpu/mosaicvpn/internal/store"
)

// newServer wires a freshly-temp-dir-backed Store, a MockBackend and an
// MCP Server, returning everything the tests need to drive RPCs.
func newServer(t *testing.T) (*store.Store, *state.Manager, *mcp.Server, proto.Server) {
	t.Helper()
	s, err := store.Open(filepath.Join(t.TempDir(), "store.json"))
	if err != nil {
		t.Fatalf("store open: %v", err)
	}
	srv := proto.Server{
		ID: "srv1", Name: "Test", Protocol: proto.ProtoVLESS,
		Address: "1.2.3.4", Port: 443,
	}
	if err := s.Update(func(st *store.State) error {
		st.Servers = []proto.Server{srv}
		return nil
	}); err != nil {
		t.Fatalf("seed servers: %v", err)
	}
	mb := state.NewMockBackend()
	mgr := state.New(s, mb, "test")
	m := mcp.New(s, mgr, nil)
	return s, mgr, m, srv
}

// rpc sends a single request and decodes the response.
func rpc(t *testing.T, h http.Handler, method string, params any) map[string]any {
	t.Helper()
	body := mustJSON(t, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  method,
		"params":  params,
	})
	r := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("rpc %q status: got %d, body=%s", method, w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v\n%s", err, w.Body.String())
	}
	return resp
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return b
}

func TestInitialize(t *testing.T) {
	_, _, m, _ := newServer(t)
	resp := rpc(t, m.Handler(), "initialize", map[string]any{})
	res, _ := resp["result"].(map[string]any)
	if res == nil {
		t.Fatalf("no result: %v", resp)
	}
	if res["protocolVersion"] != mcp.ProtocolVersion {
		t.Fatalf("protocolVersion: %v", res)
	}
	caps, _ := res["capabilities"].(map[string]any)
	if _, ok := caps["tools"]; !ok {
		t.Fatalf("capabilities.tools missing: %v", res)
	}
}

func TestToolsListIncludesEveryTool(t *testing.T) {
	_, _, m, _ := newServer(t)
	resp := rpc(t, m.Handler(), "tools/list", map[string]any{})
	res, _ := resp["result"].(map[string]any)
	tools, _ := res["tools"].([]any)
	if len(tools) == 0 {
		t.Fatalf("no tools: %v", resp)
	}
	want := []string{
		"get_status", "list_servers", "list_subscriptions",
		"list_rules", "get_prefs", "run_diag",
		"connect_server", "disconnect", "refresh_subscription",
		"add_subscription", "delete_subscription",
		"add_rule", "delete_rule", "set_prefs",
	}
	got := map[string]bool{}
	for _, t := range tools {
		m, _ := t.(map[string]any)
		got[m["name"].(string)] = true
	}
	for _, n := range want {
		if !got[n] {
			t.Errorf("tool %q missing", n)
		}
	}
}

// TestPermissionGate checks that a "read"-permission daemon rejects
// connect_server with a non-empty error in the tool result.
func TestPermissionGate(t *testing.T) {
	s, _, m, srv := newServer(t)
	if err := s.Update(func(st *store.State) error {
		st.Prefs.MCPPermission = "read"
		return nil
	}); err != nil {
		t.Fatalf("update prefs: %v", err)
	}
	resp := rpc(t, m.Handler(), "tools/call", map[string]any{
		"name":      "connect_server",
		"arguments": map[string]any{"server_id": srv.ID, "confirm": true},
	})
	res, _ := resp["result"].(map[string]any)
	if res == nil {
		t.Fatalf("no result: %v", resp)
	}
	if res["isError"] != true {
		t.Fatalf("expected isError=true under read permission, got %v", res)
	}
	text := contentText(res)
	if !strings.Contains(text, "permission") {
		t.Fatalf("expected permission error, got %q", text)
	}
}

// TestConfirmGate: a destructive tool must require arguments.confirm=true
// when prefs.MCPConfirm is on.
func TestConfirmGate(t *testing.T) {
	s, _, m, srv := newServer(t)
	if err := s.Update(func(st *store.State) error {
		st.Prefs.MCPPermission = "full"
		st.Prefs.MCPConfirm = true
		return nil
	}); err != nil {
		t.Fatalf("update prefs: %v", err)
	}
	// Without confirm — should error.
	resp := rpc(t, m.Handler(), "tools/call", map[string]any{
		"name":      "connect_server",
		"arguments": map[string]any{"server_id": srv.ID},
	})
	res, _ := resp["result"].(map[string]any)
	if res["isError"] != true {
		t.Fatalf("expected confirm gate error, got %v", res)
	}
}

func TestConnectAndDisconnectFlow(t *testing.T) {
	s, mgr, m, srv := newServer(t)
	if err := s.Update(func(st *store.State) error {
		st.Prefs.MCPPermission = "connect"
		st.Prefs.MCPConfirm = false
		return nil
	}); err != nil {
		t.Fatalf("update prefs: %v", err)
	}

	resp := rpc(t, m.Handler(), "tools/call", map[string]any{
		"name":      "connect_server",
		"arguments": map[string]any{"server_id": srv.ID},
	})
	res, _ := resp["result"].(map[string]any)
	if res["isError"] == true {
		t.Fatalf("connect tool returned error: %v", res)
	}
	if got := mgr.Status().State; got != proto.StateConnected {
		t.Fatalf("manager state: got %v, want connected", got)
	}

	resp = rpc(t, m.Handler(), "tools/call", map[string]any{
		"name":      "disconnect",
		"arguments": map[string]any{},
	})
	res, _ = resp["result"].(map[string]any)
	if res["isError"] == true {
		t.Fatalf("disconnect tool returned error: %v", res)
	}
	if got := mgr.Status().State; got != proto.StateDisconnected {
		t.Fatalf("manager state: got %v, want disconnected", got)
	}
}

func TestAddRuleAndList(t *testing.T) {
	s, _, m, _ := newServer(t)
	if err := s.Update(func(st *store.State) error {
		st.Prefs.MCPPermission = "full"
		st.Prefs.MCPConfirm = false
		return nil
	}); err != nil {
		t.Fatalf("update prefs: %v", err)
	}
	resp := rpc(t, m.Handler(), "tools/call", map[string]any{
		"name": "add_rule",
		"arguments": map[string]any{
			"rule": map[string]any{
				"name":    "ad-block",
				"enabled": true,
				"action":  "block",
				"match":   map[string]any{"domain_suffix": []string{"doubleclick.net"}},
			},
		},
	})
	res, _ := resp["result"].(map[string]any)
	if res["isError"] == true {
		t.Fatalf("add_rule failed: %v", res)
	}
	listResp := rpc(t, m.Handler(), "tools/call", map[string]any{
		"name":      "list_rules",
		"arguments": map[string]any{},
	})
	if !strings.Contains(contentText(listResp["result"].(map[string]any)), "ad-block") {
		t.Fatalf("expected new rule in list_rules: %s", contentText(listResp["result"].(map[string]any)))
	}
}

// TestNotificationsInitialized verifies the daemon does not return a body
// for a notification (per JSON-RPC 2.0).
func TestNotificationsInitialized(t *testing.T) {
	_, _, m, _ := newServer(t)
	body := []byte(`{"jsonrpc":"2.0","method":"notifications/initialized"}`)
	r := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
	w := httptest.NewRecorder()
	m.Handler().ServeHTTP(w, r)
	if w.Code != http.StatusNoContent {
		t.Fatalf("notification status: got %d, want 204; body=%s", w.Code, w.Body.String())
	}
}

// TestStartIsNoOpWhenDisabled: prefs.MCPEnabled=false must skip listen.
func TestStartIsNoOpWhenDisabled(t *testing.T) {
	s, _, m, _ := newServer(t)
	if err := s.Update(func(st *store.State) error {
		st.Prefs.MCPEnabled = false
		return nil
	}); err != nil {
		t.Fatalf("update: %v", err)
	}
	if err := m.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	// Stop must be safe even when no listener was started.
	if err := m.Stop(context.Background()); err != nil {
		t.Fatalf("Stop: %v", err)
	}
}

// contentText concatenates the text content blocks from a tool result.
func contentText(res map[string]any) string {
	cs, _ := res["content"].([]any)
	var b strings.Builder
	for _, c := range cs {
		m, _ := c.(map[string]any)
		if t, _ := m["text"].(string); t != "" {
			b.WriteString(t)
		}
	}
	return b.String()
}

// silence unused import from test scaffolding when reading the response body.
var _ = io.Discard
