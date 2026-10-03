//go:build kvm

// SPDX-License-Identifier: Apache-2.0

// Real KVM test for build execution inside a sandboxed guest (T18b).
// Run with: make test-builder-kvm

package build

import (
	"context"
	"encoding/base64"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"grillo.local/grillo/internal/backend/qemu"
	"grillo.local/grillo/internal/oci"
)

func buildRepoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not find repository root")
		}
		dir = parent
	}
}

func TestKVMBuildGuestRun(t *testing.T) {
	if _, err := os.Stat("/dev/kvm"); err != nil {
		t.Skip("SKIP: /dev/kvm is not available")
	}
	root := buildRepoRoot(t)
	kernel := filepath.Join(root, "experiments/artifacts/qemu/bzImage")
	initramfs := filepath.Join(root, "experiments/artifacts/t07/initramfs-agent.cpio.gz")
	keyPath := filepath.Join(root, "experiments/artifacts/t07/key")
	sourceRootfs := filepath.Join(root, "experiments/artifacts/t02/rootfs")
	for _, path := range []string{kernel, initramfs, keyPath, sourceRootfs} {
		if _, err := os.Stat(path); err != nil {
			t.Skipf("SKIP: missing %s (run 'make t07-guest' and 'make oci-guest')", path)
		}
	}
	keyData, err := os.ReadFile(keyPath)
	if err != nil {
		t.Fatal(err)
	}
	key, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(keyData)))
	if err != nil {
		t.Fatal(err)
	}
	backend, err := qemu.Open(qemu.Config{
		Kernel:      kernel,
		Initramfs:   initramfs,
		WorkDir:     filepath.Join(t.TempDir(), "backend"),
		CIDBase:     200,
		BootTimeout: 30 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	// Use a writable copy so the artifact is untouched and writes are observable.
	workroot := filepath.Join(t.TempDir(), "rootfs")
	if err := copyTree(sourceRootfs, workroot, copyOptions{}); err != nil {
		t.Fatal(err)
	}
	boot := &GuestBoot{
		Backend:      backend,
		Kernel:       kernel,
		Initramfs:    initramfs,
		KernelArgs:   "console=ttyS0 reboot=k panic=1 rdinit=/init",
		GuestKey:     key,
		VsockCIDBase: 200,
		VsockPort:    1024,
		MemoryMiB:    512,
		ShareTag:     "build",
		ShareTarget:  "/build",
	}
	runner := &SandboxRunner{Share: "build", Boot: boot.Boot}
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	if err := runner.Run(ctx, RunStep{
		RootfsDir: workroot,
		Command:   []string{"/bin/sh", "-c", "echo grillo-run-ok > /out.txt"},
		WorkDir:   "/",
	}, func(line string) { t.Log(line) }); err != nil {
		t.Fatalf("first run: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(workroot, "out.txt"))
	if err != nil {
		t.Fatalf("run output did not reach the shared root: %v", err)
	}
	if strings.TrimSpace(string(data)) != "grillo-run-ok" {
		t.Fatalf("out.txt = %q", data)
	}

	// A second command reuses the same booted guest.
	if err := runner.Run(ctx, RunStep{
		RootfsDir: workroot,
		Command:   []string{"/bin/sh", "-c", "cat /out.txt >> /second.txt"},
		WorkDir:   "/",
	}, nil); err != nil {
		t.Fatalf("second run: %v", err)
	}
	second, err := os.ReadFile(filepath.Join(workroot, "second.txt"))
	if err != nil || !strings.Contains(string(second), "grillo-run-ok") {
		t.Fatalf("second.txt = %q err=%v", second, err)
	}

	// A failing command is reported with its exit code.
	err = runner.Run(ctx, RunStep{
		RootfsDir: workroot,
		Command:   []string{"/bin/sh", "-c", "exit 7"},
		WorkDir:   "/",
	}, nil)
	if err == nil || !strings.Contains(err.Error(), "7") {
		t.Fatalf("expected exit code error, got %v", err)
	}

	if err := runner.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
}

// TestKVMBuildNativeBuilderRun exercises the full native pipeline: FROM scratch,
// COPY a real root filesystem, then RUN inside the sandboxed build guest, and
// finally publish and unpack the image.
func TestKVMBuildNativeBuilderRun(t *testing.T) {
	if _, err := os.Stat("/dev/kvm"); err != nil {
		t.Skip("SKIP: /dev/kvm is not available")
	}
	root := buildRepoRoot(t)
	kernel := filepath.Join(root, "experiments/artifacts/qemu/bzImage")
	initramfs := filepath.Join(root, "experiments/artifacts/t07/initramfs-agent.cpio.gz")
	keyPath := filepath.Join(root, "experiments/artifacts/t07/key")
	contextDir := filepath.Join(root, "experiments/artifacts/t02/rootfs")
	for _, path := range []string{kernel, initramfs, keyPath, contextDir} {
		if _, err := os.Stat(path); err != nil {
			t.Skipf("SKIP: missing %s (run 'make t07-guest' and 'make oci-guest')", path)
		}
	}
	keyData, err := os.ReadFile(keyPath)
	if err != nil {
		t.Fatal(err)
	}
	key, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(keyData)))
	if err != nil {
		t.Fatal(err)
	}
	backend, err := qemu.Open(qemu.Config{
		Kernel:      kernel,
		Initramfs:   initramfs,
		WorkDir:     filepath.Join(t.TempDir(), "backend"),
		CIDBase:     220,
		BootTimeout: 30 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	builder, _, puller := newNativeBuilder(t, nil)
	builder.Runner = &SandboxRunner{Share: "build", Boot: (&GuestBoot{
		Backend:      backend,
		Kernel:       kernel,
		Initramfs:    initramfs,
		KernelArgs:   "console=ttyS0 reboot=k panic=1 rdinit=/init",
		GuestKey:     key,
		VsockCIDBase: 220,
		VsockPort:    1024,
		MemoryMiB:    512,
		ShareTag:     "build",
		ShareTarget:  "/build",
	}).Boot}
	dockerfilePath := filepath.Join(contextDir, "Dockerfile")
	if err := os.WriteFile(dockerfilePath, []byte("FROM scratch\nCOPY . /\nRUN /bin/sh -c \"echo grillo-native-kvm > /native-run.txt\"\n"), 0o644); err != nil {
		t.Skipf("SKIP: cannot write into the artifact context: %v", err)
	}
	defer os.Remove(dockerfilePath)

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Second)
	defer cancel()
	result, err := builder.Build(ctx, Request{
		ContextDir: contextDir,
		Reference:  "grillo.local/native-kvm:latest",
		Network:    "none",
	}, func(line string) { t.Log(line) })
	if err != nil {
		t.Fatalf("native build: %v", err)
	}
	unpacked := t.TempDir()
	if err := puller.Unpack(result.Image, unpacked, oci.UnpackOptions{}); err != nil {
		t.Fatalf("unpack: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(unpacked, "native-run.txt"))
	if err != nil {
		t.Fatalf("RUN output missing from the published image: %v", err)
	}
	if strings.TrimSpace(string(data)) != "grillo-native-kvm" {
		t.Fatalf("native-run.txt = %q", data)
	}
	if _, err := os.Stat(filepath.Join(unpacked, "bin", "sh")); err != nil {
		t.Fatalf("base rootfs content missing: %v", err)
	}
}

// TestKVMBuildGuestNetwork verifies that a build guest gets a network interface
// and outbound connectivity through pasta, so RUN steps can download packages.
func TestKVMBuildGuestNetwork(t *testing.T) {
	if _, err := os.Stat("/dev/kvm"); err != nil {
		t.Skip("SKIP: /dev/kvm is not available")
	}
	if _, err := exec.LookPath("pasta"); err != nil {
		t.Skip("SKIP: pasta is not installed")
	}
	root := buildRepoRoot(t)
	kernel := filepath.Join(root, "experiments/artifacts/qemu/bzImage")
	initramfs := filepath.Join(root, "experiments/artifacts/t07/initramfs-agent.cpio.gz")
	keyPath := filepath.Join(root, "experiments/artifacts/t07/key")
	sourceRootfs := filepath.Join(root, "experiments/artifacts/t02/rootfs")
	for _, path := range []string{kernel, initramfs, keyPath, sourceRootfs} {
		if _, err := os.Stat(path); err != nil {
			t.Skipf("SKIP: missing %s (run 'make t07-guest' and 'make oci-guest')", path)
		}
	}
	netnsBin := filepath.Join(t.TempDir(), "grillo-netns")
	build := exec.Command("go", "build", "-o", netnsBin, "./cmd/grillo-netns")
	build.Dir = root
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build grillo-netns: %v: %s", err, out)
	}
	runtimeDir := t.TempDir()
	if err := os.MkdirAll(runtimeDir, 0o700); err != nil {
		t.Fatal(err)
	}
	keyData, err := os.ReadFile(keyPath)
	if err != nil {
		t.Fatal(err)
	}
	key, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(keyData)))
	if err != nil {
		t.Fatal(err)
	}
	network := &BuildNetwork{
		QEMU:        "qemu-system-x86_64",
		NetnsBinary: netnsBin,
		Pasta:       "pasta",
		RuntimeDir:  runtimeDir,
		Application: "grillo-build-net",
		Nameservers: testNameservers(),
	}
	defer network.Close()
	backend, err := qemu.Open(qemu.Config{
		Kernel:      kernel,
		Initramfs:   initramfs,
		WorkDir:     filepath.Join(t.TempDir(), "backend"),
		CIDBase:     240,
		BootTimeout: 30 * time.Second,
		Launch:      network.Launch,
	})
	if err != nil {
		t.Fatal(err)
	}
	workroot := filepath.Join(t.TempDir(), "rootfs")
	if err := copyTree(sourceRootfs, workroot, copyOptions{}); err != nil {
		t.Fatal(err)
	}
	boot := &GuestBoot{
		Backend:      backend,
		Kernel:       kernel,
		Initramfs:    initramfs,
		KernelArgs:   "console=ttyS0 reboot=k panic=1 rdinit=/init",
		GuestKey:     key,
		VsockCIDBase: 240,
		VsockPort:    1024,
		MemoryMiB:    512,
		ShareTag:     "build",
		ShareTarget:  "/build",
		Network:      network,
		Nameservers:  testNameservers(),
	}
	runner := &SandboxRunner{Share: "build", Boot: boot.Boot}
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	defer runner.Close()

	if err := runner.Run(ctx, RunStep{
		RootfsDir: workroot,
		Command:   []string{"/bin/sh", "-c", "ip -o -4 addr show eth0 > /iface.txt; ip route > /route.txt"},
		WorkDir:   "/",
	}, nil); err != nil {
		t.Fatalf("interface probe: %v", err)
	}
	iface, err := os.ReadFile(filepath.Join(workroot, "iface.txt"))
	if err != nil || !strings.Contains(string(iface), "10.77.0.") {
		t.Fatalf("eth0 address = %q err=%v", iface, err)
	}

	// Outbound connectivity through pasta, matching the T11 egress probe. If the
	// host itself has no internet, the egress assertion is skipped.
	if err := runner.Run(ctx, RunStep{
		RootfsDir: workroot,
		Command:   []string{"/bin/sh", "-c", "nc -w 10 -z 1.1.1.1 443 && echo ok > /egress.txt || echo fail > /egress.txt"},
		WorkDir:   "/",
	}, nil); err != nil {
		t.Fatalf("egress probe: %v", err)
	}
	egress, err := os.ReadFile(filepath.Join(workroot, "egress.txt"))
	if err != nil {
		t.Fatalf("egress result missing: %v", err)
	}
	if !strings.Contains(string(egress), "ok") {
		if !hostHasInternet() {
			t.Skip("SKIP: the host has no internet to test egress")
		}
		t.Fatalf("build guest egress failed: %q", egress)
	}
}

func hostHasInternet() bool {
	conn, err := net.DialTimeout("tcp", "1.1.1.1:443", 5*time.Second)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

func testNameservers() []string {
	if data, err := os.ReadFile("/etc/resolv.conf"); err == nil {
		var servers []string
		for _, line := range strings.Split(string(data), "\n") {
			fields := strings.Fields(line)
			if len(fields) >= 2 && fields[0] == "nameserver" {
				servers = append(servers, fields[1])
			}
		}
		if len(servers) > 0 {
			return servers
		}
	}
	return []string{"1.1.1.1", "8.8.8.8"}
}
