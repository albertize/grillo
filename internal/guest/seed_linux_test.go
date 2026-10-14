//go:build linux

// SPDX-License-Identifier: Apache-2.0

package guest

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/albertize/grillo/internal/guestproto"
)

func seedFixture(t *testing.T, entry string) (guestproto.SandboxSpec, string) {
	t.Helper()
	source, target := t.TempDir(), t.TempDir()
	var archive bytes.Buffer
	tw := tar.NewWriter(&archive)
	for _, h := range []*tar.Header{
		{Name: ".", Typeflag: tar.TypeDir, Mode: 0755, Uid: os.Geteuid(), Gid: os.Getegid()},
		{Name: entry, Typeflag: tar.TypeReg, Size: 4, Mode: 04500, Uid: os.Geteuid(), Gid: os.Getegid()},
	} {
		if err := tw.WriteHeader(h); err != nil {
			t.Fatal(err)
		}
		if h.Size > 0 {
			if _, err := tw.Write([]byte("abcd")); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "root.tar"), archive.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(archive.Bytes())
	return guestproto.SandboxSpec{ID: "seed", Build: true, BuildSeed: &guestproto.BuildSeedSpec{SourceTag: "input", TargetTag: "build", Bytes: int64(archive.Len()), Entries: 2, SHA256: hex.EncodeToString(sum[:])}, Shares: []guestproto.ShareSpec{{Tag: "input", Target: source, ReadOnly: true}, {Tag: "build", Target: target, FSType: "ext4", Source: "/dev/vda"}}}, target
}

func TestGuestSeedExtractionMetadataAndFreshness(t *testing.T) {
	spec, target := seedFixture(t, "file")
	if err := os.Mkdir(filepath.Join(target, "lost+found"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := populateBuildSeed(context.Background(), spec); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(target, "file"))
	if err != nil || string(data) != "abcd" {
		t.Fatal("seed payload", err)
	}
	info, err := os.Stat(filepath.Join(target, "file"))
	if err != nil || info.Mode().Perm() != 0500 || info.Mode()&os.ModeSetuid == 0 {
		t.Fatal("seed special mode lost", info, err)
	}
	if _, err := os.Stat(filepath.Join(target, "lost+found")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("formatter directory retained", err)
	}
	if err := populateBuildSeed(context.Background(), spec); err == nil {
		t.Fatal("non-fresh root accepted")
	}
	if data, err := os.ReadFile(filepath.Join(target, "file")); err != nil || string(data) != "abcd" {
		t.Fatal("preexisting data changed", err)
	}
}

func TestGuestSeedRejectsInvalidContextArchiveAndMountSelection(t *testing.T) {
	for _, scenario := range []string{"checksum", "count", "traversal", "whiteout", "writable-source", "wrong-device", "overlap", "nonempty-lost-found", "cancel"} {
		t.Run(scenario, func(t *testing.T) {
			name := "file"
			if scenario == "traversal" {
				name = "../escaped"
			}
			if scenario == "whiteout" {
				name = ".wh.file"
			}
			spec, target := seedFixture(t, name)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			switch scenario {
			case "checksum":
				spec.BuildSeed.SHA256 = strings.Repeat("0", 64)
			case "count":
				spec.BuildSeed.Entries++
			case "writable-source":
				spec.Shares[0].ReadOnly = false
			case "wrong-device":
				spec.Shares[1].Source = "/dev/vdb"
			case "overlap":
				spec.Shares[1].Target = spec.Shares[0].Target
			case "cancel":
				cancel()
			case "nonempty-lost-found":
				if err := os.Mkdir(filepath.Join(target, "lost+found"), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(target, "lost+found/keep"), []byte("keep"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if err := populateBuildSeed(ctx, spec); err == nil {
				t.Fatal("invalid seed accepted")
			}
			if scenario == "nonempty-lost-found" {
				if data, err := os.ReadFile(filepath.Join(target, "lost+found/keep")); err != nil || string(data) != "keep" {
					t.Fatal("preexisting contents deleted", err)
				}
			}
		})
	}
}
