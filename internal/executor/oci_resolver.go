//go:build linux

// SPDX-License-Identifier: Apache-2.0

package executor

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"grillo.local/grillo/internal/model"
	"grillo.local/grillo/internal/oci"
)

// ImagePuller resolves and unpacks images. *oci.Puller implements it.
type ImagePuller interface {
	Pull(ctx context.Context, ref oci.Reference) (oci.PulledImage, error)
	Unpack(image oci.PulledImage, root string, opts oci.UnpackOptions) error
}

// OCIResolver resolves images by pulling them through internal/oci and unpacking
// into a content-addressed cache directory. Repeated resolution of the same
// manifest reuses the unpacked rootfs.
type OCIResolver struct {
	Puller   ImagePuller
	CacheDir string
	Platform oci.Platform

	mu    sync.Mutex
	locks map[string]*sync.Mutex
}

func (r *OCIResolver) lockFor(key string) *sync.Mutex {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.locks == nil {
		r.locks = map[string]*sync.Mutex{}
	}
	lock, ok := r.locks[key]
	if !ok {
		lock = &sync.Mutex{}
		r.locks[key] = lock
	}
	return lock
}

// Resolve implements ImageResolver.
func (r *OCIResolver) Resolve(ctx context.Context, image model.ImageRef) (Image, error) {
	if r.Puller == nil {
		return Image{}, fmt.Errorf("executor: OCI resolver has no puller")
	}
	ref, err := oci.ParseReference(image.Reference)
	if err != nil {
		return Image{}, err
	}
	if image.Digest != "" {
		ref.Digest = image.Digest
		ref.Tag = ""
	}
	if err := os.MkdirAll(r.CacheDir, 0o700); err != nil {
		return Image{}, err
	}
	pulled, err := r.Puller.Pull(ctx, ref)
	if err != nil {
		return Image{}, err
	}
	key := strings.NewReplacer(":", "-", "/", "-").Replace(pulled.ManifestDigest)
	rootfs := filepath.Join(r.CacheDir, key)

	lock := r.lockFor(key)
	lock.Lock()
	defer lock.Unlock()

	if _, err := os.Stat(filepath.Join(rootfs, ".grillo-complete")); err == nil {
		return Image{HostPath: rootfs, Version: pulled.ManifestDigest}, nil
	}
	tmp, err := os.MkdirTemp(r.CacheDir, ".unpack-*")
	if err != nil {
		return Image{}, err
	}
	defer os.RemoveAll(tmp)
	if err := r.Puller.Unpack(pulled, tmp, oci.UnpackOptions{}); err != nil {
		return Image{}, fmt.Errorf("executor: unpack %s: %w", pulled.ManifestDigest, err)
	}
	if err := os.WriteFile(filepath.Join(tmp, ".grillo-complete"), []byte(pulled.ManifestDigest), 0o600); err != nil {
		return Image{}, err
	}
	if err := os.Rename(tmp, rootfs); err != nil {
		// Another resolver may have completed the same image first.
		if _, statErr := os.Stat(filepath.Join(rootfs, ".grillo-complete")); statErr == nil {
			return Image{HostPath: rootfs, Version: pulled.ManifestDigest}, nil
		}
		return Image{}, err
	}
	return Image{HostPath: rootfs, Version: pulled.ManifestDigest}, nil
}
