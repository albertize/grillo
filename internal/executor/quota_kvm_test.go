//go:build linux && kvm

// SPDX-License-Identifier: Apache-2.0

package executor

import (
	"bytes"
	"context"
	"encoding/base64"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/albertize/grillo/internal/backend/qemu"
	"github.com/albertize/grillo/internal/model"
	"github.com/albertize/grillo/internal/reconcile"
	"github.com/albertize/grillo/internal/storage"
)

// Uses real guest writes through the production executor's new block mapping,
// not a pre-write directory-size admission check or a fake storage backend.
func TestKVMBoundedVolumeENOSPCPersistence(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Fatal("unprivileged host required")
	}
	root := repoRoot(t)
	kernel := filepath.Join(root, "experiments/artifacts/qemu/bzImage")
	initramfs := filepath.Join(root, "experiments/artifacts/t07/initramfs-agent.cpio.gz")
	keyData, err := os.ReadFile(filepath.Join(root, "experiments/artifacts/t07/key"))
	if err != nil {
		t.Fatal("rebuilt guest fixture required", err)
	}
	key, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(keyData)))
	if err != nil {
		t.Fatal(err)
	}
	formatter, err := exec.LookPath("mke2fs")
	if err != nil {
		t.Fatal("reviewed mke2fs required")
	}
	formatter, _ = filepath.Abs(formatter)
	work, err := os.MkdirTemp("", "grillo-t24-volume-")
	if err != nil {
		t.Fatal(err)
	}
	var rec *reconcile.Reconciler
	t.Cleanup(func() {
		cleanup, stop := context.WithTimeout(context.Background(), 30*time.Second)
		defer stop()
		if rec != nil {
			if _, err := rec.Down(cleanup, "bounded", false); err != nil {
				t.Error("owned volume runtime cleanup", err)
			}
		}
		entries, err := os.ReadDir(filepath.Join(work, "backend"))
		if err != nil && !os.IsNotExist(err) || len(entries) != 0 {
			t.Error("owned backend state remains after teardown", err)
		}
		if t.Failed() {
			t.Log("failed operation-owned evidence retained", work)
			return
		}
		if err := os.RemoveAll(work); err != nil {
			t.Error("remove owned quota fixture", err)
		}
	})
	volumes, err := storage.OpenBounded(filepath.Join(work, "storage"), storage.BlockOptions{Formatter: formatter, DefaultBytes: 32 << 20, MaxReservedBytes: 64 << 20, MaxImages: 2})
	if err != nil {
		t.Fatal(err)
	}
	backend, err := qemu.Open(qemu.Config{Kernel: kernel, Initramfs: initramfs, WorkDir: filepath.Join(work, "backend"), CIDBase: 16000, BootTimeout: 30 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := New(Config{Backend: backend, Volumes: volumes, Kernel: kernel, Initramfs: initramfs, KernelArgs: "console=ttyS0 reboot=k panic=1 rdinit=/init", GuestKey: key, VsockPort: 1024, VsockCIDBase: 16000, Images: directoryResolver{dir: filepath.Join(root, "experiments/artifacts/t02/rootfs")}})
	if err != nil {
		t.Fatal(err)
	}
	rec = &reconcile.Reconciler{Store: reconcile.NewMemoryStore(), Executor: &reconcile.NativeExecutor{Sandboxes: SandboxController{runtime}, Volumes: VolumeController{runtime}}}
	app := model.Application{APIVersion: model.APIVersion, Kind: model.KindApplication, Identity: model.Identity{Name: "bounded", Namespace: "default"}, Volumes: []model.Volume{{Name: "data", Kind: model.VolumeManaged, Capacity: 32 << 20}}, Workloads: []model.Workload{{ID: "web", Kind: model.WorkloadDeployment, Replicas: 1, Template: model.SandboxTemplate{Containers: []model.Container{{Name: "app", Image: model.ImageRef{Reference: "busybox:1.37"}, Command: &[]string{"/bin/sh", "-c", "sleep 300"}, Mounts: []model.VolumeMount{{Volume: "data", MountPath: "/data"}}}}}}}}
	runtime.SetDesired(app)
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	if _, err := rec.Apply(ctx, app); err != nil {
		t.Fatal(err)
	}
	run := func(command string) (int, string, string) {
		t.Helper()
		var out, errOut bytes.Buffer
		code, err := runtime.Exec(ctx, "bounded", "app", []string{"/bin/sh", "-c", command}, &out, &errOut)
		if err != nil {
			t.Fatal(err)
		}
		return code, out.String(), errOut.String()
	}
	if code, _, stderr := run("echo persisted > /data/token && sync"); code != 0 {
		t.Fatal(code, stderr)
	}
	code, _, stderr := run("dd if=/dev/zero of=/data/full bs=1048576 count=64; result=$?; sync; exit $result")
	if code == 0 || !strings.Contains(strings.ToLower(stderr), "no space left") {
		t.Fatal("guest write did not reach real ENOSPC", code, stderr)
	}
	stored, _, err := volumes.Inspect(volumeID("bounded", "data"))
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(stored.Source)
	if err != nil || info.Size() != 32<<20 {
		t.Fatal("host image expanded", err)
	}
	if code, _, stderr := run("rm /data/full && echo recovered > /data/retry && sync"); code != 0 {
		t.Fatal("quota retry", code, stderr)
	}
	if _, err := rec.Down(ctx, "bounded", false); err != nil {
		t.Fatal(err)
	}
	if _, err := rec.Apply(ctx, app); err != nil {
		t.Fatal("persistent restart", err)
	}
	if code, out, stderr := run("cat /data/token /data/retry"); code != 0 || !strings.Contains(out, "persisted") || !strings.Contains(out, "recovered") {
		t.Fatal("persistent data missing", code, out, stderr)
	}
	if _, err := rec.Down(ctx, "bounded", true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(stored.Source); !os.IsNotExist(err) {
		t.Fatal("explicit owned volume removal failed", err)
	}
	t.Log("PASS: real executor ext4/virtio-blk writes reach ENOSPC at fixed geometry, removal permits retry, data survives replacement VM, explicit volume deletion")
}
