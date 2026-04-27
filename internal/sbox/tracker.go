// Package sbox: tracker.go wires a sing-box ConnectionTracker that counts
// bytes flowing through routed inbound connections. The tracker is attached
// before Start() and aggregates totals for proto.Status.BytesIn / BytesOut.
package sbox

import (
	"context"
	"net"
	"sync/atomic"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing/common/bufio"
	N "github.com/sagernet/sing/common/network"
)

// byteTracker implements adapter.ConnectionTracker. Inbound's Read counts as
// upload (client → server) which Mosaic surfaces as BytesOut; inbound's Write
// counts as download (server → client) which surfaces as BytesIn.
type byteTracker struct {
	bytesIn  *atomic.Uint64 // server → client (downloaded)
	bytesOut *atomic.Uint64 // client → server (uploaded)
}

func newByteTracker(in, out *atomic.Uint64) *byteTracker {
	return &byteTracker{bytesIn: in, bytesOut: out}
}

func (t *byteTracker) RoutedConnection(_ context.Context, conn net.Conn, _ adapter.InboundContext, _ adapter.Rule, _ adapter.Outbound) net.Conn {
	return bufio.NewCounterConn(conn, []N.CountFunc{t.addOut}, []N.CountFunc{t.addIn})
}

func (t *byteTracker) RoutedPacketConnection(_ context.Context, conn N.PacketConn, _ adapter.InboundContext, _ adapter.Rule, _ adapter.Outbound) N.PacketConn {
	return bufio.NewCounterPacketConn(conn, []N.CountFunc{t.addOut}, []N.CountFunc{t.addIn})
}

func (t *byteTracker) addIn(n int64) {
	if n > 0 {
		t.bytesIn.Add(uint64(n))
	}
}

func (t *byteTracker) addOut(n int64) {
	if n > 0 {
		t.bytesOut.Add(uint64(n))
	}
}
