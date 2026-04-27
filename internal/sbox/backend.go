// Package sbox wires sing-box into Mosaic's state.Backend interface.
//
// The backend is intentionally thin: build a JSON config via sboxconfig,
// parse it through sing-box's option machinery, and drive the resulting
// *box.Box through Start/Close. Byte counters are populated by attaching
// a ConnectionTracker to the router before Start() so every routed
// connection is wrapped with read/write counters that feed atomic totals
// surfaced through Stats().
package sbox

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	box "github.com/sagernet/sing-box"
	"github.com/sagernet/sing-box/include"
	"github.com/sagernet/sing-box/option"

	"github.com/pupspochta-cpu/mosaicvpn/internal/proto"
	"github.com/pupspochta-cpu/mosaicvpn/internal/sboxconfig"
	"github.com/pupspochta-cpu/mosaicvpn/internal/store"
)

// Backend implements state.Backend on top of an embedded sing-box.
type Backend struct {
	mu     sync.Mutex
	box    *box.Box
	cancel context.CancelFunc
	rxIn   atomic.Uint64
	rxOut  atomic.Uint64
}

// New returns a fresh, idle Backend.
func New() *Backend {
	return &Backend{}
}

// Name implements state.Backend.
func (b *Backend) Name() string { return "sing-box" }

// Start implements state.Backend. It composes the sing-box config from the
// given server/prefs/rules, parses it through sing-box's option machinery
// and brings the engine up.
func (b *Backend) Start(ctx context.Context, server proto.Server, prefs store.Prefs, rules []proto.Rule) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.box != nil {
		return errors.New("sing-box backend already running")
	}
	cfg, err := sboxconfig.Build(server, prefs, rules)
	if err != nil {
		return fmt.Errorf("build config: %w", err)
	}
	raw, err := cfg.JSON()
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}

	bctx, cancel := context.WithCancel(ctx)
	bctx = include.Context(bctx)

	var opts option.Options
	if err := opts.UnmarshalJSONContext(bctx, raw); err != nil {
		cancel()
		return fmt.Errorf("parse config: %w", err)
	}

	instance, err := box.New(box.Options{
		Context: bctx,
		Options: opts,
	})
	if err != nil {
		cancel()
		return fmt.Errorf("instantiate sing-box: %w", err)
	}

	b.rxIn.Store(0)
	b.rxOut.Store(0)
	if router := instance.Router(); router != nil {
		router.AppendTracker(newByteTracker(&b.rxIn, &b.rxOut))
	}

	if err := instance.Start(); err != nil {
		_ = instance.Close()
		cancel()
		return fmt.Errorf("start sing-box: %w", err)
	}

	b.box = instance
	b.cancel = cancel
	return nil
}

// Stop implements state.Backend. Closing the box is bounded by a short
// timeout so a misbehaving outbound can't wedge the daemon shutdown path.
func (b *Backend) Stop(ctx context.Context) error {
	b.mu.Lock()
	inst := b.box
	cancel := b.cancel
	b.box = nil
	b.cancel = nil
	b.mu.Unlock()
	if inst == nil {
		return nil
	}

	done := make(chan error, 1)
	go func() {
		done <- inst.Close()
	}()
	timeout := 5 * time.Second
	select {
	case err := <-done:
		if cancel != nil {
			cancel()
		}
		return err
	case <-time.After(timeout):
		if cancel != nil {
			cancel()
		}
		return errors.New("sing-box close timed out")
	}
}

// Stats implements state.Backend. The byte counters are fed by a
// ConnectionTracker attached to the router; latency is reported as zero
// here and computed elsewhere (see internal/latency for per-server probes).
func (b *Backend) Stats() (uint64, uint64, int) {
	return b.rxIn.Load(), b.rxOut.Load(), 0
}

// MarshalConfig is a debug helper that returns the JSON config a Start
// call would build for the given inputs. It does not touch any state.
func MarshalConfig(server proto.Server, prefs store.Prefs, rules []proto.Rule) ([]byte, error) {
	cfg, err := sboxconfig.Build(server, prefs, rules)
	if err != nil {
		return nil, err
	}
	return json.MarshalIndent(cfg, "", "  ")
}
