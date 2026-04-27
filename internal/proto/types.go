// Package proto contains the shared data types used across the daemon, CLI,
// and API client. Types are JSON-serializable and represent the canonical
// state of the system as exposed by the daemon API.
package proto

import "time"

// Protocol identifies a VPN/proxy protocol supported by Mosaic.
type Protocol string

const (
	ProtoVLESS       Protocol = "vless"
	ProtoHysteria2   Protocol = "hysteria2"
	ProtoNaive       Protocol = "naive"
	ProtoShadowsocks Protocol = "shadowsocks"
	ProtoAmneziaWG   Protocol = "amneziawg"
)

// AllProtocols returns every protocol Mosaic understands.
func AllProtocols() []Protocol {
	return []Protocol{ProtoVLESS, ProtoHysteria2, ProtoNaive, ProtoShadowsocks, ProtoAmneziaWG}
}

// IsKnown reports whether p is a recognised protocol.
func (p Protocol) IsKnown() bool {
	for _, k := range AllProtocols() {
		if k == p {
			return true
		}
	}
	return false
}

// Server represents a single VPN/proxy endpoint that the daemon can connect
// to. Servers live inside Subscriptions.
type Server struct {
	ID             string         `json:"id"`
	Name           string         `json:"name"`
	Protocol       Protocol       `json:"protocol"`
	Address        string         `json:"address"`
	Port           int            `json:"port"`
	City           string         `json:"city,omitempty"`
	Country        string         `json:"country,omitempty"`
	Tag            string         `json:"tag,omitempty"`
	SubscriptionID string         `json:"subscription_id"`
	LastTestMS     int            `json:"last_test_ms,omitempty"`
	LastTestAt     time.Time      `json:"last_test_at,omitempty"`
	LastTestError  string         `json:"last_test_error,omitempty"`
	Raw            map[string]any `json:"raw,omitempty"`
}

// Format identifies a subscription payload format.
type Format string

const (
	FormatSingbox  Format = "singbox"
	FormatClash    Format = "clash"
	FormatV2RayB64 Format = "v2ray-base64"
	FormatSIP008   Format = "sip008"
	FormatUnknown  Format = "unknown"
)

// Subscription is a remote source of Servers, periodically fetched and
// re-parsed.
type Subscription struct {
	ID                     string    `json:"id"`
	Name                   string    `json:"name"`
	URL                    string    `json:"url"`
	Format                 Format    `json:"format"`
	LastFetched            time.Time `json:"last_fetched,omitempty"`
	LastError              string    `json:"last_error,omitempty"`
	AutoRefresh            bool      `json:"auto_refresh"`
	RefreshIntervalSeconds int       `json:"refresh_interval_seconds"`
	ServerCount            int       `json:"server_count"`
}

// Action is the verdict a routing rule produces when it matches a flow.
type Action string

const (
	ActionProxy  Action = "proxy"
	ActionDirect Action = "direct"
	ActionBlock  Action = "block"
)

// Logic combines multiple match conditions in a Rule.
type Logic string

const (
	LogicAnd Logic = "and"
	LogicOr  Logic = "or"
)

// Match describes the conditions that select traffic for a Rule.
type Match struct {
	Logic         Logic    `json:"logic"`
	GeoSite       []string `json:"geosite,omitempty"`
	GeoIP         []string `json:"geoip,omitempty"`
	DomainSuffix  []string `json:"domain_suffix,omitempty"`
	DomainKeyword []string `json:"domain_keyword,omitempty"`
	Domain        []string `json:"domain,omitempty"`
	IPCIDR        []string `json:"ip_cidr,omitempty"`
	Process       []string `json:"process,omitempty"`
	Port          []string `json:"port,omitempty"`
}

// Empty reports whether the match has no conditions configured.
func (m Match) Empty() bool {
	return len(m.GeoSite) == 0 && len(m.GeoIP) == 0 && len(m.DomainSuffix) == 0 &&
		len(m.DomainKeyword) == 0 && len(m.Domain) == 0 && len(m.IPCIDR) == 0 &&
		len(m.Process) == 0 && len(m.Port) == 0
}

// Rule is a routing rule entry. Rules are evaluated in priority order; the
// first match wins.
type Rule struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Priority int    `json:"priority"`
	Enabled  bool   `json:"enabled"`
	Action   Action `json:"action"`
	Target   string `json:"target,omitempty"`
	Match    Match  `json:"match"`
}

// State is the current daemon connection state.
type State string

const (
	StateDisconnected State = "disconnected"
	StateConnecting   State = "connecting"
	StateConnected    State = "connected"
	StateError        State = "error"
)

// Status is the aggregate runtime state exposed by the daemon API.
type Status struct {
	State          State     `json:"state"`
	Server         *Server   `json:"server,omitempty"`
	Since          time.Time `json:"since,omitempty"`
	LastError      string    `json:"last_error,omitempty"`
	LatencyMS      int       `json:"latency_ms,omitempty"`
	BytesIn        uint64    `json:"bytes_in"`
	BytesOut       uint64    `json:"bytes_out"`
	TunnelMode     string    `json:"tunnel_mode"`
	KillSwitch     bool      `json:"kill_switch"`
	AgentConnected bool      `json:"agent_connected"`
	DaemonVersion  string    `json:"daemon_version"`
	DaemonPID      int       `json:"daemon_pid"`
}

// ConnectRequest tells the daemon to bring up a server.
type ConnectRequest struct {
	ServerID string `json:"server_id"`
}

// AddSubscriptionRequest adds a new subscription.
type AddSubscriptionRequest struct {
	URL    string `json:"url"`
	Name   string `json:"name,omitempty"`
	Format Format `json:"format,omitempty"` // optional override; auto-detected if empty
}

// DiagReport is the structured output of `mosaic diag`.
type DiagReport struct {
	GeneratedAt   time.Time      `json:"generated_at"`
	DaemonVersion string         `json:"daemon_version"`
	OS            string         `json:"os"`
	Arch          string         `json:"arch"`
	Status        Status         `json:"status"`
	Subscriptions []Subscription `json:"subscriptions"`
	ServerCount   int            `json:"server_count"`
	RuleCount     int            `json:"rule_count"`
	RecentLogs    []string       `json:"recent_logs"`
	Notes         []string       `json:"notes,omitempty"`
}

// Daemon listen info, surfaced to clients via the lockfile.
type DaemonEndpoint struct {
	Host    string `json:"host"`
	Port    int    `json:"port"`
	Token   string `json:"token"`
	PID     int    `json:"pid"`
	Started string `json:"started"`
	Version string `json:"version"`
}
