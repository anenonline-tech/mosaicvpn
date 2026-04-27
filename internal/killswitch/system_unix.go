//go:build !windows

package killswitch

// newPlatformSystem returns the kill-switch driver for non-Windows hosts.
// The Linux/macOS firewall integrations have not landed yet — Mosaic ships
// for Windows first — so we return the no-op driver here. The sing-box
// rule rewrite in internal/sboxconfig is still active and provides
// best-effort kill-switch behaviour at the user-space routing layer.
func newPlatformSystem() System {
	return Noop{}
}
