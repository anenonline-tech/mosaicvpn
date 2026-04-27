package sbox_test

import (
	"context"
	"net"
	"runtime"
	"strconv"
	"testing"
	"time"

	"github.com/pupspochta-cpu/mosaicvpn/internal/proto"
	"github.com/pupspochta-cpu/mosaicvpn/internal/sbox"
	"github.com/pupspochta-cpu/mosaicvpn/internal/store"
)

// TestBackend_StartStop_Proxy verifies that the sing-box backend can be
// started against a real local outbound (using a benign Shadowsocks config
// pointed at a non-existent server) in proxy mode (no tun device required)
// and cleanly stopped. We rely on sing-box validating the config and
// opening the SOCKS/HTTP listeners; the absence of real upstream
// connectivity is fine because we never push any traffic.
//
// Skipped on Windows because the test would still bind to user ports
// allocated dynamically here, and the cross-platform networking surface
// in CI is not worth the flakiness; the config-builder test in
// internal/sboxconfig already exercises the windows path.
func TestBackend_StartStop_Proxy(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skipped on windows; sboxconfig tests cover this path")
	}

	socksAddr, httpAddr := pickFreeAddrs(t, 2)

	prefs := store.DefaultPrefs()
	prefs.TunnelMode = "proxy"
	prefs.SocksAddr = socksAddr
	prefs.HTTPAddr = httpAddr

	server := proto.Server{
		Name:     "smoke",
		Protocol: proto.ProtoShadowsocks,
		Address:  "127.0.0.1",
		Port:     9 - 1 + 2, // arbitrary; we never connect upstream
		Raw: map[string]any{
			"method":   "aes-128-gcm",
			"password": "smoketest",
		},
	}

	b := sbox.New()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := b.Start(ctx, server, prefs, nil); err != nil {
		t.Fatalf("Start: %v", err)
	}

	// Confirm the SOCKS listener is up: dial it (no protocol exchange).
	c, err := net.DialTimeout("tcp", socksAddr, 2*time.Second)
	if err != nil {
		_ = b.Stop(context.Background())
		t.Fatalf("dial mixed inbound: %v", err)
	}
	_ = c.Close()

	if err := b.Stop(context.Background()); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	// Listener should be gone after Stop.
	if c, err := net.DialTimeout("tcp", socksAddr, 500*time.Millisecond); err == nil {
		_ = c.Close()
		t.Fatalf("mixed inbound still accepting connections after Stop")
	}
}

// pickFreeAddrs returns n free 127.0.0.1:port addresses by briefly
// listening to learn ports the kernel picks.
func pickFreeAddrs(t *testing.T, n int) (string, string) {
	t.Helper()
	ports := make([]string, 0, n)
	listeners := make([]net.Listener, 0, n)
	for i := 0; i < n; i++ {
		l, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatalf("listen: %v", err)
		}
		listeners = append(listeners, l)
		_, port, _ := net.SplitHostPort(l.Addr().String())
		ports = append(ports, "127.0.0.1:"+port)
	}
	for _, l := range listeners {
		_ = l.Close()
	}
	if _, err := strconv.Atoi(ports[0][len("127.0.0.1:"):]); err != nil {
		t.Fatalf("port parse: %v", err)
	}
	return ports[0], ports[1]
}

// TestBackend_RejectsDoubleStart asserts the backend doesn't allow
// concurrent sessions; the daemon's state machine should also enforce
// this but the backend is an additional safety net.
func TestBackend_RejectsDoubleStart(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skipped on windows")
	}

	socksAddr, httpAddr := pickFreeAddrs(t, 2)

	prefs := store.DefaultPrefs()
	prefs.TunnelMode = "proxy"
	prefs.SocksAddr = socksAddr
	prefs.HTTPAddr = httpAddr

	server := proto.Server{
		Name:     "double",
		Protocol: proto.ProtoShadowsocks,
		Address:  "127.0.0.1", Port: 19999,
		Raw: map[string]any{"method": "aes-128-gcm", "password": "x"},
	}

	b := sbox.New()
	ctx := context.Background()
	if err := b.Start(ctx, server, prefs, nil); err != nil {
		t.Fatalf("first Start: %v", err)
	}
	defer b.Stop(ctx)
	if err := b.Start(ctx, server, prefs, nil); err == nil {
		t.Fatalf("second Start should have failed")
	}
}
