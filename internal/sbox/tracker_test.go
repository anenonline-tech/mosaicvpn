package sbox

import (
	"context"
	"io"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sagernet/sing-box/adapter"
)

// TestByteTracker_TCPCounting wraps a net.Pipe pair with the tracker and
// asserts both directions are counted independently.
func TestByteTracker_TCPCounting(t *testing.T) {
	var in, out atomic.Uint64
	tr := newByteTracker(&in, &out)

	a, b := net.Pipe()
	t.Cleanup(func() { _ = a.Close(); _ = b.Close() })

	wrapped := tr.RoutedConnection(context.Background(), a, adapter.InboundContext{}, nil, nil)

	// Reader on `b`: bytes written by `wrapped.Write` arrive here.
	done := make(chan error, 1)
	got := make([]byte, 8)
	go func() {
		_, err := io.ReadFull(b, got)
		done <- err
	}()

	// Write 8 bytes from `wrapped` → counts as bytesIn (server → client).
	if _, err := wrapped.Write([]byte("payload!")); err != nil {
		t.Fatalf("wrapped.Write: %v", err)
	}
	if err := <-done; err != nil {
		t.Fatalf("read on b: %v", err)
	}

	// Now push 4 bytes the other way: write to `b`, read from `wrapped`.
	go func() {
		_, _ = b.Write([]byte("up!!"))
	}()
	buf := make([]byte, 4)
	if err := setReadDeadline(wrapped, 2*time.Second); err != nil {
		t.Fatalf("set deadline: %v", err)
	}
	if _, err := io.ReadFull(wrapped, buf); err != nil {
		t.Fatalf("ReadFull wrapped: %v", err)
	}

	if got := in.Load(); got != 8 {
		t.Fatalf("bytesIn = %d, want 8", got)
	}
	if got := out.Load(); got != 4 {
		t.Fatalf("bytesOut = %d, want 4", got)
	}
}

// setReadDeadline tries to set a deadline if the conn supports it.
func setReadDeadline(c net.Conn, d time.Duration) error {
	if dlc, ok := c.(interface {
		SetReadDeadline(time.Time) error
	}); ok {
		return dlc.SetReadDeadline(time.Now().Add(d))
	}
	return nil
}
