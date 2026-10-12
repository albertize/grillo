// SPDX-License-Identifier: Apache-2.0

// Explicit one-time live provisioning of the digest-pinned installed-test image.
// No alternate registry, inferred credentials or arbitrary helper is used.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"golang.org/x/sys/unix"
	"os"
	"path/filepath"
	"time"

	"github.com/albertize/grillo/internal/oci"
)

func main() {
	out := flag.String("out", "", "new private fixture directory (must not exist)")
	flag.Parse()
	if err := run(*out); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run(out string) error {
	if out == "" {
		return fmt.Errorf("explicit -out required; this command contacts Docker Hub")
	}
	out, err := filepath.Abs(out)
	if err != nil {
		return err
	}
	if _, err := os.Lstat(out); err == nil {
		return fmt.Errorf("fixture output already exists")
	} else if !os.IsNotExist(err) {
		return err
	}
	if len(out) > 4096 {
		return fmt.Errorf("fixture path bound exceeded")
	}
	parent := filepath.Dir(out)
	for ancestor := parent; ; ancestor = filepath.Dir(ancestor) {
		info, err := os.Lstat(ancestor)
		if err == nil && (!info.IsDir() || info.Mode()&os.ModeSymlink != 0) {
			return fmt.Errorf("fixture parent must not follow symlinks or non-directories")
		}
		if err != nil && !os.IsNotExist(err) {
			return err
		}
		if ancestor == filepath.Dir(ancestor) {
			break
		}
	}
	if err := os.MkdirAll(parent, 0700); err != nil {
		return err
	}
	stage, err := os.MkdirTemp(parent, ".t24-image-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	cas, err := oci.OpenCAS(filepath.Join(stage, "cas"))
	if err != nil {
		return err
	}
	const reference = "docker.io/library/busybox:1.37@sha256:bdf57e528e45e4433820e045b29b4597825a1c9e38353532d90a01445013f82e"
	ref, err := oci.ParseReference(reference)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	image, err := (&oci.Puller{CAS: cas, Registry: oci.NewRegistryClient()}).Pull(ctx, ref)
	if err != nil {
		return err
	}
	receipt, err := json.MarshalIndent(map[string]any{"reference": reference, "platform": "linux/amd64", "manifest_digest": image.ManifestDigest, "source": "Docker Hub live OCI Distribution API", "publisher_authentication": "not established by content digests", "provisioned_at": time.Now().UTC()}, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(stage, "receipt.json"), append(receipt, '\n'), 0600); err != nil {
		return err
	}
	// Same-parent atomic publication; never replace an existing output.
	if _, err := os.Lstat(out); err == nil {
		return fmt.Errorf("fixture destination appeared during provision")
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := unix.Renameat2(unix.AT_FDCWD, stage, unix.AT_FDCWD, out, unix.RENAME_NOREPLACE); err != nil {
		return err
	}
	fmt.Println(out)
	return nil
}
