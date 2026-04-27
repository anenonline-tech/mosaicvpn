/**
 * Mirror of internal/proto/types.go. The renderer consumes these types
 * verbatim from the daemon's HTTP API responses; field names match the
 * JSON tags exactly.
 */

export type Protocol =
  | "vless"
  | "vmess"
  | "shadowsocks"
  | "hysteria2"
  | "naive"
  | "amneziawg"
  | "trojan";

export type Format =
  | "singbox"
  | "clash"
  | "v2ray-base64"
  | "sip008"
  | "unknown";

export type Logic = "and" | "or";

export type Action = "proxy" | "direct" | "block";

export type State =
  | "disconnected"
  | "connecting"
  | "connected"
  | "error";

export interface Server {
  id: string;
  name: string;
  protocol: Protocol;
  address: string;
  port: number;
  city?: string;
  country?: string;
  tag?: string;
  subscription_id: string;
  last_test_ms?: number;
  last_test_error?: string;
  last_tested?: string;
}

export interface Subscription {
  id: string;
  name: string;
  url: string;
  format: Format;
  last_fetched?: string;
  last_error?: string;
  auto_refresh: boolean;
  refresh_interval_seconds: number;
  server_count: number;
}

export interface Match {
  logic: Logic;
  geosite?: string[];
  geoip?: string[];
  domain_suffix?: string[];
  domain_keyword?: string[];
  domain?: string[];
  ip_cidr?: string[];
  process?: string[];
  port?: string[];
}

export interface Rule {
  id: string;
  name: string;
  priority: number;
  enabled: boolean;
  action: Action;
  target?: string;
  match: Match;
}

export interface Status {
  state: State;
  server?: Server;
  since?: string;
  last_error?: string;
  latency_ms?: number;
  bytes_in: number;
  bytes_out: number;
  tunnel_mode: string;
  kill_switch: boolean;
  agent_connected: boolean;
  daemon_version: string;
  daemon_pid: number;
}

export interface Prefs {
  tunnel_mode: string;
  tun_stack: string;
  kill_switch: boolean;
  allow_lan: boolean;
  dns_proxied: string;
  dns_direct: string;
  auto_connect: boolean;
  show_on_launch: boolean;
  mcp_enabled: boolean;
  mcp_addr: string;
  mcp_permission: "read" | "connect" | "full";
  mcp_confirm: boolean;
}

export interface DaemonEndpoint {
  host: string;
  port: number;
  token: string;
  pid: number;
  version: string;
  started: string;
}
