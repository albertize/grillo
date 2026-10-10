//go:build linux

// SPDX-License-Identifier: Apache-2.0

package executor

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/albertize/grillo/internal/image"
	"github.com/albertize/grillo/internal/model"
	"github.com/albertize/grillo/internal/oci"
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
	// Aggregate rootfs cache policy; zero bytes selects 32 GiB.
	CacheQuota oci.CacheQuota
	// Store and CAS, when set, let locally built or previously pulled images
	// resolve without a registry round trip.
	Store *image.Store
	CAS   *oci.CAS
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
	if pulled, ok, err := r.local(image); err != nil {
		return Image{}, err
	} else if ok {
		return r.materialize(ctx, pulled)
	}
	pulled, err := r.Puller.Pull(ctx, ref)
	if err != nil {
		return Image{}, err
	}
	return r.materialize(ctx, pulled)
}

// local resolves an image recorded in the local image store.
func (r *OCIResolver) local(image model.ImageRef) (oci.PulledImage, bool, error) {
	if r.Store == nil || r.CAS == nil || image.Reference == "" {
		return oci.PulledImage{}, false, nil
	}
	record, ok, err := r.Store.Get(image.Reference)
	if err != nil || !ok {
		return oci.PulledImage{}, false, err
	}
	pulled, err := oci.LoadPulled(r.CAS, record.ManifestDigest)
	if err != nil {
		return oci.PulledImage{}, false, err
	}
	return pulled, true, nil
}

// materialize unpacks a resolved image into the content-addressed rootfs cache.
func (r *OCIResolver) materialize(ctx context.Context, pulled oci.PulledImage) (Image, error) {
	key := strings.NewReplacer(":", "-", "/", "-").Replace(pulled.ManifestDigest)
	// Never trust roots produced by the old writable-cache runtime.
	rootfs := filepath.Join(r.CacheDir, "immutable-v1-"+key)

	if _, err := os.Stat(filepath.Join(rootfs, ".grillo-complete")); err == nil {
		return Image{HostPath: rootfs, Version: pulled.ManifestDigest}, nil
	}
	quota := r.CacheQuota
	if quota.MaxBytes == 0 {
		quota.MaxBytes = 32 << 30
	}
	guard, err := oci.AcquireCacheGuard(ctx, r.CacheDir, quota)
	if err != nil {
		return Image{}, err
	}
	defer guard.Close()
	// A different resolver may have published while this caller waited.
	if _, err := os.Stat(filepath.Join(rootfs, ".grillo-complete")); err == nil {
		return Image{HostPath: rootfs, Version: pulled.ManifestDigest}, nil
	}
	available := guard.Quota.MaxBytes - guard.Bytes - int64(len(pulled.ManifestDigest))
	entries := guard.Quota.MaxEntries - guard.Entries - 2 // stage and completion marker
	if available <= 0 || entries <= 0 {
		return Image{}, oci.ErrCacheQuota
	}
	tmp, err := os.MkdirTemp(r.CacheDir, ".unpack-*")
	if err != nil {
		return Image{}, err
	}
	defer os.RemoveAll(tmp)
	if err := r.Puller.Unpack(pulled, tmp, oci.UnpackOptions{Context: ctx, MaxBytes: min(available, oci.DefaultMaxUnpackBytes), MaxFiles: min(entries, oci.DefaultMaxUnpackFiles)}); err != nil {
		return Image{}, fmt.Errorf("executor: unpack %s: %w", pulled.ManifestDigest, err)
	}
	// The staging directory is private while unpacking; the container root
	// itself must be searchable by non-root guest users after publication.
	if err := os.Chmod(tmp, 0o755); err != nil {
		return Image{}, err
	}
	if err := os.WriteFile(filepath.Join(tmp, ".grillo-complete"), []byte(pulled.ManifestDigest), 0o600); err != nil {
		return Image{}, err
	}
	if err := guard.Check(ctx); err != nil {
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
