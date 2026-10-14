//go:build kvm

// SPDX-License-Identifier: Apache-2.0

package build

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/albertize/grillo/internal/backend/qemu"
)

// This gate fails rather than skips when hardware/artifacts are missing.
func TestKVMNativeBlockWorkspace(t *testing.T) {
	if _, err := os.Stat("/dev/kvm"); err != nil {
		t.Fatal("BLOCKED: /dev/kvm unavailable")
	}
	formatter, err := exec.LookPath("mke2fs")
	if err != nil {
		t.Fatal("BLOCKED: mke2fs unavailable")
	}
	formatter, err = filepath.Abs(formatter)
	if err != nil {
		t.Fatal(err)
	}
	version, err := exec.Command(formatter, "-V").CombinedOutput()
	if err != nil || !strings.HasPrefix(string(version), "mke2fs 1.47.3 ") {
		t.Fatal("BLOCKED: reviewed mke2fs 1.47.3 required")
	}
	root := buildRepoRoot(t)
	kernel := filepath.Join(root, "experiments/artifacts/qemu/bzImage")
	initrd := filepath.Join(root, "experiments/artifacts/t07/initramfs-agent.cpio.gz")
	keyPath := filepath.Join(root, "experiments/artifacts/t07/key")
	busybox := filepath.Join(root, "experiments/artifacts/t02/rootfs/bin/busybox")
	for _, name := range []string{kernel, initrd, keyPath, busybox} {
		if _, err := os.Stat(name); err != nil {
			t.Fatalf("BLOCKED: prerequisite missing: %s", name)
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
	work, err := os.MkdirTemp("", "grillo-block-kvm-")
	if err != nil {
		t.Fatal(err)
	}
	success := false
	defer func() {
		if success {
			if err := os.RemoveAll(work); err != nil {
				t.Error(err)
			}
		} else {
			t.Logf("failed owned state retained: %s", work)
		}
	}()
	backend, err := qemu.Open(qemu.Config{Kernel: kernel, Initramfs: initrd, WorkDir: filepath.Join(work, "backend"), CIDBase: 700, BootTimeout: 30 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	boot := &GuestBoot{Backend: backend, Kernel: kernel, Initramfs: initrd, KernelArgs: "console=ttyS0 reboot=k panic=1 rdinit=/init", GuestKey: key, VsockCIDBase: 700, VsockPort: 1024, MemoryMiB: 512}
	b, _, puller := newNativeBuilder(t, nil)
	b.ScratchDir = filepath.Join(work, "scratch")
	if err := os.Mkdir(b.ScratchDir, 0700); err != nil {
		t.Fatal(err)
	}
	b.Runner = &BlockRunner{Formatter: formatter, ImageBytes: 32 << 20, Boot: boot.BootBlock}
	contextDir := filepath.Join(work, "context")
	if err := os.Mkdir(contextDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := copyTree(busybox, filepath.Join(contextDir, "busybox"), copyOptions{}); err != nil {
		t.Fatal(err)
	}
	quota := []string{"/bin/busybox", "sh", "-c", "out=$(/bin/busybox dd if=/dev/zero of=/fill bs=1048576 count=64 2>&1); rc=$?; test \"$rc\" -ne 0 || exit 10; case \"$out\" in *\"No space left on device\"*) ;; *) exit 11;; esac; /bin/busybox rm /fill; echo quota-ok > /result"}
	second := []string{"/bin/busybox", "sh", "-c", "/bin/busybox cat /result /after > /out; /bin/busybox rm /before; /bin/busybox chown 1234:1235 /out; /bin/busybox chmod 4750 /out; /bin/busybox chmod 0750 /"}
	q, _ := json.Marshal(quota)
	s, _ := json.Marshal(second)
	dockerfile := "FROM scratch\nCOPY busybox /bin/busybox\nCOPY before /before\nRUN " + string(q) + "\nCOPY after /after\nRUN " + string(s) + "\n"
	for name, data := range map[string]string{"Dockerfile": dockerfile, "before": "before", "after": "after"} {
		if err := os.WriteFile(filepath.Join(contextDir, name), []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	result, err := b.Build(ctx, Request{ContextDir: contextDir, Reference: "grillo.local/kvm-block:1", Network: "none"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	imageRoot := unpackImage(t, puller, result.Image)
	if got := readFile(t, imageRoot, "out"); got != "quota-ok\nafter" {
		t.Fatalf("unexpected interleaved result: %q", got)
	}
	if _, err := os.Stat(filepath.Join(imageRoot, "before")); !os.IsNotExist(err) {
		t.Fatal("RUN deletion lost", err)
	}
	if entries, err := os.ReadDir(b.ScratchDir); err != nil || len(entries) != 0 {
		t.Fatal("successful build left workspace", err)
	}
	success = true
}
