// Package killswitch implements platform-level traffic blocking that
// activates when a Mosaic connection is up. It is the second of two
// kill-switch layers; the first is the sing-box rule rewrite that lives
// in internal/sboxconfig (direct → reject under prefs.KillSwitch).
//
// The platform layer adds a second, defence-in-depth wall: if the daemon
// ever crashes mid-connection, or if a misbehaving outbound somehow
// punches through the route engine, the host firewall still drops every
// outbound packet that isn't whitelisted. On Windows this is done via
// netsh advfirewall rules scoped to the daemon's PID and the configured
// upstream server endpoint; on Linux/macOS the implementation is a no-op
// for now (the sing-box layer is the only enforcement) and will be
// fleshed out alongside the eventual Linux/macOS Mosaic packaging.
package killswitch

import (
	"context"
	"net"
)

// Params describes the traffic that must remain reachable while the
// kill-switch is engaged.
type Params struct {
	// DaemonPID is the PID of the mosaicd process. Outbound packets from
	// this process are always allowed (it's the one keeping the tunnel
	// up).
	DaemonPID int
	// ServerHosts is the set of upstream server addresses (IPs or
	// hostnames) the tunnel needs to reach. Hostnames are resolved at
	// engage time on platforms that need IP-level rules.
	ServerHosts []string
	// ServerPorts is the set of upstream server ports. Empty means "any
	// port allowed for ServerHosts".
	ServerPorts []int
	// AllowLAN keeps RFC1918 destinations reachable for printers, NAS,
	// and the LAN web UI. Should reflect prefs.AllowLAN.
	AllowLAN bool
}

// System represents a host firewall driver. Implementations must be
// idempotent: repeated Engage / Disengage calls are well-defined.
type System interface {
	// Name returns a short identifier for logs ("netsh", "noop", ...).
	Name() string
	// Engage installs blocking rules and returns. Cleanup happens via
	// Disengage; if the daemon crashes, sweep on next start.
	Engage(ctx context.Context, p Params) error
	// Disengage removes any rules installed by Engage. Safe to call
	// multiple times.
	Disengage(ctx context.Context) error
}

// New returns the System appropriate for this platform.
func New() System {
	return newPlatformSystem()
}

// Noop is a System that does nothing. Useful in tests and on platforms
// where the platform driver is not yet implemented.
type Noop struct{}

// Name implements System.
func (Noop) Name() string { return "noop" }

// Engage implements System.
func (Noop) Engage(context.Context, Params) error { return nil }

// Disengage implements System.
func (Noop) Disengage(context.Context) error { return nil }

// resolveHosts is a helper for platform implementations that need IP
// addresses; it returns one IP per hostname (preferring IPv4) and skips
// names that fail to resolve.
func resolveHosts(hosts []string) []net.IP {
	var out []net.IP
	for _, h := range hosts {
		if ip := net.ParseIP(h); ip != nil {
			out = append(out, ip)
			continue
		}
		ips, err := net.LookupIP(h)
		if err != nil {
			continue
		}
		// Prefer IPv4 first, fall back to IPv6.
		var picked net.IP
		for _, ip := range ips {
			if ip4 := ip.To4(); ip4 != nil {
				picked = ip4
				break
			}
		}
		if picked == nil && len(ips) > 0 {
			picked = ips[0]
		}
		if picked != nil {
			out = append(out, picked)
		}
	}
	return out
}
