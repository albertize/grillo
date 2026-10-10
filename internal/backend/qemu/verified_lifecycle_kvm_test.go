//go:build linux && kvm

// SPDX-License-Identifier: Apache-2.0

package qemu

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/albertize/grillo/internal/guest"
	"github.com/albertize/grillo/internal/observe"
	linux "github.com/albertize/grillo/internal/platform/linux"
	"github.com/albertize/grillo/internal/sandbox"
)

// Measures the production verified-copy/fresh-overlay path, not the historical
// fixture-key path. Warm agent-only boots do not measure application readiness,
// namespace networking, cold pulls or complete system resource budgets.
func TestKVMVerifiedRuntimeLifecycle(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("SKIP: unprivileged host required")
	}
	for _, path := range []string{"/dev/kvm", "/dev/vhost-vsock"} {
		file, err := os.OpenFile(path, os.O_RDWR, 0)
		if err != nil {
			t.Skipf("SKIP: inaccessible %s", path)
		}
		file.Close()
	}
	selection := os.Getenv("GRILLO_TEST_GUEST_SELECTION")
	if selection == "" {
		t.Fatal("explicit runtime guest selection required")
	}
	if !filepath.IsAbs(selection) {
		selection = filepath.Join(repoRoot(t), selection)
	}
	data, err := os.ReadFile(selection)
	if err != nil || len(data) > 4096 {
		t.Fatal("invalid runtime guest selection")
	}
	directory := strings.TrimSpace(string(data))
	manifestPath := filepath.Join(directory, "manifest.json")
	manifest, err := guest.ReadBootManifest(manifestPath, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := manifest.Verify(); err != nil {
		t.Fatal(err)
	}
	cycles := 100
	if value := os.Getenv("GRILLO_KVM_CYCLES"); value != "" {
		cycles, err = strconv.Atoi(value)
		if err != nil || cycles < 1 || cycles > 1000 {
			t.Fatal("cycles must be 1..1000")
		}
	}
	work := filepath.Join(t.TempDir(), "backend")
	backend, err := Open(Config{Kernel: filepath.Join(directory, "bzImage"), Initramfs: filepath.Join(directory, "initramfs.cpio.gz"), ArtifactManifest: manifestPath, RequirePortableManifest: true, BootKeyOverlay: true, WorkDir: work, CIDBase: 12000, BootTimeout: 30 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(cycles)*30*time.Second)
	defer cancel()
	var handles []sandbox.Handle
	var identities []linux.ProcessID
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		for _, handle := range handles {
			if err := backend.Delete(cleanup, handle); err != nil {
				t.Error("owned sandbox cleanup", err)
			}
		}
		for _, identity := range identities {
			if identity.Alive() {
				t.Error("owned VMM survived cleanup")
			}
		}
	})
	phases := map[string][]int64{}
	var rss, pss, snapshot []int64
	keys := map[string]bool{}
	for cycle := 0; cycle < cycles; cycle++ {
		spec := sandbox.Spec{ID: fmt.Sprintf("verified-%d", cycle), Kernel: filepath.Join(directory, "bzImage"), Initramfs: filepath.Join(directory, "initramfs.cpio.gz"), KernelArgs: "console=ttyS0 reboot=k panic=1 rdinit=/init", VsockPort: 1024, GuestKey: bytes.Repeat([]byte{1}, 32)}
		start := time.Now()
		handle, err := backend.Create(ctx, spec, sandbox.OperationID(spec.ID))
		if err != nil {
			t.Fatal(err)
		}
		handles = append(handles, handle)
		phases["create_us"] = append(phases["create_us"], time.Since(start).Microseconds())
		start = time.Now()
		if err := backend.Start(ctx, handle); err != nil {
			t.Fatal("verified boot", err)
		}
		phases["verified_start_to_agent_us"] = append(phases["verified_start_to_agent_us"], time.Since(start).Microseconds())
		obs, err := backend.Inspect(ctx, handle)
		if err != nil || obs.State != sandbox.StateRunning || !obs.GuestAlive || obs.PID <= 0 || obs.GuestZombies != 0 {
			t.Fatal("invalid real guest observation", err)
		}
		identity, err := linux.Identify(obs.PID)
		if err != nil {
			t.Fatal(err)
		}
		identities = append(identities, identity)
		_, _, key, err := backend.BootConnection(handle)
		if err != nil || len(key) != 32 || bytes.Equal(key, spec.GuestKey) || keys[string(key)] {
			t.Fatal("fresh-key contract failed")
		}
		keys[string(key)] = true
		metric, err := observe.SampleProcess(obs.PID)
		if err != nil || metric.RSSBytes <= 0 {
			t.Fatal("VMM RSS unavailable", err)
		}
		rss = append(rss, metric.RSSBytes)
		value, err := readLifecyclePSS(obs.PID)
		if err != nil {
			t.Fatal("VMM PSS unavailable", err)
		}
		pss = append(pss, value)
		used, err := lifecycleLogicalBytes(ctx, work)
		if err != nil {
			t.Fatal(err)
		}
		snapshot = append(snapshot, used)
		// An idempotent live start must retain this VM identity and credential.
		if err := backend.Start(ctx, handle); err != nil {
			t.Fatal(err)
		}
		_, _, liveKey, err := backend.BootConnection(handle)
		if err != nil || !bytes.Equal(key, liveKey) || !identity.Alive() {
			t.Fatal("live idempotency contract failed")
		}
		start = time.Now()
		if err := backend.Stop(ctx, handle, 5*time.Second); err != nil {
			t.Fatal(err)
		}
		if err := backend.Delete(ctx, handle); err != nil {
			t.Fatal(err)
		}
		phases["stop_delete_us"] = append(phases["stop_delete_us"], time.Since(start).Microseconds())
		if identity.Alive() {
			t.Fatal("owned VMM survived teardown")
		}
		entries, err := os.ReadDir(work)
		if err != nil || len(entries) != 0 {
			t.Fatal("owned backend resources retained", err)
		}
	}
	for _, name := range []string{"create_us", "verified_start_to_agent_us", "stop_delete_us"} {
		logLifecycleSamples(t, name, phases[name])
	}
	logLifecycleSamples(t, "vmm_rss_bytes_after_auth", rss)
	logLifecycleSamples(t, "vmm_pss_bytes_after_auth", pss)
	logLifecycleSamples(t, "owned_logical_bytes_running", snapshot)
	t.Logf("PASS: %d verified runtime-only boots, fresh keys, idempotency, zero retained backend files/VMMs; no application/idle/system-wide budget claim", cycles)
}
