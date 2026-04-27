package killswitch

import (
	"context"
	"net"
	"testing"
)

// TestNoop verifies the trivial driver — useful as a contract test for
// any future System implementation.
func TestNoop(t *testing.T) {
	var s System = Noop{}
	ctx := context.Background()
	if err := s.Engage(ctx, Params{DaemonPID: 1}); err != nil {
		t.Fatalf("Engage: %v", err)
	}
	if err := s.Disengage(ctx); err != nil {
		t.Fatalf("Disengage: %v", err)
	}
	if name := s.Name(); name != "noop" {
		t.Fatalf("Name: got %q, want %q", name, "noop")
	}
}

// TestResolveHosts checks that literal IPs survive the resolve helper
// untouched (DNS round-trips for hostnames are not exercised here to
// keep the test hermetic).
func TestResolveHosts(t *testing.T) {
	got := resolveHosts([]string{"8.8.8.8", "::1", "this-host-does-not-exist.invalid"})
	if len(got) != 2 {
		t.Fatalf("expected 2 IPs (literals), got %d: %+v", len(got), got)
	}
	if !got[0].Equal(net.ParseIP("8.8.8.8")) {
		t.Fatalf("got[0] = %v, want 8.8.8.8", got[0])
	}
	if !got[1].Equal(net.ParseIP("::1")) {
		t.Fatalf("got[1] = %v, want ::1", got[1])
	}
}
