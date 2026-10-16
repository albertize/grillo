//go:build linux

// SPDX-License-Identifier: Apache-2.0

package guest

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestReviewedRuncPinIsVerifiedRecordedAndIsolated(t *testing.T) {
	cfg, pins := runtimeFixture(t)
	original, err := buildRuntimeImage(context.Background(), cfg, pins)
	if err != nil {
		t.Fatal(err)
	}
	originalDigest := pins[0].digest
	data := []byte("reviewed replacement runc")
	if err := os.WriteFile(filepath.Join(cfg.Inputs, "runc"), data, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := buildRuntimeImage(context.Background(), cfg, pins); err == nil {
		t.Fatal("changed runc accepted without explicit selection")
	}
	sum := sha256.Sum256(data)
	cfg.RuncVersion, cfg.RuncSHA256 = "1.5.2+grillo.1", hex.EncodeToString(sum[:])
	selected, err := buildRuntimeImage(context.Background(), cfg, pins)
	if err != nil || selected == original {
		t.Fatal("replacement did not get a distinct immutable identity", err)
	}
	if pins[0].digest != originalDigest || pins[0].version != runtimeInputs[0].version {
		t.Fatal("selection mutated default pins")
	}
	raw, err := os.ReadFile(filepath.Join(selected, "runtime-build.json"))
	if err != nil {
		t.Fatal(err)
	}
	var build RuntimeBuild
	if err := json.Unmarshal(raw, &build); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, component := range build.Components {
		if component.Name == "runc" {
			found = true
			if component.Version != "runc/"+cfg.RuncVersion || component.SHA256 != cfg.RuncSHA256 {
				t.Fatal("selected runc identity missing from inventory", component)
			}
		}
	}
	if !found {
		t.Fatal("runc missing from inventory")
	}
	if manifest, err := ReadBootManifest(filepath.Join(original, "manifest.json"), false); err != nil || manifest.Verify() != nil {
		t.Fatal("previous immutable guest was changed", err)
	}
	cfg.RuncSHA256 = strings.Repeat("0", 64)
	if _, err := buildRuntimeImage(context.Background(), cfg, pins); err == nil || !strings.Contains(err.Error(), "digest mismatch") {
		t.Fatal("selected pin mismatch did not fail closed", err)
	}
}

func TestReviewedRuncSelectionRejectsIncompleteOrMalformedIdentity(t *testing.T) {
	for _, tc := range []struct{ version, digest string }{
		{"1.5.2", ""}, {"", strings.Repeat("0", 64)},
		{"1.5.2", "short"}, {"1.5.2", strings.Repeat("A", 64)},
		{"1.5.2", strings.Repeat("z", 64)}, {"../runc", strings.Repeat("0", 64)},
		{"version\nforged", strings.Repeat("0", 64)}, {strings.Repeat("v", 65), strings.Repeat("0", 64)},
	} {
		cfg, pins := runtimeFixture(t)
		cfg.RuncVersion, cfg.RuncSHA256 = tc.version, tc.digest
		if _, err := buildRuntimeImage(context.Background(), cfg, pins); err == nil {
			t.Fatalf("invalid reviewed selection accepted: %+v", tc)
		}
		if _, err := os.Stat(cfg.Store); !os.IsNotExist(err) {
			t.Fatal("invalid selection touched publication store", err)
		}
	}
}

func runtimeFixture(t *testing.T) (RuntimeImageConfig, []runtimeInput) {
	t.Helper()
	root := t.TempDir()
	cfg := RuntimeImageConfig{Agent: filepath.Join(root, "agent"), Kernel: filepath.Join(root, "kernel"), Inputs: filepath.Join(root, "inputs"), Store: filepath.Join(root, "store"), Version: "test", Commit: "test-commit", KernelVersion: "test-kernel"}
	for _, file := range []string{cfg.Agent, cfg.Kernel} {
		if err := os.WriteFile(file, []byte("fixture-"+filepath.Base(file)), 0600); err != nil {
			t.Fatal(err)
		}
	}
	pins := append([]runtimeInput(nil), runtimeInputs...)
	for i, pin := range pins {
		path := filepath.Join(cfg.Inputs, pin.path)
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		data := []byte("fixture-" + pin.target)
		sum := sha256.Sum256(data)
		pins[i].digest = hex.EncodeToString(sum[:])
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	return cfg, pins
}

func TestRuntimeImageDeterministicConcurrentAndImmutable(t *testing.T) {
	cfg, pins := runtimeFixture(t)
	var wg sync.WaitGroup
	paths := make(chan string, 4)
	errs := make(chan error, 4)
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			path, err := buildRuntimeImage(context.Background(), cfg, pins)
			paths <- path
			errs <- err
		}()
	}
	wg.Wait()
	close(paths)
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	first := ""
	for path := range paths {
		if first == "" {
			first = path
		}
		if path != first {
			t.Fatal("concurrent identity divergence")
		}
	}
	image, err := os.ReadFile(filepath.Join(first, "initramfs.cpio.gz"))
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifyRuntimeImage(image, nil); err != nil {
		t.Fatal(err)
	}
	manifest, err := ReadBootManifest(filepath.Join(first, "manifest.json"), false)
	if err != nil {
		t.Fatal(err)
	}
	if err := manifest.Verify(); err != nil {
		t.Fatal(err)
	}
	// Retry never publishes a different image for the same build identity.
	path, err := buildRuntimeImage(context.Background(), cfg, pins)
	if err != nil || path != first {
		t.Fatal(path, err)
	}
	again, _ := os.ReadFile(filepath.Join(path, "initramfs.cpio.gz"))
	if !bytes.Equal(image, again) {
		t.Fatal("image changed on retry")
	}
	if err := os.WriteFile(cfg.Agent, []byte("new-agent"), 0600); err != nil {
		t.Fatal(err)
	}
	next, err := buildRuntimeImage(context.Background(), cfg, pins)
	if err != nil || next == first {
		t.Fatal("new input overwrote immutable guest", err)
	}
	original, _ := os.ReadFile(filepath.Join(first, "initramfs.cpio.gz"))
	if !bytes.Equal(original, image) {
		t.Fatal("old guest changed")
	}
}

func TestRuntimeImageRejectsTamperingUnsafeAndInterruptedInputs(t *testing.T) {
	for _, attack := range []string{"pin", "symlink", "cancel", "cache-image", "cache-record"} {
		t.Run(attack, func(t *testing.T) {
			cfg, pins := runtimeFixture(t)
			ctx := context.Background()
			switch attack {
			case "pin":
				if err := os.WriteFile(filepath.Join(cfg.Inputs, pins[0].path), []byte("tampered"), 0600); err != nil {
					t.Fatal(err)
				}
			case "symlink":
				path := filepath.Join(cfg.Inputs, pins[0].path)
				if err := os.Rename(path, path+".real"); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(path+".real", path); err != nil {
					t.Fatal(err)
				}
			case "cancel":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			case "cache-image", "cache-record":
				path, err := buildRuntimeImage(ctx, cfg, pins)
				if err != nil {
					t.Fatal(err)
				}
				name := "initramfs.cpio.gz"
				if attack == "cache-record" {
					name = "runtime-build.json"
				}
				if err := os.WriteFile(filepath.Join(path, name), []byte("incomplete"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := buildRuntimeImage(ctx, cfg, pins); err == nil {
				t.Fatal("unsafe input/cache accepted")
			}
			entries, _ := os.ReadDir(cfg.Store)
			for _, entry := range entries {
				if len(entry.Name()) < 64 {
					t.Fatal("temporary stage leaked", entry.Name())
				}
			}
		})
	}
}

func TestRuntimeImageScannerRejectsFixturesKeysAndCorruptArchives(t *testing.T) {
	entries := runtimeDirectories()
	entries = append(entries, imageEntry{"init", 0100755, []byte("agent")})
	for _, pin := range runtimeInputs {
		entries = append(entries, imageEntry{pin.target, 0100755, []byte("pinned-component")})
	}
	image, err := packRuntimeImage(entries)
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifyRuntimeImage(image, nil); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []imageEntry{{"etc/grillo/key", 0100600, []byte("synthetic-key")}, {"rootfs-app", 0040755, nil}, {"../escape", 0100755, []byte("x")}, {"init", 0100755, []byte("duplicate")}, {"lib64", 0120777, []byte("../outside")}} {
		changed := append(append([]imageEntry(nil), entries...), bad)
		image, err := packRuntimeImage(changed)
		if err != nil {
			t.Fatal(err)
		}
		if err := VerifyRuntimeImage(image, nil); err == nil {
			t.Fatal("forbidden namespace accepted", bad.name)
		}
	}
	if err := VerifyRuntimeImage(image, [][]byte{[]byte("pinned-component")}); err == nil {
		t.Fatal("known credential marker ignored")
	}
	for end := 0; end < len(image); end++ {
		if err := VerifyRuntimeImage(image[:end], nil); err == nil {
			t.Fatal("truncated image accepted", end)
		}
	}
	if err := VerifyRuntimeImage(append(append([]byte(nil), image...), image...), nil); err == nil {
		t.Fatal("multiple archives accepted")
	}
}

func FuzzRuntimeImageScanner(f *testing.F) {
	f.Add([]byte("not-an-image"))
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 1<<20 {
			t.Skip()
		}
		_ = VerifyRuntimeImage(data, nil)
	})
}
