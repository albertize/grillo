//go:build linux

// SPDX-License-Identifier: Apache-2.0

package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"time"

	"grillo.local/grillo/internal/model"
)

// Client talks to the local API over the Unix socket.
type Client struct {
	httpClient   *http.Client
	streamClient *http.Client
	socketPath   string
}

// NewClient returns a client for socketPath.
func NewClient(socketPath string) *Client {
	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var dialer net.Dialer
			return dialer.DialContext(ctx, "unix", socketPath)
		},
	}
	return &Client{
		httpClient:   &http.Client{Transport: transport, Timeout: 30 * time.Second},
		streamClient: &http.Client{Transport: transport},
		socketPath:   socketPath,
	}
}

// Version returns the daemon version.
func (c *Client) Version(ctx context.Context) (VersionInfo, error) {
	var out VersionInfo
	return out, c.do(ctx, http.MethodGet, "/v1/version", nil, &out)
}

// Health checks the daemon.
func (c *Client) Health(ctx context.Context) (Health, error) {
	var out Health
	return out, c.do(ctx, http.MethodGet, "/v1/health", nil, &out)
}

// Apply submits an application and returns the operation ID.
func (c *Client) Apply(ctx context.Context, app model.Application) (string, error) {
	var out struct {
		OperationID string `json:"operationId"`
	}
	err := c.do(ctx, http.MethodPost, "/v1/applications", app, &out)
	return out.OperationID, err
}

// Down stops an application and returns the operation ID.
func (c *Client) Down(ctx context.Context, application string, removeVolumes bool) (string, error) {
	var out struct {
		OperationID string `json:"operationId"`
	}
	path := "/v1/applications/" + url.PathEscape(application) + "/down"
	if removeVolumes {
		path += "?volumes=true"
	}
	err := c.do(ctx, http.MethodPost, path, nil, &out)
	return out.OperationID, err
}

// Operation returns an operation's status.
func (c *Client) Operation(ctx context.Context, id string) (Operation, error) {
	var out Operation
	return out, c.do(ctx, http.MethodGet, "/v1/operations/"+url.PathEscape(id), nil, &out)
}

// Cancel cancels an operation.
func (c *Client) Cancel(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodPost, "/v1/operations/"+url.PathEscape(id)+"/cancel", nil, nil)
}

// Shutdown asks the daemon to stop. It does not stop applications.
func (c *Client) Shutdown(ctx context.Context) error {
	return c.do(ctx, http.MethodPost, "/v1/shutdown", nil, nil)
}

// Events opens the event stream from since.
func (c *Client) Events(ctx context.Context, since uint64) (io.ReadCloser, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://unix/v1/events?since="+fmt.Sprint(since), nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.streamClient.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		return nil, decodeError(resp)
	}
	return resp.Body, nil
}

func (c *Client) do(ctx context.Context, method, path string, body, out any) error {
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, "http://unix"+path, reader)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return decodeError(resp)
	}
	if out != nil {
		return json.NewDecoder(resp.Body).Decode(out)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	return nil
}

func decodeError(resp *http.Response) error {
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	var apiErr Error
	if json.Unmarshal(data, &apiErr) == nil && apiErr.Code != "" {
		return &apiErr
	}
	return fmt.Errorf("api: %s: %s", resp.Status, string(data))
}

// Error implements the error interface.
func (e *Error) Error() string {
	if e.Message == "" {
		return e.Code
	}
	return e.Code + ": " + e.Message
}

// EnsureDaemon returns once a daemon is reachable on socketPath, starting
// daemonPath if needed. It waits for a bounded health handshake. Starting the
// daemon is detached so it outlives the CLI.
func EnsureDaemon(ctx context.Context, socketPath, daemonPath string, timeout time.Duration) error {
	client := NewClient(socketPath)
	if _, err := client.Health(ctx); err == nil {
		return nil
	}
	if daemonPath == "" {
		return fmt.Errorf("api: no daemon at %s", socketPath)
	}
	logPath := socketPath + ".log"
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer logFile.Close()
	cmd := exec.Command(daemonPath)
	cmd.Stdout, cmd.Stderr = logFile, logFile
	if err := cmd.Start(); err != nil {
		return err
	}
	// Detach: the daemon must survive the CLI, so do not wait or signal it.
	_ = cmd.Process.Release()

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if _, err := client.Health(ctx); err == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
	return fmt.Errorf("api: daemon did not become healthy within %s", timeout)
}
