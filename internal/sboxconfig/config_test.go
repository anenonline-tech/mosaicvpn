package sboxconfig_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/sagernet/sing-box/include"
	"github.com/sagernet/sing-box/option"

	"github.com/pupspochta-cpu/mosaicvpn/internal/proto"
	"github.com/pupspochta-cpu/mosaicvpn/internal/sboxconfig"
	"github.com/pupspochta-cpu/mosaicvpn/internal/store"
)

// TestBuild_AllProtocols asserts that the config builder produces JSON
// that sing-box can actually parse for every protocol Mosaic supports.
// This is the core invariant that lets Phase 2 ship without manual
// fuzzing each provider's outbound shape.
func TestBuild_AllProtocols(t *testing.T) {
	cases := []struct {
		name   string
		server proto.Server
	}{
		{
			name: "vless-reality",
			server: proto.Server{
				Name:     "VL-1",
				Protocol: proto.ProtoVLESS,
				Address:  "1.2.3.4",
				Port:     443,
				Raw: map[string]any{
					"uuid":        "00000000-0000-0000-0000-000000000001",
					"flow":        "xtls-rprx-vision",
					"security":    "reality",
					"sni":         "example.com",
					"public_key":  "abcdef",
					"short_id":    "01ab",
					"fingerprint": "chrome",
				},
			},
		},
		{
			name: "hysteria2",
			server: proto.Server{
				Name:     "HY2",
				Protocol: proto.ProtoHysteria2,
				Address:  "1.2.3.4",
				Port:     8443,
				Raw: map[string]any{
					"password": "hunter2",
					"sni":      "example.com",
				},
			},
		},
		{
			name: "shadowsocks-2022",
			server: proto.Server{
				Name:     "SS",
				Protocol: proto.ProtoShadowsocks,
				Address:  "1.2.3.4",
				Port:     8388,
				Raw: map[string]any{
					"method":   "2022-blake3-aes-128-gcm",
					"password": "MTIzNDU2Nzg5MDEyMzQ1Ng==",
				},
			},
		},
		{
			name: "naive",
			server: proto.Server{
				Name:     "NV",
				Protocol: proto.ProtoNaive,
				Address:  "1.2.3.4",
				Port:     443,
				Raw: map[string]any{
					"username": "user",
					"password": "pw",
					"sni":      "example.com",
				},
			},
		},
	}

	prefs := store.DefaultPrefs()
	prefs.TunnelMode = "proxy" // tun mode requires platform support; test the proxy path here.
	rules := []proto.Rule{
		{
			ID: "r1", Name: "ad-block", Enabled: true, Action: proto.ActionBlock,
			Match: proto.Match{DomainSuffix: []string{"doubleclick.net"}},
		},
		{
			ID: "r2", Name: "lan-direct", Enabled: true, Action: proto.ActionDirect,
			Match: proto.Match{IPCIDR: []string{"10.0.0.0/8"}},
		},
		{
			ID: "r3", Name: "disabled", Enabled: false, Action: proto.ActionDirect,
			Match: proto.Match{Domain: []string{"never.example"}},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := sboxconfig.Build(tc.server, prefs, rules)
			if err != nil {
				t.Fatalf("Build: %v", err)
			}
			raw, err := cfg.JSON()
			if err != nil {
				t.Fatalf("JSON: %v", err)
			}

			ctx := include.Context(context.Background())
			var opts option.Options
			if err := opts.UnmarshalJSONContext(ctx, raw); err != nil {
				t.Fatalf("sing-box rejected config: %v\n--- config ---\n%s", err, raw)
			}
		})
	}
}

func TestBuild_DisabledRulesNotEmitted(t *testing.T) {
	cfg, err := sboxconfig.Build(
		proto.Server{
			Name:     "S",
			Protocol: proto.ProtoVLESS,
			Address:  "1.2.3.4", Port: 443,
			Raw: map[string]any{"uuid": "00000000-0000-0000-0000-000000000001"},
		},
		store.DefaultPrefs(),
		[]proto.Rule{
			{ID: "off", Enabled: false, Action: proto.ActionBlock, Match: proto.Match{Domain: []string{"a"}}},
			{ID: "on", Enabled: true, Action: proto.ActionDirect, Match: proto.Match{Domain: []string{"b"}}},
		},
	)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	rules, _ := cfg.Route["rules"].([]map[string]any)
	for _, r := range rules {
		if domains, ok := r["domain"].([]string); ok {
			for _, d := range domains {
				if d == "a" {
					raw, _ := json.Marshal(r)
					t.Fatalf("disabled rule was emitted: %s", raw)
				}
			}
		}
	}
}
