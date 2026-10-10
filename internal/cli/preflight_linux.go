//go:build linux

// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"github.com/albertize/grillo/internal/frontend/helm"
	"github.com/albertize/grillo/internal/runtimeassets"
	"golang.org/x/sys/unix"
)

// NamespaceProbeArgument is an internal no-op entry point. Its parent creates
// transient user/network namespaces with SysProcAttr; the child exits without
// touching files, network interfaces, services or workload state.
const NamespaceProbeArgument = "--internal-doctor-namespace-probe"

type readinessOutput struct{ buffer bytes.Buffer }

func (b *readinessOutput) Write(data []byte) (int, error) {
	if b.buffer.Len()+len(data) > 128<<10 {
		return 0, fmt.Errorf("readiness output limit")
	}
	return b.buffer.Write(data)
}
func readOnlyTool(ctx context.Context, binary string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, binary, args...)
	command.WaitDelay = 500 * time.Millisecond
	command.Env = []string{"PATH=" + os.Getenv("PATH"), "LANG=C", "LC_ALL=C", "HOME=/nonexistent/grillo-doctor", "HELM_PLUGINS=/nonexistent/grillo-doctor/plugins", "HELM_CONFIG_HOME=/nonexistent/grillo-doctor/config", "HELM_DATA_HOME=/nonexistent/grillo-doctor/data", "HELM_CACHE_HOME=/nonexistent/grillo-doctor/cache"}
	var output readinessOutput
	command.Stdout = &output
	command.Stderr = io.Discard
	if err := command.Run(); err != nil {
		return "", err
	}
	return output.buffer.String(), nil
}

func actualNamespaceCheck(ctx context.Context) Check {
	check := Check{Name: "namespace-access", Status: "fail", Detail: "cannot create transient unprivileged user/network namespaces"}
	binary, err := os.Executable()
	if err != nil {
		return check
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, binary, NamespaceProbeArgument)
	command.SysProcAttr = &syscall.SysProcAttr{Cloneflags: unix.CLONE_NEWUSER | unix.CLONE_NEWNET, UidMappings: []syscall.SysProcIDMap{{ContainerID: 0, HostID: os.Getuid(), Size: 1}}, GidMappings: []syscall.SysProcIDMap{{ContainerID: 0, HostID: os.Getgid(), Size: 1}}, GidMappingsEnableSetgroups: false}
	if err := command.Run(); err != nil {
		return check
	}
	check.Status = "ok"
	check.Detail = "transient user/network namespace creation verified; TAP/firewall/MAC datapath still needs real E2E"
	return check
}

func toolCapabilityChecks(ctx context.Context) []Check {
	var checks []Check
	machine, err := readOnlyTool(ctx, "qemu-system-x86_64", "-no-user-config", "-machine", "help")
	devices, deviceErr := readOnlyTool(ctx, "qemu-system-x86_64", "-no-user-config", "-device", "help")
	qemu := Check{Name: "qemu-devices", Status: "ok", Detail: "microvm and required vsock/virtio-fs/net/block devices listed"}
	if err != nil || deviceErr != nil || !strings.Contains(machine, "microvm") {
		qemu.Status = "fail"
		qemu.Detail = "cannot list QEMU microvm capabilities"
	}
	for _, name := range []string{"vhost-vsock-device", "vhost-user-fs-device", "virtio-net-device", "virtio-blk-device"} {
		if !strings.Contains(devices, name) {
			qemu.Status = "fail"
			qemu.Detail = "required QEMU microvm device unavailable"
		}
	}
	checks = append(checks, qemu)
	version, err := readOnlyTool(ctx, "qemu-system-x86_64", "--version")
	qemuVersion := Check{Name: "qemu-version", Status: "warn", Detail: "version differs from locally evidenced 10.2.2; compatibility not verified"}
	if err == nil && strings.Contains(version, "version 10.2.2 ") {
		qemuVersion.Status = "ok"
		qemuVersion.Detail = "locally evidenced QEMU 10.2.2 (not a supported distro range)"
	}
	checks = append(checks, qemuVersion)
	path := virtioFSDCheck().Detail
	help, err := readOnlyTool(ctx, path, "--help")
	fsCheck := Check{Name: "virtiofsd-options", Status: "ok", Detail: "required filesystem helper options listed"}
	if err != nil || !strings.Contains(help, "--socket-path") || !strings.Contains(help, "--shared-dir") || !strings.Contains(help, "--sandbox") {
		fsCheck.Status = "fail"
		fsCheck.Detail = "compatible virtiofsd options unavailable"
	}
	checks = append(checks, fsCheck)
	renderer, err := runtimeassets.Resolve("helm", os.Getenv("GRILLO_HELM_BINARY"))
	version = ""
	if err == nil {
		version, err = readOnlyTool(ctx, renderer, "version", "--template", "{{.Version}}")
	}
	helmCheck := Check{Name: "helm-version", Status: "ok", Detail: "exact managed renderer " + helm.Version}
	if err != nil || strings.TrimSpace(version) != helm.Version {
		helmCheck.Status = "fail"
		helmCheck.Detail = "require exact official Helm " + helm.Version + "; renderer output suppressed"
	}
	checks = append(checks, helmCheck)
	if data, err := os.ReadFile("/sys/fs/selinux/enforce"); err == nil && strings.TrimSpace(string(data)) == "1" {
		checks = append(checks, Check{Name: "mac-policy", Status: "fail", Detail: "SELinux Enforcing runtime networking is not validated; known Fedora pasta blocker remains"})
	}
	checks = append(checks, Check{Name: "host-support", Status: "warn", Detail: "no supported release/clean-host matrix yet; read-only probes are not an E2E or security guarantee"})
	return checks
}

func kvmAPICheck() Check {
	check := Check{Name: "kvm-api", Status: "fail", Detail: "cannot query usable KVM API; distinguish missing device from access/policy denial"}
	fd, err := unix.Open("/dev/kvm", unix.O_RDWR|unix.O_CLOEXEC, 0)
	if err != nil {
		return check
	}
	defer unix.Close(fd)
	// Linux KVM_GET_API_VERSION is a read-only ioctl and does not create a VM.
	version, err := unix.IoctlRetInt(fd, 0xAE00)
	if err != nil || version != 12 {
		return check
	}
	check.Status = "ok"
	check.Detail = "KVM API 12 queried without creating a VM"
	return check
}
