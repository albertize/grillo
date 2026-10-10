//go:build linux

// SPDX-License-Identifier: Apache-2.0

package distribution

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func TestBinaryProvenanceFromCopiedGoBytes(t *testing.T) {
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	result, err := binaryProvenance(binary, "bin/test")
	if err != nil {
		t.Fatal(err)
	}
	if result.Path != "bin/test" || !strings.HasPrefix(result.GoVersion, "go1.") {
		t.Fatal("missing actual executable provenance", result)
	}
	fake := filepath.Join(t.TempDir(), "fake")
	if err := os.WriteFile(fake, []byte("not Go executable metadata"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := binaryProvenance(fake, "bin/fake"); err == nil {
		t.Fatal("missing metadata accepted")
	}
}

func TestModuleProvenancePrivateReplacementAndBounds(t *testing.T) {
	for _, path := range []string{"/private/fixture", "../private/fixture", "C:\\private\\fixture", "C:/private/fixture", "relative/private/fixture"} {
		module, err := moduleProvenance(path, "", "", true)
		if err != nil || module.Path != "<local-replacement>" {
			t.Fatal("private replacement path exposed", err)
		}
	}
	module, err := moduleProvenance("golang.org/x/net", "v0.60.0", "h1:synthetic", false)
	if err != nil || module.Path != "golang.org/x/net" {
		t.Fatal("module metadata lost", err)
	}
	if _, err := moduleProvenance(strings.Repeat("x", 1025), "", "", false); err == nil {
		t.Fatal("metadata size not bounded")
	}
}

func noticeFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for _, name := range []string{"internal/ui/assets/generated/licenses.txt", "web/licenses/font-awesome-LICENSE.txt", "web/licenses/redhatdisplay-OFL.txt", "web/licenses/redhatmono-OFL.txt", "web/licenses/redhattext-OFL.txt"} {
		file := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(file), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(file, []byte("synthetic upstream notice\n"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	return root
}
func TestRuntimeNoticesCopiedVerbatimAndInventoried(t *testing.T) {
	source := noticeFixture(t)
	stage := t.TempDir()
	inventory := Inventory{}
	if err := copyRuntimeNotices(context.Background(), source, stage, &inventory); err != nil {
		t.Fatal(err)
	}
	if len(inventory.Files) != 5 || !strings.HasPrefix(inventory.NoticeCoverage, "partial:") {
		t.Fatal("notice completeness fabricated", inventory)
	}
	for _, file := range inventory.Files {
		data, err := os.ReadFile(filepath.Join(stage, file.Path))
		if err != nil || string(data) != "synthetic upstream notice\n" {
			t.Fatal("notice changed", err)
		}
		if len(file.SHA256) != 64 || file.Size != int64(len(data)) {
			t.Fatal("notice not inventoried", file)
		}
	}
}
func TestRuntimeNoticesRejectUnsafeMissingOversizedAndCancelled(t *testing.T) {
	for _, kind := range []string{"missing", "empty", "symlink", "fifo", "oversized"} {
		t.Run(kind, func(t *testing.T) {
			source := noticeFixture(t)
			file := filepath.Join(source, "web/licenses/font-awesome-LICENSE.txt")
			if err := os.Remove(file); err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "empty":
				if err := os.WriteFile(file, nil, 0600); err != nil {
					t.Fatal(err)
				}
			case "symlink":
				outside := filepath.Join(t.TempDir(), "preserve")
				if err := os.WriteFile(outside, []byte("preserve"), 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(outside, file); err != nil {
					t.Fatal(err)
				}
			case "fifo":
				if err := syscall.Mkfifo(file, 0600); err != nil {
					t.Fatal(err)
				}
			case "oversized":
				f, err := os.Create(file)
				if err != nil {
					t.Fatal(err)
				}
				err = f.Truncate((1 << 20) + 1)
				f.Close()
				if err != nil {
					t.Fatal(err)
				}
			}
			if err := copyRuntimeNotices(context.Background(), source, t.TempDir(), &Inventory{}); err == nil {
				t.Fatal("unsafe notice accepted")
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := copyRuntimeNotices(ctx, noticeFixture(t), t.TempDir(), &Inventory{}); !errors.Is(err, context.Canceled) {
		t.Fatal("notice copy ignored cancellation", err)
	}
}
