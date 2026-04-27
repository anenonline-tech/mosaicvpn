// Package state owns the connection state machine of the Mosaic daemon.
//
// Transitions:
//
//	disconnected → connecting → connected
//	connecting   → error      (failure during connect)
//	connected    → disconnected (user disconnect or backend stop)
//	connected    → error      (backend dropped unexpectedly)
//	error        → connecting (retry)
//
// All state changes go through the manager so we can serialise
// connect/disconnect, expose a single Status snapshot, and broadcast
// events to API clients.
package state

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/pupspochta-cpu/mosaicvpn/internal/logx"
	"github.com/pupspochta-cpu/mosaicvpn/internal/proto"
	"github.com/pupspochta-cpu/mosaicvpn/internal/store"
)

// Backend is the abstract VPN engine the manager drives. Phase 1 ships a
// MockBackend; Phase 2 supplies a sing-box backed implementation.
type Backend interface {
	Name() string
	Start(ctx context.Context, server proto.Server, prefs store.Prefs, rules []proto.Rule) error
	Stop(ctx context.Context) error
	Stats() (bytesIn, bytesOut uint64, latencyMS int)
}

// StatsHeartbeat is how often the manager rebroadcasts Status while
// connected so subscribers can observe live byte/latency counters
// without polling. The backend itself updates its counters at a
// finer cadence; this is just the SSE heartbeat.
const StatsHeartbeat = 1 * time.Second

// Manager owns the connection state and is safe for concurrent use.
type Manager struct {
	mu      sync.Mutex
	st      proto.Status
	store   *store.Store
	backend Backend
	cancel  context.CancelFunc
	subs    []chan proto.Status
	version string
	pid     int
	started time.Time

	heartbeat time.Duration
}

// New constructs a Manager around an existing store and backend.
func New(s *store.Store, backend Backend, version string) *Manager {
	m := &Manager{
		store:     s,
		backend:   backend,
		version:   version,
		pid:       osPID(),
		started:   time.Now().UTC(),
		heartbeat: StatsHeartbeat,
	}
	prefs := s.Snapshot().Prefs
	m.st = proto.Status{
		State:         proto.StateDisconnected,
		TunnelMode:    prefs.TunnelMode,
		KillSwitch:    prefs.KillSwitch,
		DaemonVersion: version,
		DaemonPID:     m.pid,
	}
	return m
}

// SetHeartbeat overrides the cadence at which Status is rebroadcast
// while connected. Used by tests; production code keeps the default.
func (m *Manager) SetHeartbeat(d time.Duration) {
	m.mu.Lock()
	m.heartbeat = d
	m.mu.Unlock()
}

// Status returns a snapshot of the current state, with live counters from
// the backend folded in.
func (m *Manager) Status() proto.Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	st := m.st
	if m.backend != nil && st.State == proto.StateConnected {
		in, out, lat := m.backend.Stats()
		st.BytesIn = in
		st.BytesOut = out
		st.LatencyMS = lat
	}
	return st
}

// Subscribe returns a buffered channel that receives every Status update.
// The returned cancel function detaches the subscriber.
func (m *Manager) Subscribe() (<-chan proto.Status, func()) {
	ch := make(chan proto.Status, 16)
	m.mu.Lock()
	m.subs = append(m.subs, ch)
	m.mu.Unlock()
	cancel := func() {
		m.mu.Lock()
		out := m.subs[:0]
		for _, c := range m.subs {
			if c != ch {
				out = append(out, c)
			}
		}
		m.subs = out
		m.mu.Unlock()
		close(ch)
	}
	return ch, cancel
}

// Connect starts the backend against the supplied server. If the manager
// is already connected, it will disconnect first.
func (m *Manager) Connect(ctx context.Context, serverID string) error {
	server, ok := m.store.FindServer(serverID)
	if !ok {
		return fmt.Errorf("server %q not found", serverID)
	}
	m.mu.Lock()
	if m.st.State == proto.StateConnecting {
		m.mu.Unlock()
		return errors.New("already connecting")
	}
	if m.st.State == proto.StateConnected || m.st.State == proto.StateError {
		m.mu.Unlock()
		_ = m.Disconnect(ctx)
		m.mu.Lock()
	}

	m.transitionLocked(proto.Status{
		State:         proto.StateConnecting,
		Server:        &server,
		Since:         time.Now().UTC(),
		TunnelMode:    m.st.TunnelMode,
		KillSwitch:    m.st.KillSwitch,
		DaemonVersion: m.st.DaemonVersion,
		DaemonPID:     m.st.DaemonPID,
	})

	cctx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel
	m.mu.Unlock()

	snap := m.store.Snapshot()
	if err := m.backend.Start(cctx, server, snap.Prefs, snap.Rules); err != nil {
		m.mu.Lock()
		m.transitionLocked(proto.Status{
			State:         proto.StateError,
			LastError:     err.Error(),
			Server:        &server,
			Since:         time.Now().UTC(),
			TunnelMode:    m.st.TunnelMode,
			KillSwitch:    m.st.KillSwitch,
			DaemonVersion: m.st.DaemonVersion,
			DaemonPID:     m.st.DaemonPID,
		})
		m.mu.Unlock()
		return err
	}

	m.mu.Lock()
	m.transitionLocked(proto.Status{
		State:         proto.StateConnected,
		Server:        &server,
		Since:         time.Now().UTC(),
		TunnelMode:    m.st.TunnelMode,
		KillSwitch:    m.st.KillSwitch,
		DaemonVersion: m.st.DaemonVersion,
		DaemonPID:     m.st.DaemonPID,
	})
	m.mu.Unlock()

	go m.statsHeartbeat(cctx)

	if err := m.store.SetLastServer(serverID); err != nil {
		logx.Warn("could not persist last server", "err", err)
	}
	return nil
}

// statsHeartbeat rebroadcasts Status (with fresh backend counters folded
// in) while the connection is up. It exits when ctx is cancelled — i.e.
// on Disconnect, since Connect uses the same cancel.
func (m *Manager) statsHeartbeat(ctx context.Context) {
	if m.heartbeat <= 0 {
		return
	}
	t := time.NewTicker(m.heartbeat)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			m.mu.Lock()
			if m.st.State != proto.StateConnected {
				m.mu.Unlock()
				return
			}
			st := m.st
			if m.backend != nil {
				in, out, lat := m.backend.Stats()
				st.BytesIn = in
				st.BytesOut = out
				st.LatencyMS = lat
			}
			for _, ch := range m.subs {
				select {
				case ch <- st:
				default:
				}
			}
			m.mu.Unlock()
		}
	}
}

// Disconnect stops the backend and returns to disconnected state.
func (m *Manager) Disconnect(ctx context.Context) error {
	m.mu.Lock()
	if m.st.State == proto.StateDisconnected {
		m.mu.Unlock()
		return nil
	}
	if m.cancel != nil {
		m.cancel()
		m.cancel = nil
	}
	m.mu.Unlock()

	if err := m.backend.Stop(ctx); err != nil {
		return err
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	m.transitionLocked(proto.Status{
		State:         proto.StateDisconnected,
		Since:         time.Now().UTC(),
		TunnelMode:    m.st.TunnelMode,
		KillSwitch:    m.st.KillSwitch,
		DaemonVersion: m.st.DaemonVersion,
		DaemonPID:     m.st.DaemonPID,
	})
	return nil
}

// SetTunnelPrefs informs the manager of the current tunnel-mode/kill-switch
// configuration so that Status reflects them even before reconnect.
func (m *Manager) SetTunnelPrefs(mode string, killSwitch bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	st := m.st
	st.TunnelMode = mode
	st.KillSwitch = killSwitch
	m.transitionLocked(st)
}

// Started returns when the daemon began running.
func (m *Manager) Started() time.Time { return m.started }

// PID returns the daemon process id.
func (m *Manager) PID() int { return m.pid }

// Version returns the daemon version string.
func (m *Manager) Version() string { return m.version }

func (m *Manager) transitionLocked(next proto.Status) {
	m.st = next
	for _, ch := range m.subs {
		select {
		case ch <- next:
		default:
			// drop if subscriber is slow; subscribers should be fast.
		}
	}
}

// osPID returns the daemon's PID. Wrapped so tests can override.
var osPID = func() int { return os.Getpid() }
