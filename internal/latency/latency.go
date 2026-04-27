// Package latency probes server reachability with simple TCP handshakes.
//
// We deliberately don't speak any of the proxy protocols here: a full
// VLESS / Hysteria / SS handshake would be substantially more work to
// implement and would also be measured _through_ a TLS / utls stack
// that depends on shared state with sing-box. A bare TCP dial to
// (server.Address, server.Port) is a faithful proxy for "can I reach
// the upstream at all" and is what every other client (Clash Meta,
// NekoBox, etc.) does for its own latency UI.
package latency

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/pupspochta-cpu/mosaicvpn/internal/proto"
)

// Result is the outcome of probing a single server.
type Result struct {
	ServerID string
	MS       int           // round-trip in milliseconds; meaningful only when Err == nil.
	RTT      time.Duration // raw timing for callers that want sub-ms precision.
	Err      error
	At       time.Time
}

// Options configures Probe / ProbeAll.
type Options struct {
	// Timeout is the hard ceiling for a single TCP handshake. Default 5s.
	Timeout time.Duration

	// Concurrency caps how many handshakes ProbeAll runs in parallel.
	// Default 16. Values <=0 fall back to the default.
	Concurrency int

	// Dialer is exposed for tests. Production callers can leave it nil.
	Dialer Dialer
}

// Dialer is a tiny seam that lets tests substitute net.Dial.
type Dialer interface {
	DialContext(ctx context.Context, network, address string) (net.Conn, error)
}

func (o Options) timeout() time.Duration {
	if o.Timeout <= 0 {
		return 5 * time.Second
	}
	return o.Timeout
}

func (o Options) concurrency() int {
	if o.Concurrency <= 0 {
		return 16
	}
	return o.Concurrency
}

func (o Options) dialer() Dialer {
	if o.Dialer != nil {
		return o.Dialer
	}
	return &net.Dialer{}
}

// Probe times a single TCP handshake to the server's upstream address.
// The returned Result is populated even on failure (Err != nil).
func Probe(ctx context.Context, s proto.Server, opts Options) Result {
	addr := net.JoinHostPort(s.Address, strconv.Itoa(s.Port))
	at := time.Now()

	dialCtx, cancel := context.WithTimeout(ctx, opts.timeout())
	defer cancel()

	start := time.Now()
	c, err := opts.dialer().DialContext(dialCtx, "tcp", addr)
	rtt := time.Since(start)

	r := Result{ServerID: s.ID, RTT: rtt, At: at}
	if err != nil {
		r.Err = fmt.Errorf("dial %s: %w", addr, err)
		return r
	}
	_ = c.Close()
	r.MS = int(rtt.Milliseconds())
	if r.MS == 0 {
		r.MS = 1 // never report 0 — it confuses sort-ascending UIs.
	}
	return r
}

// ProbeAll probes every server concurrently up to opts.Concurrency. The
// returned slice is sorted ascending by RTT, with failures last.
func ProbeAll(ctx context.Context, servers []proto.Server, opts Options) []Result {
	if len(servers) == 0 {
		return nil
	}
	results := make([]Result, len(servers))
	sem := make(chan struct{}, opts.concurrency())
	var wg sync.WaitGroup
	for i, s := range servers {
		i, s := i, s
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			results[i] = Probe(ctx, s, opts)
		}()
	}
	wg.Wait()

	sort.SliceStable(results, func(i, j int) bool {
		ai, aj := results[i].Err, results[j].Err
		switch {
		case ai == nil && aj != nil:
			return true
		case ai != nil && aj == nil:
			return false
		case ai != nil && aj != nil:
			return false
		}
		return results[i].RTT < results[j].RTT
	})
	return results
}

// ErrCancelled is returned by Probe when the parent context is cancelled
// before the handshake completes. Callers can use errors.Is to detect it.
var ErrCancelled = errors.New("latency probe cancelled")
