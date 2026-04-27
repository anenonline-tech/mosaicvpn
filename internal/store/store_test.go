package store_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/pupspochta-cpu/mosaicvpn/internal/proto"
	"github.com/pupspochta-cpu/mosaicvpn/internal/store"
)

func newStore(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.Open(filepath.Join(t.TempDir(), "store.json"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	return s
}

func TestDefaultPrefs(t *testing.T) {
	s := newStore(t)
	snap := s.Snapshot()
	if snap.Prefs.TunnelMode != "tun" {
		t.Fatalf("expected default tunnel mode tun, got %q", snap.Prefs.TunnelMode)
	}
	if !snap.Prefs.KillSwitch {
		t.Fatal("expected kill-switch on by default")
	}
}

func TestAddSubscriptionAndServers(t *testing.T) {
	s := newStore(t)
	sub, err := s.AddOrUpdateSubscription(proto.Subscription{
		URL:    "https://example.com/sub",
		Name:   "Example",
		Format: proto.FormatSingbox,
	})
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	if sub.ID == "" {
		t.Fatal("expected ID assigned")
	}

	servers := []proto.Server{
		{ID: "s1", Name: "tokyo", Protocol: proto.ProtoVLESS, Address: "1.2.3.4", Port: 443},
		{ID: "s2", Name: "amst", Protocol: proto.ProtoHysteria2, Address: "5.6.7.8", Port: 8443},
	}
	if err := s.ReplaceServersFor(sub.ID, servers); err != nil {
		t.Fatalf("replace servers: %v", err)
	}

	snap := s.Snapshot()
	if len(snap.Servers) != 2 {
		t.Fatalf("expected 2 servers, got %d", len(snap.Servers))
	}
	for _, sv := range snap.Servers {
		if sv.SubscriptionID != sub.ID {
			t.Fatalf("server %q has wrong sub id %q", sv.ID, sv.SubscriptionID)
		}
	}
	for _, su := range snap.Subscriptions {
		if su.ID == sub.ID && su.ServerCount != 2 {
			t.Fatalf("expected ServerCount=2, got %d", su.ServerCount)
		}
	}
}

func TestUpdateSubscriptionByURLDeduplicates(t *testing.T) {
	s := newStore(t)
	a, _ := s.AddOrUpdateSubscription(proto.Subscription{URL: "u", Name: "A"})
	b, _ := s.AddOrUpdateSubscription(proto.Subscription{URL: "u", Name: "B"})
	if a.ID != b.ID {
		t.Fatalf("expected stable ID across same-URL adds, got %q vs %q", a.ID, b.ID)
	}
	snap := s.Snapshot()
	if len(snap.Subscriptions) != 1 {
		t.Fatalf("expected 1 subscription, got %d", len(snap.Subscriptions))
	}
	if snap.Subscriptions[0].Name != "B" {
		t.Fatalf("expected updated name B, got %q", snap.Subscriptions[0].Name)
	}
}

func TestPersistRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "store.json")

	s, err := store.Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	sub, _ := s.AddOrUpdateSubscription(proto.Subscription{URL: "u", Name: "n"})
	_ = s.ReplaceServersFor(sub.ID, []proto.Server{
		{ID: "x", Name: "tokyo", Protocol: proto.ProtoVLESS, Address: "1.1.1.1", Port: 443},
	})
	_ = s.SetLastServer("x")

	s2, err := store.Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	snap := s2.Snapshot()
	if snap.LastServerID != "x" {
		t.Fatalf("expected LastServerID=x, got %q", snap.LastServerID)
	}
	if len(snap.Servers) != 1 {
		t.Fatalf("expected 1 server, got %d", len(snap.Servers))
	}
}

// TestOpen_BackfillsTunStack guards against the regression Devin Review
// caught: an on-disk config written before the TunStack field existed
// must not deserialise into TunStack="" because that would silently
// change config-builder behaviour at runtime.
func TestOpen_BackfillsTunStack(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "store.json")

	// A legacy on-disk shape: every field except TunStack populated, the
	// way Phase 1 wrote it.
	legacy := []byte(`{
		"prefs": {
			"tunnel_mode": "tun",
			"socks_addr": "127.0.0.1:1080",
			"http_addr":  "127.0.0.1:1081",
			"mtu": 1420,
			"kill_switch": true,
			"allow_lan": true,
			"dns_mode": "fake-ip",
			"dns_proxied": "https://1.1.1.1/dns-query",
			"dns_direct":  "udp://77.88.8.8",
			"share_addr":  "0.0.0.0:1080",
			"auto_start":  "service",
			"auto_connect": true,
			"show_on_launch": true,
			"mcp_enabled": true,
			"mcp_addr":    "127.0.0.1:8731",
			"mcp_permission": "connect",
			"mcp_confirm": true
		},
		"version": 1
	}`)
	if err := os.WriteFile(path, legacy, 0o600); err != nil {
		t.Fatalf("seed legacy store: %v", err)
	}

	s, err := store.Open(path)
	if err != nil {
		t.Fatalf("open legacy store: %v", err)
	}
	got := s.Snapshot().Prefs
	if got.TunStack == "" {
		t.Fatalf("TunStack was left empty after Open; expected backfill to %q",
			store.DefaultPrefs().TunStack)
	}
	if got.TunStack != store.DefaultPrefs().TunStack {
		t.Fatalf("TunStack = %q, want %q", got.TunStack, store.DefaultPrefs().TunStack)
	}
	// Sanity: the rest of the legacy prefs were preserved (i.e. Open
	// didn't reset *everything* to defaults via the SocksAddr=="" path).
	if got.SocksAddr != "127.0.0.1:1080" {
		t.Fatalf("legacy SocksAddr clobbered: %q", got.SocksAddr)
	}
}

func TestRules(t *testing.T) {
	s := newStore(t)
	r1, _ := s.AddRule(proto.Rule{Name: "block ads", Action: proto.ActionBlock, Match: proto.Match{GeoSite: []string{"category-ads"}}})
	r2, _ := s.AddRule(proto.Rule{Name: "stream", Action: proto.ActionProxy, Target: "auto", Match: proto.Match{GeoSite: []string{"netflix"}}})
	if r1.Priority != 1 || r2.Priority != 2 {
		t.Fatalf("expected default priorities 1,2, got %d %d", r1.Priority, r2.Priority)
	}
	if err := s.DeleteRule(r1.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	snap := s.Snapshot()
	if len(snap.Rules) != 1 || snap.Rules[0].ID != r2.ID {
		t.Fatalf("expected only r2 after deletion: %+v", snap.Rules)
	}
}
