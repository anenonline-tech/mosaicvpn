// mosaic is the CLI client for the Mosaic VPN daemon.
//
// All commands are thin wrappers around the daemon's HTTP API: the CLI
// reads the lockfile, attaches the bearer token, and prints either a
// human-friendly view or JSON if --json is set. Designed so an AI agent
// can drive the daemon end-to-end without touching the GUI.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/pupspochta-cpu/mosaicvpn/internal/apiclient"
	"github.com/pupspochta-cpu/mosaicvpn/internal/paths"
	"github.com/pupspochta-cpu/mosaicvpn/internal/proto"
	"github.com/pupspochta-cpu/mosaicvpn/internal/store"
)

// Version is overridable via -ldflags.
var Version = "0.1.0-dev"

type cliOpts struct {
	dataDir string
	jsonOut bool
}

func main() {
	if err := newRoot().Execute(); err != nil {
		os.Exit(1)
	}
}

func newRoot() *cobra.Command {
	opts := &cliOpts{}
	root := &cobra.Command{
		Use:           "mosaic",
		Short:         "Control the Mosaic VPN daemon.",
		SilenceUsage:  true,
		SilenceErrors: false,
		Version:       Version,
	}
	root.PersistentFlags().StringVar(&opts.dataDir, "data-dir", "", "override Mosaic data directory")
	root.PersistentFlags().BoolVar(&opts.jsonOut, "json", false, "emit JSON instead of human output")

	root.AddCommand(
		newStatusCmd(opts),
		newConnectCmd(opts),
		newDisconnectCmd(opts),
		newSubCmd(opts),
		newServersCmd(opts),
		newRuleCmd(opts),
		newPrefsCmd(opts),
		newDiagCmd(opts),
	)
	return root
}

func resolveDataDir(opts *cliOpts) string {
	if opts.dataDir != "" {
		return opts.dataDir
	}
	return paths.DataDir()
}

func newClient(opts *cliOpts) (*apiclient.Client, error) {
	c, err := apiclient.FromLockfile(paths.LockFile(resolveDataDir(opts)))
	if err != nil {
		return nil, err
	}
	return c, nil
}

func emit(opts *cliOpts, w io.Writer, v any, human func(io.Writer)) error {
	if opts.jsonOut {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(v)
	}
	human(w)
	return nil
}

// ---------- status / connect / disconnect ---------------------------------

func newStatusCmd(opts *cliOpts) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show current daemon and connection status.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, err := newClient(opts)
			if err != nil {
				return handleNoDaemon(cmd, opts, err)
			}
			st, err := c.Status(cmd.Context())
			if err != nil {
				return err
			}
			return emit(opts, cmd.OutOrStdout(), st, func(w io.Writer) {
				printStatus(w, st)
			})
		},
	}
}

func newConnectCmd(opts *cliOpts) *cobra.Command {
	return &cobra.Command{
		Use:   "connect <server-id-or-name>",
		Short: "Connect to a server by ID or name.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newClient(opts)
			if err != nil {
				return err
			}
			id, err := resolveServerID(cmd.Context(), c, args[0])
			if err != nil {
				return err
			}
			st, err := c.Connect(cmd.Context(), id)
			if err != nil {
				return err
			}
			return emit(opts, cmd.OutOrStdout(), st, func(w io.Writer) {
				printStatus(w, st)
			})
		},
	}
}

func newDisconnectCmd(opts *cliOpts) *cobra.Command {
	return &cobra.Command{
		Use:   "disconnect",
		Short: "Drop the current connection.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, err := newClient(opts)
			if err != nil {
				return err
			}
			st, err := c.Disconnect(cmd.Context())
			if err != nil {
				return err
			}
			return emit(opts, cmd.OutOrStdout(), st, func(w io.Writer) {
				printStatus(w, st)
			})
		},
	}
}

// ---------- subscriptions -------------------------------------------------

func newSubCmd(opts *cliOpts) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "sub",
		Aliases: []string{"subs", "subscription"},
		Short:   "Manage server subscriptions.",
	}
	cmd.AddCommand(
		&cobra.Command{
			Use:   "list",
			Short: "List configured subscriptions.",
			RunE: func(cmd *cobra.Command, _ []string) error {
				c, err := newClient(opts)
				if err != nil {
					return err
				}
				subs, err := c.Subscriptions(cmd.Context())
				if err != nil {
					return err
				}
				return emit(opts, cmd.OutOrStdout(), subs, func(w io.Writer) {
					if len(subs) == 0 {
						fmt.Fprintln(w, "(no subscriptions configured)")
						return
					}
					for _, s := range subs {
						fmt.Fprintf(w, "%s  %s\n", s.ID, s.Name)
						fmt.Fprintf(w, "  url:     %s\n", s.URL)
						fmt.Fprintf(w, "  format:  %s\n", s.Format)
						fmt.Fprintf(w, "  servers: %d\n", s.ServerCount)
						if !s.LastFetched.IsZero() {
							fmt.Fprintf(w, "  fetched: %s\n", s.LastFetched.Local().Format(time.RFC3339))
						}
						if s.LastError != "" {
							fmt.Fprintf(w, "  error:   %s\n", s.LastError)
						}
						fmt.Fprintln(w)
					}
				})
			},
		},
		func() *cobra.Command {
			var (
				name   string
				format string
			)
			c := &cobra.Command{
				Use:   "add <url>",
				Short: "Add and immediately fetch a subscription.",
				Args:  cobra.ExactArgs(1),
				RunE: func(cmd *cobra.Command, args []string) error {
					rawURL := args[0]
					if _, err := url.Parse(rawURL); err != nil {
						return fmt.Errorf("invalid url: %w", err)
					}
					cl, err := newClient(opts)
					if err != nil {
						return err
					}
					sub, err := cl.AddSubscription(cmd.Context(), proto.AddSubscriptionRequest{
						URL:    rawURL,
						Name:   name,
						Format: proto.Format(format),
					})
					if err != nil {
						return err
					}
					return emit(opts, cmd.OutOrStdout(), sub, func(w io.Writer) {
						fmt.Fprintf(w, "added %s (%s) — %d servers\n", sub.Name, sub.Format, sub.ServerCount)
					})
				},
			}
			c.Flags().StringVar(&name, "name", "", "human-friendly name (defaults to URL)")
			c.Flags().StringVar(&format, "format", "", "force format: singbox|clash|v2ray-base64|sip008")
			return c
		}(),
		&cobra.Command{
			Use:   "refresh <id>",
			Short: "Refetch a subscription.",
			Args:  cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				cl, err := newClient(opts)
				if err != nil {
					return err
				}
				sub, err := cl.RefreshSubscription(cmd.Context(), args[0])
				if err != nil {
					return err
				}
				return emit(opts, cmd.OutOrStdout(), sub, func(w io.Writer) {
					fmt.Fprintf(w, "refreshed %s — %d servers\n", sub.Name, sub.ServerCount)
				})
			},
		},
		&cobra.Command{
			Use:   "delete <id>",
			Short: "Delete a subscription and its servers.",
			Args:  cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				cl, err := newClient(opts)
				if err != nil {
					return err
				}
				if err := cl.DeleteSubscription(cmd.Context(), args[0]); err != nil {
					return err
				}
				fmt.Fprintln(cmd.OutOrStdout(), "deleted")
				return nil
			},
		},
	)
	return cmd
}

// ---------- servers -------------------------------------------------------

func newServersCmd(opts *cliOpts) *cobra.Command {
	var subID string
	c := &cobra.Command{
		Use:   "servers",
		Short: "List known servers.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cl, err := newClient(opts)
			if err != nil {
				return err
			}
			servers, err := cl.Servers(cmd.Context(), subID)
			if err != nil {
				return err
			}
			sort.SliceStable(servers, func(i, j int) bool {
				return servers[i].Name < servers[j].Name
			})
			return emit(opts, cmd.OutOrStdout(), servers, func(w io.Writer) {
				if len(servers) == 0 {
					fmt.Fprintln(w, "(no servers; add a subscription with `mosaic sub add`)")
					return
				}
				for _, s := range servers {
					fmt.Fprintf(w, "%-12s  %-10s  %-22s  %s:%d\n",
						truncate(s.ID, 12), s.Protocol, truncate(s.Name, 22), s.Address, s.Port)
				}
			})
		},
	}
	c.Flags().StringVar(&subID, "subscription", "", "filter by subscription id")
	return c
}

// ---------- rules ---------------------------------------------------------

func newRuleCmd(opts *cliOpts) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "rule",
		Aliases: []string{"rules"},
		Short:   "Inspect routing rules.",
	}
	cmd.AddCommand(
		&cobra.Command{
			Use:   "list",
			Short: "List rules in priority order.",
			RunE: func(cmd *cobra.Command, _ []string) error {
				cl, err := newClient(opts)
				if err != nil {
					return err
				}
				rules, err := cl.Rules(cmd.Context())
				if err != nil {
					return err
				}
				return emit(opts, cmd.OutOrStdout(), rules, func(w io.Writer) {
					if len(rules) == 0 {
						fmt.Fprintln(w, "(no rules; daemon falls through to default direct)")
						return
					}
					for _, r := range rules {
						state := "on"
						if !r.Enabled {
							state = "off"
						}
						fmt.Fprintf(w, "%3d  [%s]  %-12s  %-7s  %s\n",
							r.Priority, state, truncate(r.ID, 12), r.Action, r.Name)
					}
				})
			},
		},
		&cobra.Command{
			Use:   "delete <id>",
			Short: "Delete a rule.",
			Args:  cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				cl, err := newClient(opts)
				if err != nil {
					return err
				}
				if err := cl.DeleteRule(cmd.Context(), args[0]); err != nil {
					return err
				}
				fmt.Fprintln(cmd.OutOrStdout(), "deleted")
				return nil
			},
		},
	)
	return cmd
}

// ---------- prefs ---------------------------------------------------------

func newPrefsCmd(opts *cliOpts) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "prefs",
		Short: "Show or update preferences.",
	}
	cmd.AddCommand(
		&cobra.Command{
			Use:   "show",
			Short: "Print preferences.",
			RunE: func(cmd *cobra.Command, _ []string) error {
				cl, err := newClient(opts)
				if err != nil {
					return err
				}
				p, err := cl.Prefs(cmd.Context())
				if err != nil {
					return err
				}
				return emit(opts, cmd.OutOrStdout(), p, func(w io.Writer) {
					printPrefs(w, p)
				})
			},
		},
		func() *cobra.Command {
			var (
				killSwitch string
				tunnelMode string
				tunStack   string
				autoStart  string
				blockIPv6  string
				shareLAN   string
			)
			c := &cobra.Command{
				Use:   "set",
				Short: "Update individual preferences.",
				RunE: func(cmd *cobra.Command, _ []string) error {
					cl, err := newClient(opts)
					if err != nil {
						return err
					}
					p, err := cl.Prefs(cmd.Context())
					if err != nil {
						return err
					}
					if killSwitch != "" {
						p.KillSwitch = parseBool(killSwitch)
					}
					if tunnelMode != "" {
						p.TunnelMode = tunnelMode
					}
					if tunStack != "" {
						p.TunStack = tunStack
					}
					if autoStart != "" {
						p.AutoStart = autoStart
					}
					if blockIPv6 != "" {
						p.BlockIPv6 = parseBool(blockIPv6)
					}
					if shareLAN != "" {
						p.ShareLAN = parseBool(shareLAN)
					}
					out, err := cl.SetPrefs(cmd.Context(), p)
					if err != nil {
						return err
					}
					return emit(opts, cmd.OutOrStdout(), out, func(w io.Writer) {
						printPrefs(w, out)
					})
				},
			}
			c.Flags().StringVar(&killSwitch, "kill-switch", "", "true/false")
			c.Flags().StringVar(&tunnelMode, "tunnel", "", "tun|proxy")
			c.Flags().StringVar(&tunStack, "tun-stack", "", "system|gvisor|mixed")
			c.Flags().StringVar(&autoStart, "auto-start", "", "service|user|manual")
			c.Flags().StringVar(&blockIPv6, "block-ipv6", "", "true/false")
			c.Flags().StringVar(&shareLAN, "share-lan", "", "true/false")
			return c
		}(),
	)
	return cmd
}

// ---------- diag ----------------------------------------------------------

func newDiagCmd(opts *cliOpts) *cobra.Command {
	return &cobra.Command{
		Use:   "diag",
		Short: "Print a diagnostic snapshot for support / agent debugging.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cl, err := newClient(opts)
			if err != nil {
				return err
			}
			rep, err := cl.Diag(cmd.Context())
			if err != nil {
				return err
			}
			return emit(opts, cmd.OutOrStdout(), rep, func(w io.Writer) {
				fmt.Fprintf(w, "daemon:    %s (pid %d)\n", rep.DaemonVersion, rep.Status.DaemonPID)
				fmt.Fprintf(w, "state:     %s\n", rep.Status.State)
				if rep.Status.Server != nil {
					fmt.Fprintf(w, "server:    %s [%s]\n", rep.Status.Server.Name, rep.Status.Server.Protocol)
				}
				fmt.Fprintf(w, "subs:      %d\n", len(rep.Subscriptions))
				fmt.Fprintf(w, "servers:   %d\n", rep.ServerCount)
				fmt.Fprintf(w, "rules:     %d\n", rep.RuleCount)
				fmt.Fprintf(w, "generated: %s\n", rep.GeneratedAt.Local().Format(time.RFC3339))
			})
		},
	}
}

// ---------- helpers -------------------------------------------------------

func handleNoDaemon(cmd *cobra.Command, opts *cliOpts, err error) error {
	if errors.Is(err, apiclient.ErrDaemonNotRunning) {
		st := proto.Status{State: proto.StateDisconnected}
		return emit(opts, cmd.OutOrStdout(), st, func(w io.Writer) {
			fmt.Fprintln(w, "daemon: not running")
		})
	}
	return err
}

func resolveServerID(ctx context.Context, c *apiclient.Client, ref string) (string, error) {
	servers, err := c.Servers(ctx, "")
	if err != nil {
		return "", err
	}
	for _, s := range servers {
		if s.ID == ref || strings.EqualFold(s.Name, ref) {
			return s.ID, nil
		}
	}
	for _, s := range servers {
		if strings.Contains(strings.ToLower(s.Name), strings.ToLower(ref)) {
			return s.ID, nil
		}
	}
	return "", fmt.Errorf("server %q not found", ref)
}

func parseBool(s string) bool {
	switch strings.ToLower(s) {
	case "true", "1", "yes", "on":
		return true
	}
	return false
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	if n < 1 {
		return ""
	}
	return s[:n-1] + "…"
}

func printStatus(w io.Writer, st proto.Status) {
	fmt.Fprintf(w, "state:    %s\n", st.State)
	if st.Server != nil {
		fmt.Fprintf(w, "server:   %s [%s] %s:%d\n", st.Server.Name, st.Server.Protocol, st.Server.Address, st.Server.Port)
	}
	if !st.Since.IsZero() {
		fmt.Fprintf(w, "since:    %s\n", st.Since.Local().Format(time.RFC3339))
	}
	if st.LatencyMS > 0 {
		fmt.Fprintf(w, "latency:  %d ms\n", st.LatencyMS)
	}
	if st.BytesIn > 0 || st.BytesOut > 0 {
		fmt.Fprintf(w, "traffic:  %s in / %s out\n", humanBytes(st.BytesIn), humanBytes(st.BytesOut))
	}
	fmt.Fprintf(w, "tunnel:   %s\n", st.TunnelMode)
	fmt.Fprintf(w, "kill-sw:  %v\n", st.KillSwitch)
	if st.LastError != "" {
		fmt.Fprintf(w, "error:    %s\n", st.LastError)
	}
}

func printPrefs(w io.Writer, p store.Prefs) {
	fmt.Fprintf(w, "tunnel:        %s\n", p.TunnelMode)
	fmt.Fprintf(w, "tun-stack:     %s\n", p.TunStack)
	fmt.Fprintf(w, "socks:         %s\n", p.SocksAddr)
	fmt.Fprintf(w, "http:          %s\n", p.HTTPAddr)
	fmt.Fprintf(w, "mtu:           %d\n", p.MTU)
	fmt.Fprintf(w, "kill-switch:   %v\n", p.KillSwitch)
	fmt.Fprintf(w, "allow-lan:     %v\n", p.AllowLAN)
	fmt.Fprintf(w, "block-ipv6:    %v\n", p.BlockIPv6)
	fmt.Fprintf(w, "share-lan:     %v (%s)\n", p.ShareLAN, p.ShareAddr)
	fmt.Fprintf(w, "auto-start:    %s\n", p.AutoStart)
	fmt.Fprintf(w, "auto-connect:  %v\n", p.AutoConnect)
	fmt.Fprintf(w, "mcp-enabled:   %v (%s)\n", p.MCPEnabled, p.MCPAddr)
	fmt.Fprintf(w, "mcp-permission: %s\n", p.MCPPermission)
}

func humanBytes(n uint64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := uint64(unit), 0
	for n2 := n / unit; n2 >= unit; n2 /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}
