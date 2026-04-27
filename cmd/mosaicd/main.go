// mosaicd is the Mosaic VPN daemon. It acquires a single-instance lock,
// loads on-disk state, exposes an HTTP API on the loopback interface, and
// drives the VPN backend.
//
// The default backend is sing-box (real VPN engine). The `-mock` flag
// switches to a deterministic mock backend that simulates connect /
// disconnect transitions without touching the network — useful for
// development, integration tests, and CI environments where sing-box
// can't open a tun device or bind to privileged ports.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/pupspochta-cpu/mosaicvpn/internal/api"
	"github.com/pupspochta-cpu/mosaicvpn/internal/killswitch"
	"github.com/pupspochta-cpu/mosaicvpn/internal/logx"
	"github.com/pupspochta-cpu/mosaicvpn/internal/mcp"
	"github.com/pupspochta-cpu/mosaicvpn/internal/paths"
	"github.com/pupspochta-cpu/mosaicvpn/internal/proto"
	"github.com/pupspochta-cpu/mosaicvpn/internal/sbox"
	"github.com/pupspochta-cpu/mosaicvpn/internal/single"
	"github.com/pupspochta-cpu/mosaicvpn/internal/state"
	"github.com/pupspochta-cpu/mosaicvpn/internal/store"
)

// Version is set at build time via -ldflags "-X main.Version=...".
var Version = "0.1.0-dev"

func main() {
	var (
		dataDir = flag.String("data-dir", "", "override Mosaic data directory")
		verbose = flag.Bool("v", false, "verbose logging")
		mock    = flag.Bool("mock", false, "use the deterministic mock backend instead of sing-box")
	)
	flag.Parse()

	if *verbose {
		logx.SetLevel(logx.LevelDebug)
	}

	if err := run(*dataDir, *mock); err != nil {
		fmt.Fprintf(os.Stderr, "mosaicd: %v\n", err)
		os.Exit(1)
	}
}

func run(dataDirOverride string, useMock bool) error {
	dataDir := dataDirOverride
	if dataDir == "" {
		dataDir = paths.DataDir()
	}
	if err := paths.EnsureDir(dataDir); err != nil {
		return fmt.Errorf("create data dir: %w", err)
	}

	store, err := store.Open(paths.StoreFile(dataDir))
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}

	var backend state.Backend
	if useMock {
		backend = state.NewMockBackend()
	} else {
		backend = sbox.New()
	}
	mgr := state.New(store, backend, Version)
	ks := killswitch.New()
	// Sweep stale rules from a previous run before we begin servicing API
	// calls; if mosaicd crashed mid-connection, the OS firewall may still
	// have leftover Mosaic-KillSwitch-* rules.
	_ = ks.Disengage(context.Background())
	mgr.SetKillSwitch(ks)

	apiSrv := api.NewServer(store, mgr, nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	addr, shutdown, err := apiSrv.Listen(ctx)
	if err != nil {
		return fmt.Errorf("api listen: %w", err)
	}

	host, portStr, _ := net.SplitHostPort(addr)
	port, _ := strconv.Atoi(portStr)
	endpoint := proto.DaemonEndpoint{
		Host:    host,
		Port:    port,
		Token:   apiSrv.Token(),
		PID:     os.Getpid(),
		Version: Version,
		Started: time.Now().UTC().Format(time.RFC3339),
	}

	lock, prev, err := single.Acquire(paths.LockFile(dataDir), endpoint)
	if err != nil {
		_ = shutdown(context.Background())
		if errors.Is(err, single.ErrAlreadyRunning) && prev != nil {
			return fmt.Errorf("another daemon is already running on %s:%d (pid %d)",
				prev.Host, prev.Port, prev.PID)
		}
		return fmt.Errorf("acquire lock: %w", err)
	}
	defer lock.Release()

	logx.Info("mosaicd started",
		"version", Version,
		"data_dir", dataDir,
		"api", fmt.Sprintf("http://%s:%d", host, port),
		"backend", backend.Name(),
	)

	mcpSrv := mcp.New(store, mgr, mcp.Fetcher(api.HTTPFetcher(http.DefaultClient)))
	if err := mcpSrv.Start(ctx); err != nil {
		// MCP is optional surface; log and continue rather than fail the daemon.
		logx.Warn("mcp start failed", "err", err)
	}

	// Wait for SIGINT/SIGTERM.
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	<-sig

	logx.Info("shutting down")
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	_ = mgr.Disconnect(shutdownCtx)
	_ = mcpSrv.Stop(shutdownCtx)
	_ = shutdown(shutdownCtx)
	return nil
}
