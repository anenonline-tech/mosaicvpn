
# Mosaic

A multi-protocol VPN client for Windows that an AI agent can drive end-to-end.

> **Status — Phase 2 of 4 (in progress).** Phase 1 (daemon + CLI + store +
> parsers + rule engine) is on `main`. This branch wires in **sing-box** as
> the real VPN backend (VLESS, Hysteria2, Shadowsocks, NaiveProxy). TUN
> auto-route, AmneziaWG outbound, kill-switch, and the MCP server are the
> remaining items inside Phase 2; they'll land as follow-up commits once
> the engine plumbing here is reviewed. Pass `-mock` to `mosaicd` to keep
> using the deterministic mock backend (useful in tests / non-admin envs).

## What's in the box

```
cmd/
  mosaicd/      → background daemon (HTTP API, state machine, store)
  mosaic/       → CLI client, identical capabilities to the GUI/MCP
internal/
  api/          → daemon HTTP API (bearer-token auth on loopback)
  apiclient/    → Go client used by the CLI and (later) MCP
  logx/         → structured slog wrapper
  paths/        → cross-platform data-directory resolver
  proto/        → API and storage types (single source of truth)
  rules/        → routing rule-engine: domain, IP-CIDR, GeoSite, GeoIP,
                  process, port, AND/OR logic
  single/       → single-instance enforcement (named mutex on Windows,
                  flock on Unix, lockfile carrying token + endpoint)
  state/        → connection state machine + Backend interface
                  (mock backend ships in Phase 1; sing-box in Phase 2)
  store/        → on-disk JSON store (atomic writes)
  subs/         → subscription parsers: sing-box, Clash YAML,
                  v2ray base64 (vless/vmess/ss/hy2/naive), SIP008
docs/mockups/   → Atlas-aesthetic UI mockups (cream paper, copper accent)
```

Roadmap:

| Phase | Scope |
|------:|-------|
| 1 (this) | Daemon + CLI + parsers + state machine + tests |
| 2 | Real sing-box engine, AmneziaWG, Wintun TUN, kill-switch, MCP |
| 3 | Tauri + React GUI implementing the 5 Atlas screens |
| 4 | MSI/MSIX installer, Windows service registration, code signing |

## Architecture

```
┌──────────────────────────────────────────────────────────────┐
│  mosaicd (Windows service / foreground)                      │
│  ┌─────────────┐  ┌─────────────┐  ┌──────────────────────┐  │
│  │ state.Mgr   │  │ store.Store │  │ subs.Parser          │  │
│  │ (sm + sub)  │  │ (atomic)    │  │ (4 formats)          │  │
│  └──────┬──────┘  └──────┬──────┘  └──────────────────────┘  │
│         │                │                                    │
│  ┌──────▼────────────────▼────────────────────────────────┐  │
│  │ api.Server   GET/POST /v1/* on 127.0.0.1:random        │  │
│  │              Bearer token written into lockfile         │  │
│  └────────────┬─────────────────────────────────────┬─────┘  │
│  ┌────────────▼───┐  ┌────────────┐  ┌──────────────▼─────┐  │
│  │ Backend iface  │  │ Wintun TUN │  │ MCP server         │  │
│  │ ──────────────│  │  (P2)      │  │  (P2)              │  │
│  │ MockBackend P1│  └────────────┘  └────────────────────┘  │
│  │ SingboxBack P2│                                            │
│  └───────────────┘                                            │
└────────────┬─────────────────────────────────────────────────┘
             │ HTTP + Bearer token
   ┌─────────┴──────────┐──────────────┬─────────────────┐
   │ Tauri GUI (P3)     │ mosaic CLI   │ Coding agent    │
   │ React + WebView2   │ (cobra)      │ via MCP / CLI   │
   └────────────────────┴──────────────┴─────────────────┘
```

### Single-instance enforcement

The daemon refuses to start a second copy via three layered mechanisms:

1. **Platform lock on the lockfile** — `flock` on Unix, `LockFileEx` on
   Windows. The OS releases the lock automatically on crash.
2. **Named mutex `Global\Mosaic.daemon`** on Windows for system-wide
   uniqueness across user sessions.
3. **Lockfile contents** include the listening host, port, bearer token,
   PID, and version. Clients (CLI, GUI, MCP) read this file to discover
   *the* running daemon — there can only be one.

If a second `mosaicd` is launched, it fails fast with the address of the
first instance:

```
$ mosaicd
mosaicd: another daemon is already running on 127.0.0.1:43479 (pid 29590)
```

### Subscription parsing

`internal/subs` accepts a raw payload and auto-detects the format:

| Format | Detection |
|---|---|
| `singbox` | JSON, contains `"outbounds"` |
| `clash` | YAML, contains `proxies:` |
| `v2ray-base64` | base64 or plain list of `vless://`, `vmess://`, `ss://`, `hysteria2://`, `naive+https://` |
| `sip008` | JSON, contains `"servers"` (Shadowsocks-only) |

All formats normalise into a single `proto.Server` shape with a stable,
deterministic `ID` (SHA-1 of subscription + protocol + address + port +
secret bits). That means re-fetching a subscription does not invalidate
existing rules that target a server by ID.

### State machine

`internal/state.Manager` owns the connection lifecycle:

```
disconnected ─▶ connecting ─▶ connected
                     │            │
                     ▼            ▼
                  error      disconnected
                     ▲            ▲
                     └────────────┘
```

The manager is the only thing allowed to mutate `Status`. API handlers,
the CLI, and the eventual MCP server all read snapshots and observe
transitions through `Subscribe()` (used by the SSE `/v1/events` stream).

### Routing rule engine

`internal/rules.Engine` evaluates a `Flow` against an ordered list of
`proto.Rule`s; the first rule whose `Match` clause fires wins. A `Match`
supports:

- `domain` (exact), `domain_suffix`, `domain_keyword`
- `ip_cidr` (IPv4 + IPv6)
- `process` (executable name)
- `port` (single value or `lo-hi` ranges)
- `geosite` (categories — needs a `GeoSiteResolver`)
- `geoip` (country codes — needs a `GeoIPResolver`)
- `logic`: `and` (default) or `or`

The resolvers are pluggable interfaces; Phase 1 leaves them `nil` (so
`geosite`/`geoip` conditions never fire), Phase 2 wires in real GeoSite
and MaxMind data files.

## Build

Requires Go 1.22+.

```bash
git clone https://github.com/pupspochta-cpu/mosaicvpn.git
cd mosaic
go build ./...
```

Cross-compile for Windows (also exercised in CI):

```bash
GOOS=windows GOARCH=amd64 go build -o bin/mosaicd.exe ./cmd/mosaicd
GOOS=windows GOARCH=amd64 go build -o bin/mosaic.exe   ./cmd/mosaic
```

## Run

Run the daemon (this terminal stays attached):

```bash
mosaicd
```

Talk to it from another terminal:

```bash
mosaic status
mosaic sub add  https://provider.example/sub.txt --name "My Provider"
mosaic servers
mosaic connect tokyo
mosaic disconnect
mosaic prefs show
mosaic prefs set --kill-switch=false --tunnel=proxy
mosaic diag --json
```

Override the data directory (handy for sandboxing or tests):

```bash
mosaicd  -data-dir /tmp/mosaic-data
mosaic   --data-dir /tmp/mosaic-data status
```

Default data locations:

| OS | Path |
|---|---|
| Windows | `%ProgramData%\Mosaic` |
| macOS | `~/Library/Application Support/Mosaic` |
| Linux | `$XDG_DATA_HOME/mosaic` or `~/.local/share/mosaic` |

## Test

```bash
go test ./...
go test -race ./...
```

Phase 1 unit tests cover:

- `internal/single` — acquire / release / second-instance rejection
- `internal/subs` — sing-box, Clash, v2ray-base64, SIP008 parsers, ID
  determinism, unknown-format handling
- `internal/store` — defaults, persistence round-trip, dedupe by URL,
  rule lifecycle
- `internal/rules` — domain suffix / keyword / exact, port ranges,
  IP-CIDR, AND/OR logic, GeoSite/GeoIP via fake resolvers, disabled rules
- `internal/state` — initial state, connect / disconnect, error path,
  subscriber events, last-server persistence
- `internal/api` — auth middleware, subscription lifecycle (add/refresh/
  delete), rule reorder, prefs round-trip, connect-via-CLI

End-to-end smoke (manual, but easily scripted):

```bash
mosaicd  -data-dir /tmp/mosaic-data &
echo 'vless://abc@1.2.3.4:443?security=reality#Tokyo' | base64 > /tmp/sub.txt
python3 -m http.server 8765 --directory /tmp &
mosaic --data-dir /tmp/mosaic-data sub add http://127.0.0.1:8765/sub.txt
mosaic --data-dir /tmp/mosaic-data connect Tokyo
mosaic --data-dir /tmp/mosaic-data status      # state: connected
mosaic --data-dir /tmp/mosaic-data disconnect
```

## Design system (mockups)

The visual direction lives at `docs/mockups/` (Atlas: cream paper, copper
accent, cartographic main view, no generic UI components). The Tauri GUI
in Phase 3 will implement these screens 1:1 against the API documented
above.

## Licence

TBD — will be set to MIT or Apache-2.0 before the first tagged release.
