//go:build linux

// SPDX-License-Identifier: Apache-2.0

package api

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/albertize/grillo/internal/build"
	"github.com/albertize/grillo/internal/image"
	"github.com/albertize/grillo/internal/model"
	"github.com/albertize/grillo/internal/observe"
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

// Images lists the image inventory.
func (c *Client) Images(ctx context.Context) ([]image.Record, error) {
	var out struct {
		Images []image.Record `json:"images"`
	}
	err := c.do(ctx, http.MethodGet, "/v1/images", nil, &out)
	return out.Images, err
}

// InspectImage returns one image record.
func (c *Client) InspectImage(ctx context.Context, reference string) (image.Record, error) {
	var out image.Record
	path := "/v1/images/inspect?ref=" + url.QueryEscape(reference)
	return out, c.do(ctx, http.MethodGet, path, nil, &out)
}

// PruneImages garbage collects unused images and returns the operation ID.
func (c *Client) PruneImages(ctx context.Context, keep []string) (string, error) {
	var out struct {
		OperationID string `json:"operationId"`
	}
	err := c.do(ctx, http.MethodPost, "/v1/images/prune", map[string]any{"keep": keep}, &out)
	return out.OperationID, err
}

// PinImage protects or releases an image from pruning.
func (c *Client) PinImage(ctx context.Context, reference string, pinned bool) error {
	return c.do(ctx, http.MethodPost, "/v1/images/pin", map[string]any{"reference": reference, "pinned": pinned}, nil)
}

// Build starts an image build and returns the operation ID.
func (c *Client) Build(ctx context.Context, request build.Request) (string, error) {
	var out struct {
		OperationID string `json:"operationId"`
	}
	err := c.do(ctx, http.MethodPost, "/v1/build", request, &out)
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

// ListLogs returns log records without following.
func (c *Client) ListLogs(ctx context.Context, since uint64, resource, container string) ([]observe.LogRecord, error) {
	query := fmt.Sprintf("/v1/logs?since=%d", since)
	if resource != "" {
		query += "&resource=" + url.QueryEscape(resource)
	}
	if container != "" {
		query += "&container=" + url.QueryEscape(container)
	}
	var out struct {
		Records []observe.LogRecord `json:"records"`
	}
	if err := c.do(ctx, http.MethodGet, query, nil, &out); err != nil {
		return nil, err
	}
	return out.Records, nil
}

// FollowLogs streams log records.
func (c *Client) FollowLogs(ctx context.Context, since uint64, resource, container string) (io.ReadCloser, error) {
	query := fmt.Sprintf("/v1/logs?follow=true&since=%d", since)
	if resource != "" {
		query += "&resource=" + url.QueryEscape(resource)
	}
	if container != "" {
		query += "&container=" + url.QueryEscape(container)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://unix"+query, nil)
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

// Applications lists the applications the daemon knows.
func (c *Client) Applications(ctx context.Context) ([]string, error) {
	var out struct {
		Applications []string `json:"applications"`
	}
	if err := c.do(ctx, http.MethodGet, "/v1/applications", nil, &out); err != nil {
		return nil, err
	}
	return out.Applications, nil
}

// Restart restarts every running container of an application.
func (c *Client) Restart(ctx context.Context, application string) (string, error) {
	var out struct {
		OperationID string `json:"operationId"`
	}
	err := c.do(ctx, http.MethodPost, "/v1/applications/"+url.PathEscape(application)+"/restart", nil, &out)
	return out.OperationID, err
}

// Status returns an application's container states.
func (c *Client) Status(ctx context.Context, application string) ([]ContainerStatus, error) {
	var out struct {
		Containers []ContainerStatus `json:"containers"`
	}
	err := c.do(ctx, http.MethodGet, "/v1/applications/"+url.PathEscape(application), nil, &out)
	return out.Containers, err
}

// View returns an allowlisted resource and metric snapshot.
func (c *Client) View(ctx context.Context, application string) (observe.ApplicationView, error) {
	var out observe.ApplicationView
	err := c.do(ctx, http.MethodGet, "/v1/applications/"+url.PathEscape(application)+"/view", nil, &out)
	return out, err
}

// Routes returns the daemon's actual published loopback fallback endpoints.
func (c *Client) Routes(ctx context.Context, application string) ([]RouteStatus, error) {
	var out struct {
		Routes []RouteStatus `json:"routes"`
	}
	err := c.do(ctx, http.MethodGet, "/v1/applications/"+url.PathEscape(application), nil, &out)
	return out.Routes, err
}

// Exec runs a command in a container and returns captured output.
func (c *Client) Exec(ctx context.Context, application, container string, args []string) (int, string, string, error) {
	request := map[string]any{"application": application, "container": container, "args": args}
	var out struct {
		ExitCode int    `json:"exitCode"`
		Stdout   string `json:"stdout"`
		Stderr   string `json:"stderr"`
	}
	if err := c.do(ctx, http.MethodPost, "/v1/exec", request, &out); err != nil {
		return 0, "", "", err
	}
	return out.ExitCode, out.Stdout, out.Stderr, nil
}

// ExecStream runs a command and streams stdout/stderr to the writers, returning
// the exit code. It uses the documented SSE-framed exec stream.
func (c *Client) ExecStream(ctx context.Context, application, container string, args []string, stdout, stderr io.Writer) (int, error) {
	body, err := json.Marshal(map[string]any{"application": application, "container": container, "args": args})
	if err != nil {
		return 0, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://unix/v1/exec?stream=true", bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.streamClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, decodeError(resp)
	}
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)
	event := ""
	code := 0
	gotExit := false
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case strings.HasPrefix(line, "event: "):
			event = strings.TrimPrefix(line, "event: ")
		case strings.HasPrefix(line, "data: "):
			data := strings.TrimPrefix(line, "data: ")
			switch event {
			case "stdout", "stderr":
				raw, err := base64.StdEncoding.DecodeString(data)
				if err != nil {
					return code, fmt.Errorf("api: malformed exec output")
				}
				writer := stdout
				if event == "stderr" {
					writer = stderr
				}
				if writer != nil {
					if _, err := writer.Write(raw); err != nil {
						return code, err
					}
				}
			case "exit":
				if _, err := fmt.Sscanf(data, "%d", &code); err != nil {
					return 0, fmt.Errorf("api: malformed exec exit")
				}
				gotExit = true
			case "error":
				raw, _ := base64.StdEncoding.DecodeString(data)
				return code, errors.New(string(raw))
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return code, err
	}
	if !gotExit {
		return code, fmt.Errorf("api: exec stream ended without an exit result")
	}
	return code, nil
}
