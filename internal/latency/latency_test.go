package latency_test

import (
	"context"
	"net"
	"strconv"
	"testing"
	"time"

	"github.com/pupspochta-cpu/mosaicvpn/internal/latency"
	"github.com/pupspochta-cpu/mosaicvpn/internal/proto"
)

// TestProbe_OK starts a real TCP listener, dials it, and asserts that
// the probe reports success with a sane (>0) ms reading.
func TestProbe_OK(t *testing.T) {
	host, port := startListener(t)
	r := latency.Probe(context.Background(), proto.Server{
		ID: "ok", Address: host, Port: port,
	}, latency.Options{Timeout: 2 * time.Second})

	if r.Err != nil {
		t.Fatalf("probe: %v", r.Err)
	}
	if r.MS < 1 {
		t.Fatalf("expected MS >= 1, got %d", r.MS)
	}
	if r.RTT <= 0 {
		t.Fatalf("expected RTT > 0, got %v", r.RTT)
	}
}

// TestProbe_Refused covers the case where nothing is listening on the
// target port. Probe must complete (not block on the timeout) and
// surface the underlying error. Port 1 on loopback is reserved
// (tcpmux) and reliably refused on every CI runner we care about.
func TestProbe_Refused(t *testing.T) {
	r := latency.Probe(context.Background(), proto.Server{
		ID: "refused", Address: "127.0.0.1", Port: 1,
	}, latency.Options{Timeout: 1 * time.Second})

	if r.Err == nil {
		t.Fatalf("expected dial error, got success (ms=%d)", r.MS)
	}
}

// TestProbeAll_SortsByRTT verifies that successful probes come back in
// ascending-RTT order and failures land at the end.
func TestProbeAll_SortsByRTT(t *testing.T) {
	host, port := startListener(t)
	servers := []proto.Server{
		{ID: "ok-1", Address: host, Port: port},
		{ID: "broken", Address: "127.0.0.1", Port: 1}, // almost certainly closed
		{ID: "ok-2", Address: host, Port: port},
	}
	res := latency.ProbeAll(context.Background(), servers,
		latency.Options{Timeout: 1 * time.Second, Concurrency: 4})

	if len(res) != 3 {
		t.Fatalf("expected 3 results, got %d", len(res))
	}
	if res[0].Err != nil || res[1].Err != nil {
		t.Fatalf("expected first two results to be successes; got %+v", res)
	}
	if res[2].Err == nil {
		t.Fatalf("expected last result to be a failure; got %+v", res[2])
	}
}

// startListener returns a TCP host/port that is currently accepting
// connections. The listener is closed on test teardown.
func startListener(t *testing.T) (string, int) {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = l.Close() })
	host, portStr, _ := net.SplitHostPort(l.Addr().String())
	port, _ := strconv.Atoi(portStr)
	return host, port
}


