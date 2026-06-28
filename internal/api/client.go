// Package api is the CLI's client to the daemon over the Unix control socket.
package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"time"
)

// Client talks to the daemon over a Unix socket.
type Client struct {
	socket string
	http   *http.Client
}

// NewClient builds a client for the given socket path.
func NewClient(socket string) *Client {
	return &Client{
		socket: socket,
		http: &http.Client{
			Timeout: 30 * time.Minute, // agent/eval runs can be long
			Transport: &http.Transport{
				DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
					var d net.Dialer
					return d.DialContext(ctx, "unix", socket)
				},
			},
		},
	}
}

func (c *Client) url(path string) string { return "http://faraday" + path }

// ErrDaemonDown indicates the daemon socket is unreachable.
type ErrDaemonDown struct{ Socket string }

func (e ErrDaemonDown) Error() string {
	return fmt.Sprintf("faraday daemon not reachable at %s — start it with: faraday daemon", e.Socket)
}

func (c *Client) do(ctx context.Context, method, path string, body any, out any) error {
	var rdr *bytes.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rdr = bytes.NewReader(b)
	} else {
		rdr = bytes.NewReader(nil)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.url(path), rdr)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return ErrDaemonDown{Socket: c.socket}
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		var e struct {
			Error string `json:"error"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&e)
		if e.Error == "" {
			e.Error = resp.Status
		}
		return fmt.Errorf("%s", e.Error)
	}
	if out != nil {
		return json.NewDecoder(resp.Body).Decode(out)
	}
	return nil
}

// Get performs a GET and decodes into out.
func (c *Client) Get(ctx context.Context, path string, out any) error {
	return c.do(ctx, http.MethodGet, path, nil, out)
}

// Post performs a POST and decodes into out.
func (c *Client) Post(ctx context.Context, path string, body, out any) error {
	return c.do(ctx, http.MethodPost, path, body, out)
}
