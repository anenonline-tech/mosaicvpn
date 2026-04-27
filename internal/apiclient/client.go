// Package apiclient is a thin HTTP client for the Mosaic daemon API. It is
// shared by the CLI, the MCP server, and any other client that needs to
// talk to a running daemon.
package apiclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/pupspochta-cpu/mosaicvpn/internal/proto"
	"github.com/pupspochta-cpu/mosaicvpn/internal/single"
	"github.com/pupspochta-cpu/mosaicvpn/internal/store"
)

// ErrDaemonNotRunning is returned when the lockfile is absent or empty.
var ErrDaemonNotRunning = errors.New("mosaic daemon is not running")

// Client talks to a Mosaic daemon over its loopback API.
type Client struct {
	endpoint proto.DaemonEndpoint
	http     *http.Client
}

// FromLockfile reads daemon endpoint info from the lockfile.
func FromLockfile(path string) (*Client, error) {
	ep, err := single.ReadEndpoint(path)
	if err != nil {
		if errors.Is(err, io.EOF) {
			return nil, ErrDaemonNotRunning
		}
		return nil, ErrDaemonNotRunning
	}
	if ep.Port == 0 || ep.Token == "" {
		return nil, ErrDaemonNotRunning
	}
	if ep.Host == "" {
		ep.Host = "127.0.0.1"
	}
	return &Client{
		endpoint: ep,
		http:     &http.Client{Timeout: 30 * time.Second},
	}, nil
}

// New constructs a client against an explicit endpoint (mostly used in tests).
func New(host string, port int, token string) *Client {
	return &Client{
		endpoint: proto.DaemonEndpoint{Host: host, Port: port, Token: token},
		http:     &http.Client{Timeout: 30 * time.Second},
	}
}

// BaseURL returns the http://host:port base for the daemon.
func (c *Client) BaseURL() string {
	return fmt.Sprintf("http://%s:%d", c.endpoint.Host, c.endpoint.Port)
}

func (c *Client) do(ctx context.Context, method, path string, body, out any) error {
	var reqBody io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reqBody = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL()+path, reqBody)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.endpoint.Token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		var msg struct {
			Error string `json:"error"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&msg)
		if msg.Error == "" {
			msg.Error = resp.Status
		}
		return fmt.Errorf("daemon returned %s: %s", resp.Status, msg.Error)
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// Status returns the current daemon status.
func (c *Client) Status(ctx context.Context) (proto.Status, error) {
	var st proto.Status
	err := c.do(ctx, http.MethodGet, "/v1/status", nil, &st)
	return st, err
}

// Connect requests a connection to the named server.
func (c *Client) Connect(ctx context.Context, serverID string) (proto.Status, error) {
	var st proto.Status
	err := c.do(ctx, http.MethodPost, "/v1/connect", proto.ConnectRequest{ServerID: serverID}, &st)
	return st, err
}

// Disconnect requests the daemon to drop the active connection.
func (c *Client) Disconnect(ctx context.Context) (proto.Status, error) {
	var st proto.Status
	err := c.do(ctx, http.MethodPost, "/v1/disconnect", nil, &st)
	return st, err
}

// Subscriptions returns the configured subscriptions.
func (c *Client) Subscriptions(ctx context.Context) ([]proto.Subscription, error) {
	var out []proto.Subscription
	err := c.do(ctx, http.MethodGet, "/v1/subscriptions", nil, &out)
	return out, err
}

// AddSubscription adds and immediately fetches a new subscription.
func (c *Client) AddSubscription(ctx context.Context, req proto.AddSubscriptionRequest) (proto.Subscription, error) {
	var out proto.Subscription
	err := c.do(ctx, http.MethodPost, "/v1/subscriptions", req, &out)
	return out, err
}

// RefreshSubscription forces a fetch of an existing subscription.
func (c *Client) RefreshSubscription(ctx context.Context, id string) (proto.Subscription, error) {
	var out proto.Subscription
	err := c.do(ctx, http.MethodPost, "/v1/subscriptions/"+id+"/refresh", nil, &out)
	return out, err
}

// DeleteSubscription removes a subscription and its servers.
func (c *Client) DeleteSubscription(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, "/v1/subscriptions/"+id, nil, nil)
}

// Servers lists servers, optionally filtered by subscription id.
func (c *Client) Servers(ctx context.Context, subscriptionID string) ([]proto.Server, error) {
	path := "/v1/servers"
	if subscriptionID != "" {
		path += "?subscription_id=" + subscriptionID
	}
	var out []proto.Server
	err := c.do(ctx, http.MethodGet, path, nil, &out)
	return out, err
}

// TestResult is one row of a `mosaic test` / POST /v1/servers:test response.
type TestResult struct {
	ServerID string    `json:"server_id"`
	MS       int       `json:"ms"`
	Err      string    `json:"error,omitempty"`
	At       time.Time `json:"at"`
}

// TestServers triggers parallel TCP-handshake probes against the given
// server ids (or all stored servers when ids is empty) and returns the
// per-server outcomes.
func (c *Client) TestServers(ctx context.Context, ids []string, concurrency int) ([]TestResult, error) {
	body := struct {
		IDs         []string `json:"ids,omitempty"`
		Concurrency int      `json:"concurrency,omitempty"`
	}{IDs: ids, Concurrency: concurrency}
	var out []TestResult
	err := c.do(ctx, http.MethodPost, "/v1/servers:test", body, &out)
	return out, err
}

// Rules returns the routing rules in priority order.
func (c *Client) Rules(ctx context.Context) ([]proto.Rule, error) {
	var out []proto.Rule
	err := c.do(ctx, http.MethodGet, "/v1/rules", nil, &out)
	return out, err
}

// AddRule appends a new rule.
func (c *Client) AddRule(ctx context.Context, r proto.Rule) (proto.Rule, error) {
	var out proto.Rule
	err := c.do(ctx, http.MethodPost, "/v1/rules", r, &out)
	return out, err
}

// DeleteRule deletes a rule by id.
func (c *Client) DeleteRule(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, "/v1/rules/"+id, nil, nil)
}

// ReorderRules sets the priority order of rules.
func (c *Client) ReorderRules(ctx context.Context, ids []string) ([]proto.Rule, error) {
	var out []proto.Rule
	err := c.do(ctx, http.MethodPost, "/v1/rules:reorder", map[string][]string{"ids": ids}, &out)
	return out, err
}

// Prefs returns the daemon preferences.
func (c *Client) Prefs(ctx context.Context) (store.Prefs, error) {
	var p store.Prefs
	err := c.do(ctx, http.MethodGet, "/v1/prefs", nil, &p)
	return p, err
}

// SetPrefs updates the daemon preferences.
func (c *Client) SetPrefs(ctx context.Context, p store.Prefs) (store.Prefs, error) {
	var out store.Prefs
	err := c.do(ctx, http.MethodPut, "/v1/prefs", p, &out)
	return out, err
}

// Diag returns a diagnostic snapshot.
func (c *Client) Diag(ctx context.Context) (proto.DiagReport, error) {
	var out proto.DiagReport
	err := c.do(ctx, http.MethodGet, "/v1/diag", nil, &out)
	return out, err
}
