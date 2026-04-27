// Package sboxconfig translates Mosaic's domain types (proto.Server,
// store.Prefs, proto.Rule) into a sing-box JSON configuration.
//
// We deliberately go through the JSON wire format rather than the typed
// option structs in sing-box: it keeps Mosaic's surface decoupled from
// upstream's frequently-changing internals and lets us round-trip
// configurations to disk for debugging.
package sboxconfig

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/pupspochta-cpu/mosaicvpn/internal/proto"
	"github.com/pupspochta-cpu/mosaicvpn/internal/store"
)

// Config is what we serialise; sing-box parses an equivalent shape.
type Config struct {
	Log          map[string]any   `json:"log,omitempty"`
	DNS          map[string]any   `json:"dns,omitempty"`
	Inbounds     []map[string]any `json:"inbounds,omitempty"`
	Outbounds    []map[string]any `json:"outbounds,omitempty"`
	Route        map[string]any   `json:"route,omitempty"`
	Experimental map[string]any   `json:"experimental,omitempty"`
}

// Build composes a complete sing-box config for the given server and prefs.
// Rules are translated into the route.rules array.
func Build(server proto.Server, prefs store.Prefs, rules []proto.Rule) (*Config, error) {
	out, err := buildOutbound(server)
	if err != nil {
		return nil, err
	}
	cfg := &Config{
		Log: map[string]any{
			"level":     "info",
			"timestamp": true,
		},
		DNS:       buildDNS(prefs),
		Inbounds:  buildInbounds(prefs),
		Outbounds: append(buildBaseOutbounds(), out),
		Route:     buildRoute(prefs, rules, server),
	}
	return cfg, nil
}

// JSON serialises the config with indentation.
func (c *Config) JSON() ([]byte, error) {
	return json.MarshalIndent(c, "", "  ")
}

func buildBaseOutbounds() []map[string]any {
	return []map[string]any{
		{"type": "direct", "tag": "direct"},
	}
}

func buildOutbound(s proto.Server) (map[string]any, error) {
	switch s.Protocol {
	case proto.ProtoVLESS:
		return buildVLESS(s)
	case proto.ProtoHysteria2:
		return buildHysteria2(s)
	case proto.ProtoShadowsocks:
		return buildShadowsocks(s)
	case proto.ProtoNaive:
		return buildNaive(s)
	case proto.ProtoAmneziaWG:
		// AmneziaWG is handled by a separate process in Phase 2; emit a
		// direct outbound here so the config still validates and the rest
		// of the daemon keeps working until that lands.
		return map[string]any{"type": "direct", "tag": "proxy"}, nil
	default:
		return nil, fmt.Errorf("unsupported protocol %q", s.Protocol)
	}
}

func buildVLESS(s proto.Server) (map[string]any, error) {
	uuid := stringField(s.Raw, "uuid")
	if uuid == "" {
		return nil, fmt.Errorf("vless: missing uuid for %s", s.Name)
	}
	out := map[string]any{
		"type":        "vless",
		"tag":         "proxy",
		"server":      s.Address,
		"server_port": s.Port,
		"uuid":        uuid,
	}
	if flow := stringField(s.Raw, "flow"); flow != "" {
		out["flow"] = flow
	}
	tls := buildVLESSTLS(s)
	if tls != nil {
		out["tls"] = tls
	}
	if tr := buildTransport(s); tr != nil {
		out["transport"] = tr
	}
	return out, nil
}

func buildVLESSTLS(s proto.Server) map[string]any {
	security := stringField(s.Raw, "security")
	if security == "" && stringField(s.Raw, "sni") == "" {
		return nil
	}
	tls := map[string]any{
		"enabled": true,
	}
	if sni := stringField(s.Raw, "sni"); sni != "" {
		tls["server_name"] = sni
	}
	if fp := stringField(s.Raw, "fingerprint"); fp != "" {
		tls["utls"] = map[string]any{"enabled": true, "fingerprint": fp}
	} else {
		tls["utls"] = map[string]any{"enabled": true, "fingerprint": "chrome"}
	}
	if security == "reality" {
		reality := map[string]any{"enabled": true}
		if pk := stringField(s.Raw, "public_key"); pk != "" {
			reality["public_key"] = pk
		}
		if sid := stringField(s.Raw, "short_id"); sid != "" {
			reality["short_id"] = sid
		}
		tls["reality"] = reality
	}
	return tls
}

func buildTransport(s proto.Server) map[string]any {
	network := stringField(s.Raw, "network")
	switch strings.ToLower(network) {
	case "ws":
		t := map[string]any{"type": "ws"}
		if p := stringField(s.Raw, "path"); p != "" {
			t["path"] = p
		}
		if h := stringField(s.Raw, "host"); h != "" {
			t["headers"] = map[string]string{"Host": h}
		}
		return t
	case "grpc":
		return map[string]any{"type": "grpc", "service_name": stringField(s.Raw, "path")}
	case "h2", "http":
		return map[string]any{"type": "http", "path": stringField(s.Raw, "path")}
	}
	return nil
}

func buildHysteria2(s proto.Server) (map[string]any, error) {
	pw := stringField(s.Raw, "password")
	if pw == "" {
		return nil, fmt.Errorf("hysteria2: missing password for %s", s.Name)
	}
	out := map[string]any{
		"type":        "hysteria2",
		"tag":         "proxy",
		"server":      s.Address,
		"server_port": s.Port,
		"password":    pw,
		"tls": map[string]any{
			"enabled":     true,
			"server_name": firstNonEmpty(stringField(s.Raw, "sni"), s.Address),
			"insecure":    boolField(s.Raw, "insecure"),
		},
	}
	if obfs := stringField(s.Raw, "obfs"); obfs != "" {
		out["obfs"] = map[string]any{
			"type":     obfs,
			"password": stringField(s.Raw, "obfs_password"),
		}
	}
	return out, nil
}

func buildShadowsocks(s proto.Server) (map[string]any, error) {
	method := stringField(s.Raw, "method")
	pw := stringField(s.Raw, "password")
	if method == "" || pw == "" {
		return nil, fmt.Errorf("shadowsocks: missing method/password for %s", s.Name)
	}
	out := map[string]any{
		"type":        "shadowsocks",
		"tag":         "proxy",
		"server":      s.Address,
		"server_port": s.Port,
		"method":      method,
		"password":    pw,
	}
	if plugin := stringField(s.Raw, "plugin"); plugin != "" {
		out["plugin"] = plugin
		out["plugin_opts"] = stringField(s.Raw, "plugin_opts")
	}
	return out, nil
}

func buildNaive(s proto.Server) (map[string]any, error) {
	user := stringField(s.Raw, "username")
	pw := stringField(s.Raw, "password")
	if user == "" || pw == "" {
		return nil, fmt.Errorf("naive: missing credentials for %s", s.Name)
	}
	out := map[string]any{
		"type":        "naive",
		"tag":         "proxy",
		"server":      s.Address,
		"server_port": s.Port,
		"username":    user,
		"password":    pw,
		"tls": map[string]any{
			"enabled":     true,
			"server_name": firstNonEmpty(stringField(s.Raw, "sni"), s.Address),
		},
	}
	return out, nil
}

func buildDNS(prefs store.Prefs) map[string]any {
	proxied := firstNonEmpty(prefs.DNSProxied, "https://1.1.1.1/dns-query")
	direct := firstNonEmpty(prefs.DNSDirect, "udp://77.88.8.8")
	out := map[string]any{
		"servers": []map[string]any{
			{"tag": "proxy-dns", "address": proxied, "detour": "proxy"},
			{"tag": "direct-dns", "address": direct, "detour": "direct"},
		},
		"final":    "proxy-dns",
		"strategy": "ipv4_only",
	}
	if prefs.DNSMode == "fake-ip" {
		out["fakeip"] = map[string]any{
			"enabled":     true,
			"inet4_range": "198.18.0.0/15",
		}
		out["independent_cache"] = true
	}
	return out
}

func buildInbounds(prefs store.Prefs) []map[string]any {
	switch strings.ToLower(prefs.TunnelMode) {
	case "tun":
		return []map[string]any{tunInbound(prefs)}
	default: // "proxy"
		return proxyInbounds(prefs)
	}
}

func tunInbound(prefs store.Prefs) map[string]any {
	in := map[string]any{
		"type":           "tun",
		"tag":            "tun-in",
		"interface_name": "Mosaic",
		"address":        []string{"172.18.0.1/30"},
		"mtu":            firstNonZero(prefs.MTU, 1420),
		"auto_route":     true,
		"strict_route":   true,
		"stack":          "system",
	}
	if prefs.BlockIPv6 {
		in["address"] = []string{"172.18.0.1/30"}
	} else {
		in["address"] = []string{"172.18.0.1/30", "fdfe:dcba:9876::1/126"}
	}
	return in
}

func proxyInbounds(prefs store.Prefs) []map[string]any {
	socksAddr := firstNonEmpty(prefs.SocksAddr, "127.0.0.1:1080")
	httpAddr := firstNonEmpty(prefs.HTTPAddr, "127.0.0.1:1081")

	socksHost, socksPort := splitHostPort(socksAddr)
	httpHost, httpPort := splitHostPort(httpAddr)

	if prefs.ShareLAN {
		shareHost, _ := splitHostPort(firstNonEmpty(prefs.ShareAddr, "0.0.0.0:1080"))
		socksHost = shareHost
		httpHost = shareHost
	}

	return []map[string]any{
		{
			"type":   "mixed",
			"tag":    "mixed-in",
			"listen": socksHost, "listen_port": socksPort,
		},
		{
			"type":   "http",
			"tag":    "http-in",
			"listen": httpHost, "listen_port": httpPort,
		},
	}
}

func buildRoute(prefs store.Prefs, rules []proto.Rule, server proto.Server) map[string]any {
	r := map[string]any{
		"final":                 "proxy",
		"auto_detect_interface": true,
	}
	rs := make([]map[string]any, 0, len(rules)+3)
	// Sniff inbound traffic (replaces the deprecated inbound `sniff` field).
	rs = append(rs, map[string]any{"action": "sniff"})
	// Resolve DNS via the rule-action engine (the legacy `dns` outbound was
	// removed in sing-box 1.13).
	rs = append(rs, map[string]any{"protocol": "dns", "action": "hijack-dns"})
	if prefs.AllowLAN {
		rs = append(rs, map[string]any{"ip_is_private": true, "outbound": "direct"})
	}
	for _, rule := range rules {
		if !rule.Enabled {
			continue
		}
		rs = append(rs, translateRule(rule))
	}
	if len(rs) > 0 {
		r["rules"] = rs
	}
	return r
}

func translateRule(r proto.Rule) map[string]any {
	out := map[string]any{}
	if len(r.Match.Domain) > 0 {
		out["domain"] = r.Match.Domain
	}
	if len(r.Match.DomainSuffix) > 0 {
		out["domain_suffix"] = r.Match.DomainSuffix
	}
	if len(r.Match.DomainKeyword) > 0 {
		out["domain_keyword"] = r.Match.DomainKeyword
	}
	if len(r.Match.IPCIDR) > 0 {
		out["ip_cidr"] = r.Match.IPCIDR
	}
	if len(r.Match.Port) > 0 {
		out["port"] = r.Match.Port
	}
	if len(r.Match.Process) > 0 {
		out["process_name"] = r.Match.Process
	}
	if len(r.Match.GeoSite) > 0 {
		out["rule_set"] = prefixAll("geosite-", r.Match.GeoSite)
	}
	if len(r.Match.GeoIP) > 0 {
		// Reuse rule_set; sing-box also supports "geoip" but it's deprecated.
		merged, _ := out["rule_set"].([]string)
		out["rule_set"] = append(merged, prefixAll("geoip-", r.Match.GeoIP)...)
	}
	switch r.Action {
	case proto.ActionDirect:
		out["outbound"] = "direct"
	case proto.ActionBlock:
		// sing-box 1.13 removed the `block` outbound; use the reject rule action.
		out["action"] = "reject"
	case proto.ActionProxy:
		out["outbound"] = "proxy"
	default:
		out["outbound"] = "proxy"
	}
	return out
}

// ---------- helpers ------------------------------------------------------

func stringField(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	v, ok := m[key]
	if !ok {
		return ""
	}
	switch x := v.(type) {
	case string:
		return x
	case fmt.Stringer:
		return x.String()
	default:
		return fmt.Sprint(v)
	}
}

func boolField(m map[string]any, key string) bool {
	if m == nil {
		return false
	}
	switch v := m[key].(type) {
	case bool:
		return v
	case string:
		return v == "true" || v == "1"
	}
	return false
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

func firstNonZero(values ...int) int {
	for _, v := range values {
		if v != 0 {
			return v
		}
	}
	return 0
}

func splitHostPort(addr string) (string, int) {
	i := strings.LastIndex(addr, ":")
	if i < 0 {
		return addr, 0
	}
	host := addr[:i]
	port := 0
	for _, c := range addr[i+1:] {
		if c < '0' || c > '9' {
			return host, 0
		}
		port = port*10 + int(c-'0')
	}
	return host, port
}

func prefixAll(prefix string, in []string) []string {
	out := make([]string, len(in))
	for i, v := range in {
		out[i] = prefix + strings.ToLower(v)
	}
	return out
}
