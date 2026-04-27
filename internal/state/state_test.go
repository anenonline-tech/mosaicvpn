package state_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/pupspochta-cpu/mosaicvpn/internal/proto"
	"github.com/pupspochta-cpu/mosaicvpn/internal/state"
	"github.com/pupspochta-cpu/mosaicvpn/internal/store"
)

func newSetup(t *testing.T) (*store.Store, *state.MockBackend, *state.Manager, proto.Server) {
	t.Helper()
	s, err := store.Open(filepath.Join(t.TempDir(), "store.json"))
	if err != nil {
		t.Fatal(err)
	}
	srv := proto.Server{
		ID:       "tokyo",
		Name:     "Tokyo",
		Protocol: proto.ProtoVLESS,
		Address:  "1.2.3.4",
		Port:     443,
	}
	sub, _ := s.AddOrUpdateSubscription(proto.Subscription{URL: "u", Name: "n"})
	if err := s.ReplaceServersFor(sub.ID, []proto.Server{srv}); err != nil {
		t.Fatal(err)
	}
	srv.SubscriptionID = sub.ID

	mb := state.NewMockBackend()
	mgr := state.New(s, mb, "test")
	return s, mb, mgr, srv
}

func TestInitialState(t *testing.T) {
	_, _, mgr, _ := newSetup(t)
	st := mgr.Status()
	if st.State != proto.StateDisconnected {
		t.Fatalf("expected disconnected, got %s", st.State)
	}
	if st.DaemonVersion != "test" {
		t.Fatalf("expected daemon version test, got %q", st.DaemonVersion)
	}
}

func TestConnectDisconnect(t *testing.T) {
	_, _, mgr, srv := newSetup(t)
	if err := mgr.Connect(context.Background(), srv.ID); err != nil {
		t.Fatalf("connect: %v", err)
	}
	st := mgr.Status()
	if st.State != proto.StateConnected {
		t.Fatalf("expected connected, got %s", st.State)
	}
	if st.Server == nil || st.Server.ID != srv.ID {
		t.Fatalf("expected server set, got %+v", st.Server)
	}
	if err := mgr.Disconnect(context.Background()); err != nil {
		t.Fatalf("disconnect: %v", err)
	}
	st = mgr.Status()
	if st.State != proto.StateDisconnected {
		t.Fatalf("expected disconnected, got %s", st.State)
	}
}

func TestConnectErrorTransitionsToError(t *testing.T) {
	_, mb, mgr, srv := newSetup(t)
	mb.SetStartError(errors.New("boom"))

	if err := mgr.Connect(context.Background(), srv.ID); err == nil {
		t.Fatal("expected connect to fail")
	}
	st := mgr.Status()
	if st.State != proto.StateError {
		t.Fatalf("expected error state, got %s", st.State)
	}
	if st.LastError == "" {
		t.Fatal("expected non-empty LastError")
	}
}

func TestConnectUnknownServer(t *testing.T) {
	_, _, mgr, _ := newSetup(t)
	if err := mgr.Connect(context.Background(), "no-such"); err == nil {
		t.Fatal("expected error for unknown server")
	}
}

func TestSubscribeReceivesEvents(t *testing.T) {
	_, _, mgr, srv := newSetup(t)
	ch, cancel := mgr.Subscribe()
	defer cancel()

	go func() {
		_ = mgr.Connect(context.Background(), srv.ID)
	}()

	want := map[proto.State]bool{
		proto.StateConnecting: false,
		proto.StateConnected:  false,
	}
	deadline := time.After(2 * time.Second)
	for {
		select {
		case ev, ok := <-ch:
			if !ok {
				t.Fatal("subscription channel closed")
			}
			if _, tracked := want[ev.State]; tracked {
				want[ev.State] = true
				if want[proto.StateConnecting] && want[proto.StateConnected] {
					return
				}
			}
		case <-deadline:
			t.Fatalf("did not see connecting+connected events; got %+v", want)
		}
	}
}

// TestStatsTickerBroadcasts asserts that, while connected, subscribers get
// periodic Status events with updated byte counters even without explicit
// state transitions.
func TestStatsTickerBroadcasts(t *testing.T) {
	prev := state.StatsInterval
	state.StatsInterval = 50 * time.Millisecond
	t.Cleanup(func() { state.StatsInterval = prev })

	_, _, mgr, srv := newSetup(t)
	ch, cancel := mgr.Subscribe()
	defer cancel()

	if err := mgr.Connect(context.Background(), srv.ID); err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer mgr.Disconnect(context.Background())

	deadline := time.After(3 * time.Second)
	var firstBytes uint64
	var sawDelta bool
	for !sawDelta {
		select {
		case ev, ok := <-ch:
			if !ok {
				t.Fatal("subscription channel closed")
			}
			if ev.State != proto.StateConnected {
				continue
			}
			if firstBytes == 0 {
				firstBytes = ev.BytesIn
				continue
			}
			if ev.BytesIn > firstBytes {
				sawDelta = true
			}
		case <-deadline:
			t.Fatalf("did not see byte counter advance via stats ticker; first=%d", firstBytes)
		}
	}
}

func TestPersistsLastServer(t *testing.T) {
	s, _, mgr, srv := newSetup(t)
	if err := mgr.Connect(context.Background(), srv.ID); err != nil {
		t.Fatal(err)
	}
	if got := s.Snapshot().LastServerID; got != srv.ID {
		t.Fatalf("expected LastServerID=%q, got %q", srv.ID, got)
	}
}
