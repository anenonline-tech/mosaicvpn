package mcp

import (
	"context"
	"fmt"
	"runtime"
	"time"

	"github.com/pupspochta-cpu/mosaicvpn/internal/proto"
	"github.com/pupspochta-cpu/mosaicvpn/internal/store"
	"github.com/pupspochta-cpu/mosaicvpn/internal/subs"
)

// buildTools returns the catalog of tools exposed over MCP. The list is
// intentionally minimal — every tool maps 1:1 onto an existing daemon
// API endpoint so the MCP surface stays in lockstep with the human one.
func (s *Server) buildTools() []toolDef {
	return []toolDef{
		{
			Name:        "get_status",
			Description: "Return the current connection state, bytes counters, and active server.",
			InputSchema: schemaObject(map[string]any{}, nil),
			requires:    permRead,
			handler:     s.toolGetStatus,
		},
		{
			Name:        "list_servers",
			Description: "List every known server across all subscriptions.",
			InputSchema: schemaObject(map[string]any{}, nil),
			requires:    permRead,
			handler:     s.toolListServers,
		},
		{
			Name:        "list_subscriptions",
			Description: "List configured subscriptions and their parse status.",
			InputSchema: schemaObject(map[string]any{}, nil),
			requires:    permRead,
			handler:     s.toolListSubs,
		},
		{
			Name:        "list_rules",
			Description: "List configured routing rules in priority order.",
			InputSchema: schemaObject(map[string]any{}, nil),
			requires:    permRead,
			handler:     s.toolListRules,
		},
		{
			Name:        "get_prefs",
			Description: "Return the daemon preferences (DNS, tunnel mode, kill-switch, MCP, ...).",
			InputSchema: schemaObject(map[string]any{}, nil),
			requires:    permRead,
			handler:     s.toolGetPrefs,
		},
		{
			Name:        "run_diag",
			Description: "Generate a diagnostic snapshot suitable for bug reports.",
			InputSchema: schemaObject(map[string]any{}, nil),
			requires:    permRead,
			handler:     s.toolDiag,
		},

		{
			Name: "connect_server",
			Description: "Bring up the tunnel against the given server id. Destructive — " +
				"changes the host's network state.",
			InputSchema: schemaObject(
				map[string]any{
					"server_id": schemaString("server.id from list_servers"),
					"confirm":   schemaBool("must be true when prefs.MCPConfirm is on"),
				},
				[]string{"server_id"},
			),
			requires: permConnect,
			confirm:  true,
			handler:  s.toolConnect,
		},
		{
			Name:        "disconnect",
			Description: "Tear down the tunnel.",
			InputSchema: schemaObject(map[string]any{
				"confirm": schemaBool("must be true when prefs.MCPConfirm is on"),
			}, nil),
			requires: permConnect,
			confirm:  true,
			handler:  s.toolDisconnect,
		},
		{
			Name:        "refresh_subscription",
			Description: "Re-fetch and re-parse a subscription, replacing its server list.",
			InputSchema: schemaObject(
				map[string]any{"subscription_id": schemaString("subscription.id")},
				[]string{"subscription_id"},
			),
			requires: permConnect,
			handler:  s.toolRefreshSub,
		},

		{
			Name: "add_subscription",
			Description: "Add a new subscription URL and immediately fetch / parse it. " +
				"Destructive: introduces new servers and triggers an outbound HTTP request.",
			InputSchema: schemaObject(
				map[string]any{
					"url":     schemaString("subscription URL"),
					"name":    schemaString("display name (optional)"),
					"format":  schemaString("override format: singbox|clash|v2ray-base64|sip008 (optional)"),
					"confirm": schemaBool("must be true when prefs.MCPConfirm is on"),
				},
				[]string{"url"},
			),
			requires: permFull,
			confirm:  true,
			handler:  s.toolAddSub,
		},
		{
			Name:        "delete_subscription",
			Description: "Delete a subscription and all servers it owns.",
			InputSchema: schemaObject(
				map[string]any{
					"subscription_id": schemaString("subscription.id"),
					"confirm":         schemaBool("must be true when prefs.MCPConfirm is on"),
				},
				[]string{"subscription_id"},
			),
			requires: permFull,
			confirm:  true,
			handler:  s.toolDeleteSub,
		},
		{
			Name:        "add_rule",
			Description: "Append a new routing rule.",
			InputSchema: schemaObject(
				map[string]any{
					"rule": schemaRawObject("a proto.Rule object"),
				},
				[]string{"rule"},
			),
			requires: permFull,
			confirm:  true,
			handler:  s.toolAddRule,
		},
		{
			Name:        "delete_rule",
			Description: "Delete a routing rule by id.",
			InputSchema: schemaObject(
				map[string]any{
					"rule_id": schemaString("rule.id"),
					"confirm": schemaBool("must be true when prefs.MCPConfirm is on"),
				},
				[]string{"rule_id"},
			),
			requires: permFull,
			confirm:  true,
			handler:  s.toolDeleteRule,
		},
		{
			Name: "set_prefs",
			Description: "Replace the daemon preferences. Pass the full prefs object " +
				"as returned from get_prefs with your edits applied.",
			InputSchema: schemaObject(
				map[string]any{
					"prefs":   schemaRawObject("a store.Prefs object"),
					"confirm": schemaBool("must be true when prefs.MCPConfirm is on"),
				},
				[]string{"prefs"},
			),
			requires: permFull,
			confirm:  true,
			handler:  s.toolSetPrefs,
		},
	}
}

// ---------- read tools ----------------------------------------------------

func (s *Server) toolGetStatus(_ toolCtx, _ map[string]any) any {
	return s.mgr.Status()
}

func (s *Server) toolListServers(_ toolCtx, _ map[string]any) any {
	return s.store.Snapshot().Servers
}

func (s *Server) toolListSubs(_ toolCtx, _ map[string]any) any {
	return s.store.Snapshot().Subscriptions
}

func (s *Server) toolListRules(_ toolCtx, _ map[string]any) any {
	return s.store.Snapshot().Rules
}

func (s *Server) toolGetPrefs(_ toolCtx, _ map[string]any) any {
	return s.store.Snapshot().Prefs
}

func (s *Server) toolDiag(_ toolCtx, _ map[string]any) any {
	snap := s.store.Snapshot()
	return proto.DiagReport{
		GeneratedAt:   time.Now().UTC(),
		DaemonVersion: s.mgr.Version(),
		OS:            runtime.GOOS,
		Status:        s.mgr.Status(),
		Subscriptions: snap.Subscriptions,
		ServerCount:   len(snap.Servers),
		RuleCount:     len(snap.Rules),
	}
}

// ---------- connect tools -------------------------------------------------

func (s *Server) toolConnect(_ toolCtx, args map[string]any) any {
	id := strArg(args, "server_id")
	if id == "" {
		return errResult("server_id required")
	}
	if err := s.mgr.Connect(context.Background(), id); err != nil {
		return errResult("connect: %v", err)
	}
	return s.mgr.Status()
}

func (s *Server) toolDisconnect(_ toolCtx, _ map[string]any) any {
	if err := s.mgr.Disconnect(context.Background()); err != nil {
		return errResult("disconnect: %v", err)
	}
	return s.mgr.Status()
}

func (s *Server) toolRefreshSub(_ toolCtx, args map[string]any) any {
	id := strArg(args, "subscription_id")
	if id == "" {
		return errResult("subscription_id required")
	}
	snap := s.store.Snapshot()
	var sub proto.Subscription
	for _, x := range snap.Subscriptions {
		if x.ID == id {
			sub = x
			break
		}
	}
	if sub.ID == "" {
		return errResult("subscription %q not found", id)
	}
	if err := s.refreshSubscription(context.Background(), sub); err != nil {
		return errResult("refresh: %v", err)
	}
	// Return the updated subscription record so the agent can confirm.
	for _, x := range s.store.Snapshot().Subscriptions {
		if x.ID == id {
			return x
		}
	}
	return textResult("subscription %q refreshed", id)
}

// ---------- full tools ----------------------------------------------------

func (s *Server) toolAddSub(_ toolCtx, args map[string]any) any {
	url := strArg(args, "url")
	if url == "" {
		return errResult("url required")
	}
	name := strArg(args, "name")
	if name == "" {
		name = url
	}
	format := proto.Format(strArg(args, "format"))
	sub, err := s.store.AddOrUpdateSubscription(proto.Subscription{
		Name:        name,
		URL:         url,
		Format:      format,
		AutoRefresh: true,
	})
	if err != nil {
		return errResult("add: %v", err)
	}
	if err := s.refreshSubscription(context.Background(), sub); err != nil {
		// Persist the error on the subscription so the agent can see it.
		sub.LastError = err.Error()
		_, _ = s.store.AddOrUpdateSubscription(sub)
		return errResult("fetch: %v", err)
	}
	for _, x := range s.store.Snapshot().Subscriptions {
		if x.ID == sub.ID {
			return x
		}
	}
	return sub
}

func (s *Server) toolDeleteSub(_ toolCtx, args map[string]any) any {
	id := strArg(args, "subscription_id")
	if id == "" {
		return errResult("subscription_id required")
	}
	if err := s.store.DeleteSubscription(id); err != nil {
		return errResult("delete: %v", err)
	}
	return textResult("subscription %q deleted", id)
}

func (s *Server) toolAddRule(_ toolCtx, args map[string]any) any {
	rawRule, ok := args["rule"]
	if !ok {
		return errResult("rule required")
	}
	rule, err := asProtoRule(rawRule)
	if err != nil {
		return errResult("rule decode: %v", err)
	}
	saved, err := s.store.AddRule(rule)
	if err != nil {
		return errResult("add: %v", err)
	}
	return saved
}

func (s *Server) toolDeleteRule(_ toolCtx, args map[string]any) any {
	id := strArg(args, "rule_id")
	if id == "" {
		return errResult("rule_id required")
	}
	if err := s.store.DeleteRule(id); err != nil {
		return errResult("delete: %v", err)
	}
	return textResult("rule %q deleted", id)
}

func (s *Server) toolSetPrefs(_ toolCtx, args map[string]any) any {
	rawPrefs, ok := args["prefs"]
	if !ok {
		return errResult("prefs required")
	}
	prefs, err := asStorePrefs(rawPrefs)
	if err != nil {
		return errResult("prefs decode: %v", err)
	}
	if err := s.store.SetPrefs(prefs); err != nil {
		return errResult("set: %v", err)
	}
	s.mgr.SetTunnelPrefs(prefs.TunnelMode, prefs.KillSwitch)
	return s.store.Snapshot().Prefs
}

// refreshSubscription mirrors api.Server.refresh: fetch, parse, replace
// the subscription's server list. Lives here to keep MCP independent of
// the api package.
func (s *Server) refreshSubscription(ctx context.Context, sub proto.Subscription) error {
	if s.fetcher == nil {
		return fmt.Errorf("no fetcher configured")
	}
	body, _, err := s.fetcher(ctx, sub.URL)
	if err != nil {
		return fmt.Errorf("fetch: %w", err)
	}
	var res subs.Result
	if sub.Format != "" && sub.Format != proto.FormatUnknown {
		res, err = subs.ParseAs(sub.ID, body, sub.Format)
	} else {
		res, err = subs.Parse(sub.ID, body)
	}
	if err != nil {
		return fmt.Errorf("parse: %w", err)
	}
	sub.Format = res.Format
	sub.ServerCount = len(res.Servers)
	sub.LastFetched = time.Now().UTC()
	sub.LastError = ""
	if _, err := s.store.AddOrUpdateSubscription(sub); err != nil {
		return err
	}
	return s.store.ReplaceServersFor(sub.ID, res.Servers)
}

// ---------- argument decoders --------------------------------------------

func asProtoRule(v any) (proto.Rule, error) {
	raw, err := jsonReencode(v)
	if err != nil {
		return proto.Rule{}, err
	}
	var r proto.Rule
	return r, jsonDecode(raw, &r)
}

func asStorePrefs(v any) (store.Prefs, error) {
	raw, err := jsonReencode(v)
	if err != nil {
		return store.Prefs{}, err
	}
	var p store.Prefs
	return p, jsonDecode(raw, &p)
}
