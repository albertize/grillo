//go:build linux

// SPDX-License-Identifier: Apache-2.0

package executor

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"grillo.local/grillo/internal/guestproto"
	"grillo.local/grillo/internal/model"
	"grillo.local/grillo/internal/netns"
	"grillo.local/grillo/internal/network"
	"grillo.local/grillo/internal/plan"
	"grillo.local/grillo/internal/sandbox"
)

// Application network defaults. Applications are isolated in separate pasta
// namespaces, so the same subnet is reused safely.
const (
	bridgeName    = "grillo0"
	bridgeGateway = "10.77.0.1"
	bridgePrefix  = 24
	bridgeSubnet  = "10.77.0.0/24"
	bridgeMask    = "255.255.255.0"
)

// LaunchSandbox starts a VMM inside the application's network namespace.
func (e *Executor) LaunchSandbox(ctx context.Context, spec sandbox.Spec, args []string, logPath string) (sandbox.VMM, error) {
	if _, err := e.ensureSupervisor(ctx, spec.Application); err != nil {
		return nil, err
	}
	client := &netns.Client{SocketPath: e.socketPath(spec.Application)}
	return client.Launch(tapName(spec.ID), e.cfg.QEMU, args, logPath, tapMAC(spec.ID))
}

func (e *Executor) ensureSupervisor(ctx context.Context, application string) (*netns.Helper, error) {
	e.launchMu.Lock()
	defer e.launchMu.Unlock()
	if helper, ok := e.supervisors[application]; ok && helper.Alive() {
		return helper, nil
	}
	socket := e.socketPath(application)
	// Reuse a surviving supervisor rather than orphaning its namespace.
	if err := (&netns.Client{SocketPath: socket}).Ping(); err == nil {
		return nil, nil
	}
	var logFile *os.File
	if e.cfg.RuntimeDir != "" {
		logFile, _ = os.OpenFile(filepath.Join(e.cfg.RuntimeDir, "netns-"+sanitizeName(application)+".log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
		if logFile != nil {
			defer logFile.Close()
		}
	}
	helper, err := netns.StartSupervisor(ctx,
		netns.Config{SocketPath: socket, Bridge: bridgeName, Gateway: bridgeGateway, Prefix: bridgePrefix, Uplink: "uplink"},
		netns.SupervisorCommand{
			Pasta: e.cfg.Pasta,
			Bin:   e.cfg.NetnsBinary,
			Args: []string{
				"--socket", socket, "--bridge", bridgeName, "--gateway", bridgeGateway,
				"--prefix", fmt.Sprint(bridgePrefix), "--uplink", "uplink",
			},
		},
		logFile, 10*time.Second)
	if err != nil {
		return nil, err
	}
	e.supervisors[application] = helper
	return helper, nil
}

func (e *Executor) socketPath(application string) string {
	return filepath.Join(e.cfg.RuntimeDir, "netns-"+sanitizeName(application)+".sock")
}

func (e *Executor) allocateIP(application, sandboxID string) (string, error) {
	e.launchMu.Lock()
	defer e.launchMu.Unlock()
	ipam := e.ipams[application]
	if ipam == nil {
		opened, err := network.OpenIPAM(filepath.Join(e.cfg.RuntimeDir, "ipam-"+sanitizeName(application)), bridgeSubnet, bridgeGateway)
		if err != nil {
			return "", err
		}
		e.ipams[application] = opened
		ipam = opened
	}
	lease, err := ipam.Allocate(sandboxID, application, "", "")
	if err != nil {
		return "", err
	}
	return lease.IP, nil
}

func (e *Executor) releaseIP(application, sandboxID string) {
	e.launchMu.Lock()
	defer e.launchMu.Unlock()
	if ipam := e.ipams[application]; ipam != nil {
		_ = ipam.Release(sandboxID)
	}
}

// SandboxInfo identifies a running sandbox and its address.
type SandboxInfo struct {
	Key string
	IP  string
	CID uint32
}

// Sandboxes returns the running sandboxes of an application with their addresses.
func (e *Executor) Sandboxes(application string) []SandboxInfo {
	e.mu.Lock()
	defer e.mu.Unlock()
	var out []SandboxInfo
	for key, rt := range e.runtimes {
		if rt.app == application {
			out = append(out, SandboxInfo{Key: key, IP: rt.ip, CID: rt.cid})
		}
	}
	return out
}

// ExecRuntime runs a command in a container of a specific sandbox.
func (e *Executor) ExecRuntime(ctx context.Context, key, container string, args []string, stdout, stderr io.Writer) (int, error) {
	e.mu.Lock()
	rt := e.runtimes[key]
	e.mu.Unlock()
	if rt == nil || rt.guest == nil {
		return 0, fmt.Errorf("executor: no runtime %q", key)
	}
	result, err := rt.guest.Exec(ctx, guestproto.ExecRequest{Container: container, Args: args}, stdout, stderr)
	if err != nil {
		return 0, err
	}
	return result.ExitCode, nil
}

// Close stops the application network supervisors.
func (e *Executor) Close() {
	e.launchMu.Lock()
	helpers := make([]*netns.Helper, 0, len(e.supervisors))
	for _, helper := range e.supervisors {
		helpers = append(helpers, helper)
	}
	e.supervisors = map[string]*netns.Helper{}
	e.launchMu.Unlock()
	for _, helper := range helpers {
		_ = helper.Stop(2 * time.Second)
	}
}

// serviceIPs returns the addresses of every desired sandbox whose workload
// matches the service selector.
func (e *Executor) serviceIPs(app model.Application, selector map[string]string) []string {
	descriptors, err := plan.DesiredSandboxes(app)
	if err != nil {
		return nil
	}
	var ips []string
	for _, descriptor := range descriptors {
		workload := findWorkload(app, descriptor.Workload)
		if workload == nil || !selectorMatches(selector, workload.Labels) {
			continue
		}
		if ip, err := e.allocateIP(app.Identity.Name, descriptor.ID); err == nil {
			ips = append(ips, ip)
		}
	}
	return ips
}

// tapMAC derives a stable locally administered MAC per sandbox so two VMs never
// collide.
func tapMAC(id string) string {
	sum := sha256.Sum256([]byte("mac:" + id))
	return fmt.Sprintf("02:00:%02x:%02x:%02x:%02x", sum[0], sum[1], sum[2], sum[3])
}

func tapName(id string) string {
	sum := sha256.Sum256([]byte(id))
	return "tap" + hex.EncodeToString(sum[:4])
}

func sanitizeName(name string) string {
	var builder strings.Builder
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			builder.WriteRune(r)
		default:
			builder.WriteByte('-')
		}
	}
	if builder.Len() == 0 {
		return "app"
	}
	return builder.String()
}
