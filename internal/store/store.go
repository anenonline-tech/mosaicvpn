// Package store persists Mosaic configuration to disk and provides
// concurrency-safe accessors.
//
// The on-disk format is a single JSON file that holds the full set of
// subscriptions, the servers they produce, the routing rules, the
// preferences, and the ID of the last-connected server. Writes are
// atomic (write to *.tmp + rename).
package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/pupspochta-cpu/mosaicvpn/internal/proto"
)

// State is the persistent in-memory representation of all Mosaic data.
// It is the single source of truth for everything that survives a daemon
// restart.
type State struct {
	Subscriptions []proto.Subscription `json:"subscriptions"`
	Servers       []proto.Server       `json:"servers"`
	Rules         []proto.Rule         `json:"rules"`
	Prefs         Prefs                `json:"prefs"`
	LastServerID  string               `json:"last_server_id,omitempty"`
	Version       int                  `json:"version"`
}

// Prefs holds user-configurable behaviour of the daemon.
type Prefs struct {
	TunnelMode      string `json:"tunnel_mode"` // "tun" | "proxy"
	TunStack        string `json:"tun_stack"`   // "system" | "gvisor" | "mixed"
	SocksAddr       string `json:"socks_addr"`
	HTTPAddr        string `json:"http_addr"`
	MTU             int    `json:"mtu"`
	KillSwitch      bool   `json:"kill_switch"`
	AllowLAN        bool   `json:"allow_lan"`
	BypassProcesses []string `json:"bypass_processes,omitempty"`
	BlockIPv6       bool   `json:"block_ipv6"`
	DNSMode         string `json:"dns_mode"` // "fake-ip" | "real-ip"
	DNSProxied      string `json:"dns_proxied"`
	DNSDirect       string `json:"dns_direct"`
	ShareLAN        bool   `json:"share_lan"`
	ShareAddr       string `json:"share_addr"`
	ShareAllow      []string `json:"share_allow,omitempty"`
	AutoStart       string `json:"auto_start"` // "service" | "user" | "manual"
	AutoConnect     bool   `json:"auto_connect"`
	ShowOnLaunch    bool   `json:"show_on_launch"`
	MCPEnabled      bool   `json:"mcp_enabled"`
	MCPAddr         string `json:"mcp_addr"`
	MCPPermission   string `json:"mcp_permission"` // "read" | "connect" | "full"
	MCPConfirm      bool   `json:"mcp_confirm"`
}

// DefaultPrefs returns the prefs Mosaic ships with on a fresh install.
func DefaultPrefs() Prefs {
	return Prefs{
		TunnelMode:    "tun",
		// gvisor is the safer default: it works on every platform without
		// the strict-route system-stack requirements that bite users
		// running another VPN/firewall in parallel. Power users can opt
		// into "system" for native performance.
		TunStack:      "gvisor",
		SocksAddr:     "127.0.0.1:1080",
		HTTPAddr:      "127.0.0.1:1081",
		MTU:           1420,
		KillSwitch:    true,
		AllowLAN:      true,
		BlockIPv6:     false,
		DNSMode:       "fake-ip",
		DNSProxied:    "https://1.1.1.1/dns-query",
		DNSDirect:     "udp://77.88.8.8",
		ShareLAN:      false,
		ShareAddr:     "0.0.0.0:1080",
		AutoStart:     "service",
		AutoConnect:   true,
		ShowOnLaunch:  true,
		MCPEnabled:    true,
		MCPAddr:       "127.0.0.1:8731",
		MCPPermission: "connect",
		MCPConfirm:    true,
	}
}

// Default returns a State with sensible defaults.
func Default() State {
	return State{
		Prefs:   DefaultPrefs(),
		Version: 1,
	}
}

// Store is a thread-safe wrapper around State backed by a JSON file.
type Store struct {
	mu    sync.RWMutex
	state State
	path  string
}

// Open loads the store at path, creating a default state if the file does
// not exist.
func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("ensure dir: %w", err)
	}
	s := &Store{path: path, state: Default()}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		if err := s.persistLocked(); err != nil {
			return nil, err
		}
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, &s.state); err != nil {
		return nil, fmt.Errorf("decode store: %w", err)
	}
	// Backfill defaults for new fields. The whole-Prefs reset only kicks
	// in for very old/corrupted stores (SocksAddr is set in DefaultPrefs
	// and was always set on writes). Per-field migrations live below.
	if s.state.Prefs.SocksAddr == "" {
		s.state.Prefs = DefaultPrefs()
	}
	// TunStack was added after the Prefs struct shipped; an existing
	// config will deserialise it as "". Map "" to the current safe
	// default ("gvisor") explicitly so the runtime never has to guess.
	// This is the documented migration: gvisor is now the cross-platform
	// default; users who want strict-route system stack must opt in via
	// `mosaic prefs set --tun-stack system`.
	if s.state.Prefs.TunStack == "" {
		s.state.Prefs.TunStack = DefaultPrefs().TunStack
	}
	return s, nil
}

// Snapshot returns a deep-ish copy of the current state. Servers and rules
// are copied by value but Raw maps remain shared — callers must not mutate
// them.
func (s *Store) Snapshot() State {
	s.mu.RLock()
	defer s.mu.RUnlock()
	cp := s.state
	cp.Subscriptions = append([]proto.Subscription(nil), s.state.Subscriptions...)
	cp.Servers = append([]proto.Server(nil), s.state.Servers...)
	cp.Rules = append([]proto.Rule(nil), s.state.Rules...)
	return cp
}

// Update applies fn to a copy of the state under write lock and persists
// the result on success.
func (s *Store) Update(fn func(*State) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := fn(&s.state); err != nil {
		return err
	}
	return s.persistLocked()
}

func (s *Store) persistLocked() error {
	tmp := s.path + ".tmp"
	data, err := json.MarshalIndent(s.state, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

// AddOrUpdateSubscription stores a subscription, replacing any existing
// entry with the same URL. Returns the stored subscription.
func (s *Store) AddOrUpdateSubscription(sub proto.Subscription) (proto.Subscription, error) {
	var saved proto.Subscription
	err := s.Update(func(st *State) error {
		for i, existing := range st.Subscriptions {
			if existing.URL == sub.URL {
				if sub.ID == "" {
					sub.ID = existing.ID
				}
				st.Subscriptions[i] = sub
				saved = sub
				return nil
			}
		}
		if sub.ID == "" {
			sub.ID = fmt.Sprintf("sub-%d", time.Now().UnixNano())
		}
		st.Subscriptions = append(st.Subscriptions, sub)
		saved = sub
		return nil
	})
	return saved, err
}

// ReplaceServersFor replaces all stored servers belonging to subID with the
// supplied list. Each server's SubscriptionID is forced to subID.
func (s *Store) ReplaceServersFor(subID string, servers []proto.Server) error {
	return s.Update(func(st *State) error {
		// keep servers from other subscriptions
		filtered := st.Servers[:0]
		for _, sv := range st.Servers {
			if sv.SubscriptionID != subID {
				filtered = append(filtered, sv)
			}
		}
		st.Servers = filtered
		for _, sv := range servers {
			sv.SubscriptionID = subID
			st.Servers = append(st.Servers, sv)
		}
		// stable order: subscription, then name
		sort.SliceStable(st.Servers, func(i, j int) bool {
			a, b := st.Servers[i], st.Servers[j]
			if a.SubscriptionID != b.SubscriptionID {
				return a.SubscriptionID < b.SubscriptionID
			}
			return a.Name < b.Name
		})
		// update count on the subscription record
		for i := range st.Subscriptions {
			if st.Subscriptions[i].ID == subID {
				st.Subscriptions[i].ServerCount = countServers(st.Servers, subID)
				st.Subscriptions[i].LastFetched = time.Now().UTC()
				st.Subscriptions[i].LastError = ""
			}
		}
		return nil
	})
}

// MarkSubscriptionError records a fetch error against a subscription
// without changing its server list.
func (s *Store) MarkSubscriptionError(subID, errMsg string) error {
	return s.Update(func(st *State) error {
		for i := range st.Subscriptions {
			if st.Subscriptions[i].ID == subID {
				st.Subscriptions[i].LastError = errMsg
				st.Subscriptions[i].LastFetched = time.Now().UTC()
				return nil
			}
		}
		return fmt.Errorf("subscription %q not found", subID)
	})
}

func countServers(all []proto.Server, subID string) int {
	n := 0
	for _, s := range all {
		if s.SubscriptionID == subID {
			n++
		}
	}
	return n
}

// FindServer locates a server by id.
func (s *Store) FindServer(id string) (proto.Server, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, sv := range s.state.Servers {
		if sv.ID == id {
			return sv, true
		}
	}
	return proto.Server{}, false
}

// SetLastServer remembers which server was most recently chosen.
func (s *Store) SetLastServer(id string) error {
	return s.Update(func(st *State) error {
		st.LastServerID = id
		return nil
	})
}

// SetPrefs replaces preferences wholesale.
func (s *Store) SetPrefs(p Prefs) error {
	return s.Update(func(st *State) error {
		st.Prefs = p
		return nil
	})
}

// AddRule appends a new rule.
func (s *Store) AddRule(r proto.Rule) (proto.Rule, error) {
	var saved proto.Rule
	err := s.Update(func(st *State) error {
		if r.ID == "" {
			r.ID = fmt.Sprintf("rule-%d", time.Now().UnixNano())
		}
		if r.Priority == 0 {
			r.Priority = len(st.Rules) + 1
		}
		st.Rules = append(st.Rules, r)
		saved = r
		return nil
	})
	return saved, err
}

// ReplaceRules sets the full rule list (used for drag-reorder operations).
func (s *Store) ReplaceRules(rules []proto.Rule) error {
	return s.Update(func(st *State) error {
		st.Rules = rules
		return nil
	})
}

// DeleteRule removes a rule by id.
func (s *Store) DeleteRule(id string) error {
	return s.Update(func(st *State) error {
		out := st.Rules[:0]
		for _, r := range st.Rules {
			if r.ID != id {
				out = append(out, r)
			}
		}
		st.Rules = out
		return nil
	})
}

// DeleteSubscription removes a subscription and all its servers.
func (s *Store) DeleteSubscription(subID string) error {
	return s.Update(func(st *State) error {
		subs := st.Subscriptions[:0]
		for _, sub := range st.Subscriptions {
			if sub.ID != subID {
				subs = append(subs, sub)
			}
		}
		st.Subscriptions = subs

		servers := st.Servers[:0]
		for _, sv := range st.Servers {
			if sv.SubscriptionID != subID {
				servers = append(servers, sv)
			}
		}
		st.Servers = servers
		return nil
	})
}
