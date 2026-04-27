//go:build windows

package killswitch

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"sync"
)

// newPlatformSystem returns the netsh-backed Windows kill-switch driver.
func newPlatformSystem() System {
	return &netshSystem{}
}

// netshSystem drives Windows Defender Firewall through `netsh advfirewall`.
//
// Strategy: while engaged we own a single outbound block-all rule plus a
// handful of allow rules:
//
//   - Mosaic-KillSwitch-BlockAll      Block any
//   - Mosaic-KillSwitch-AllowDaemon   Allow program=<daemon exe>
//   - Mosaic-KillSwitch-AllowServer-N Allow remoteip=<server IP> port=<port>
//   - Mosaic-KillSwitch-AllowLAN      Allow remoteip=LocalSubnet (optional)
//
// Rule names are deterministic so a stale Engage from a previous run can
// be cleaned up by Disengage / sweep at startup.
type netshSystem struct {
	mu       sync.Mutex
	engaged  bool
	ruleTags []string
}

const (
	rulePrefix     = "Mosaic-KillSwitch-"
	ruleBlockAll   = rulePrefix + "BlockAll"
	ruleAllowDaemo = rulePrefix + "AllowDaemon"
	ruleAllowLAN   = rulePrefix + "AllowLAN"
)

func (s *netshSystem) Name() string { return "netsh" }

func (s *netshSystem) Engage(ctx context.Context, p Params) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.engaged {
		return nil
	}
	// Always sweep first so a previous-run leftover can't shadow our rules.
	_ = s.cleanupLocked(ctx)

	exe, err := daemonExecutable(p.DaemonPID)
	if err != nil {
		return fmt.Errorf("resolve daemon executable: %w", err)
	}

	tags := []string{ruleBlockAll}
	if err := netshAddRule(ctx, "name="+ruleBlockAll,
		"dir=out", "action=block", "enable=yes", "profile=any",
		"remoteip=any", "protocol=any"); err != nil {
		return err
	}

	if exe != "" {
		tags = append(tags, ruleAllowDaemo)
		if err := netshAddRule(ctx, "name="+ruleAllowDaemo,
			"dir=out", "action=allow", "enable=yes", "profile=any",
			"program="+exe); err != nil {
			_ = s.cleanupLocked(ctx)
			return err
		}
	}

	for i, ip := range resolveHosts(p.ServerHosts) {
		tag := fmt.Sprintf("%sAllowServer-%d", rulePrefix, i)
		args := []string{
			"name=" + tag,
			"dir=out", "action=allow", "enable=yes", "profile=any",
			"remoteip=" + ip.String(),
		}
		if len(p.ServerPorts) > 0 {
			ports := make([]string, len(p.ServerPorts))
			for j, port := range p.ServerPorts {
				ports[j] = fmt.Sprintf("%d", port)
			}
			args = append(args, "remoteport="+strings.Join(ports, ","), "protocol=tcp")
		}
		if err := netshAddRule(ctx, args...); err != nil {
			_ = s.cleanupLocked(ctx)
			return err
		}
		tags = append(tags, tag)
	}

	if p.AllowLAN {
		tags = append(tags, ruleAllowLAN)
		if err := netshAddRule(ctx, "name="+ruleAllowLAN,
			"dir=out", "action=allow", "enable=yes", "profile=any",
			"remoteip=LocalSubnet"); err != nil {
			_ = s.cleanupLocked(ctx)
			return err
		}
	}

	s.ruleTags = tags
	s.engaged = true
	return nil
}

func (s *netshSystem) Disengage(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cleanupLocked(ctx)
}

func (s *netshSystem) cleanupLocked(ctx context.Context) error {
	tags := s.ruleTags
	if len(tags) == 0 {
		// Best effort: also delete any lingering rules from a previous
		// process by name prefix. netsh accepts a literal name only, so
		// we have to enumerate the well-known tags.
		tags = []string{ruleBlockAll, ruleAllowDaemo, ruleAllowLAN}
		// AllowServer-* tags can't be deleted blindly without enumeration;
		// they get cleaned up if they were owned by this process.
	}
	var firstErr error
	for _, t := range tags {
		if err := netshDeleteRule(ctx, t); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	s.ruleTags = nil
	s.engaged = false
	return firstErr
}

func netshAddRule(ctx context.Context, args ...string) error {
	full := append([]string{"advfirewall", "firewall", "add", "rule"}, args...)
	cmd := exec.CommandContext(ctx, "netsh", full...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("netsh add rule: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func netshDeleteRule(ctx context.Context, name string) error {
	cmd := exec.CommandContext(ctx, "netsh", "advfirewall", "firewall", "delete", "rule", "name="+name)
	// netsh returns non-zero when the rule doesn't exist; treat that as success.
	out, err := cmd.CombinedOutput()
	if err != nil && !strings.Contains(strings.ToLower(string(out)), "no rules match") {
		return fmt.Errorf("netsh delete rule %q: %w (%s)", name, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// daemonExecutable returns the on-disk path of the daemon process so
// netsh can scope an allow rule to it.
func daemonExecutable(_ int) (string, error) {
	exe, err := osExecutable()
	if err != nil {
		return "", err
	}
	return exe, nil
}

// osExecutable is var so tests can stub it.
var osExecutable = func() (string, error) {
	return executableSelf()
}
