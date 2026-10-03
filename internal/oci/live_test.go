//go:build netreg

// SPDX-License-Identifier: Apache-2.0

package oci

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestLivePullPinnedImage pulls a digest-pinned image from a real registry,
// verifies the CAS contents, and unpacks it. It requires network access; a
// network failure is a documented SKIP, not a pass.
func TestLivePullPinnedImage(t *testing.T) {
	const pinned = "docker.io/library/busybox:1.37@sha256:bdf57e528e45e4433820e045b29b4597825a1c9e38353532d90a01445013f82e"
	ref, err := ParseReference(pinned)
	if err != nil {
		t.Fatal(err)
	}
	cas, err := OpenCAS(filepath.Join(t.TempDir(), "cas"))
	if err != nil {
		t.Fatal(err)
	}
	puller := &Puller{CAS: cas, Registry: NewRegistryClient(), Platform: Platform{OS: "linux", Architecture: "amd64"}}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	pulled, err := puller.Pull(ctx, ref)
	if err != nil {
		t.Skipf("SKIP: live registry unavailable: %v", err)
	}
	// The pinned digest is the index; the pull selects and verifies the
	// linux/amd64 manifest, so only check that it resolved.
	if !strings.HasPrefix(pulled.ManifestDigest, "sha256:") {
		t.Fatalf("manifest digest = %q", pulled.ManifestDigest)
	}
	if len(pulled.Manifest.Layers) == 0 || !cas.Has(pulled.Manifest.Layers[0].Digest) {
		t.Fatal("layer blob is not in the CAS")
	}
	root := t.TempDir()
	if err := puller.Unpack(pulled, root, UnpackOptions{}); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(filepath.Join(root, "bin"))
	if err != nil {
		t.Fatalf("unpacked rootfs has no /bin: %v", err)
	}
	found := false
	for _, entry := range entries {
		if strings.Contains(entry.Name(), "busybox") || entry.Name() == "sh" {
			found = true
		}
	}
	if !found {
		t.Fatalf("/bin does not contain the expected busybox content: %v", entries)
	}
}
