// SPDX-License-Identifier: Apache-2.0

package oci

import (
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
)

// Puller resolves a reference to a verified image and can unpack it.
type Puller struct {
	CAS          *CAS
	Registry     *RegistryClient
	Platform     Platform
	MaxBlobBytes int64
}

// PulledImage is a resolved, verified image.
type PulledImage struct {
	Reference      Reference
	Manifest       Manifest
	ManifestDigest string
	Config         ImageConfig
}

func (p *Puller) platform() Platform {
	if p.Platform.OS == "" {
		return Platform{OS: "linux", Architecture: "amd64"}
	}
	return p.Platform
}

func (p *Puller) maxBlobBytes() int64 {
	if p.MaxBlobBytes <= 0 {
		return DefaultMaxBlobBytes
	}
	return p.MaxBlobBytes
}

// Pull resolves ref (following an index to the target platform), fetches the
// manifest, config, and layers into the CAS, and verifies every digest and size.
func (p *Puller) Pull(ctx context.Context, ref Reference) (PulledImage, error) {
	if p == nil || p.CAS == nil {
		return PulledImage{}, fmt.Errorf("oci: pull requires a CAS")
	}
	data, mediaType, err := p.manifest(ctx, ref)
	if err != nil {
		return PulledImage{}, err
	}
	if IsIndex(mediaType) || mediaType == "" {
		index, err := ParseIndex(data)
		if err != nil {
			return PulledImage{}, err
		}
		descriptor, err := SelectPlatform(index, p.platform())
		if err != nil {
			return PulledImage{}, err
		}
		child := ref
		child.Tag = ""
		child.Digest = descriptor.Digest
		// Retain the verified index too: it binds the requested pin to the
		// selected platform manifest during subsequent offline resolution.
		if err := p.CAS.PutBytes("sha256:"+sha256Hex(data), data); err != nil {
			return PulledImage{}, err
		}
		data, _, err = p.manifest(ctx, child)
		if err != nil {
			return PulledImage{}, err
		}
		if int64(len(data)) != descriptor.Size {
			return PulledImage{}, ErrSizeMismatch
		}
		ref = child
	}
	manifestDigest := "sha256:" + sha256Hex(data)
	if ref.Digest != "" && ref.Digest != manifestDigest {
		return PulledImage{}, fmt.Errorf("%w: manifest %s, want %s", ErrDigestMismatch, manifestDigest, ref.Digest)
	}
	manifest, err := ParseManifest(data)
	if err != nil {
		return PulledImage{}, err
	}
	if err := p.CAS.PutBytes(manifestDigest, data); err != nil {
		return PulledImage{}, err
	}
	if manifest.Config.Size > DefaultMaxManifestBytes {
		return PulledImage{}, fmt.Errorf("oci: config exceeds metadata byte limit")
	}
	if err := p.fetchBlob(ctx, ref, manifest.Config.Digest, manifest.Config.Size); err != nil {
		return PulledImage{}, fmt.Errorf("oci: config %s: %w", manifest.Config.Digest, err)
	}
	configData, err := p.readBlob(manifest.Config.Digest)
	if err != nil {
		return PulledImage{}, err
	}
	config, err := ParseImageConfig(configData)
	if err != nil {
		return PulledImage{}, err
	}
	for i, layer := range manifest.Layers {
		if err := p.fetchBlob(ctx, ref, layer.Digest, layer.Size); err != nil {
			return PulledImage{}, fmt.Errorf("oci: layer %d (%s): %w", i, layer.Digest, err)
		}
	}
	return PulledImage{
		Reference:      ref,
		Manifest:       manifest,
		ManifestDigest: manifestDigest,
		Config:         config,
	}, nil
}

// Unpack applies the image layers to root in order, verifying diff_ids when the
// image config provides them.
func (p *Puller) Unpack(image PulledImage, root string, opts UnpackOptions) error {
	budget, err := newUnpackBudget(opts)
	if err != nil {
		return err
	}
	for i, layer := range image.Manifest.Layers {
		if err := budget.opts.Context.Err(); err != nil {
			return err
		}
		if i < len(image.Config.RootFS.DiffIDs) {
			got, err := p.layerDiffID(layer, budget)
			if err != nil {
				return err
			}
			if got != image.Config.RootFS.DiffIDs[i] {
				return fmt.Errorf("%w: layer %d diff_id %s, want %s", ErrDigestMismatch, i, got, image.Config.RootFS.DiffIDs[i])
			}
		}
		f, err := p.CAS.Open(layer.Digest)
		if err != nil {
			return err
		}
		unpackErr := unpackLayer(root, f, IsGzipLayer(layer.MediaType), budget)
		closeErr := f.Close()
		if unpackErr != nil {
			return fmt.Errorf("oci: unpack layer %d: %w", i, unpackErr)
		}
		if closeErr != nil {
			return closeErr
		}
	}
	return nil
}

func (p *Puller) layerDiffID(layer Descriptor, budget *unpackBudget) (string, error) {
	f, err := p.CAS.Open(layer.Digest)
	if err != nil {
		return "", err
	}
	defer f.Close()
	bounded := func(reader io.Reader) (string, error) {
		return DiffID(&quotaReader{reader: reader, ctx: budget.opts.Context, remaining: budget.opts.MaxArchiveBytes - budget.archive})
	}
	if !IsGzipLayer(layer.MediaType) {
		return bounded(f)
	}
	gz, err := gzip.NewReader(f)
	if err != nil {
		return "", err
	}
	defer gz.Close()
	return bounded(gz)
}

func (p *Puller) fetchBlob(ctx context.Context, ref Reference, digest string, size int64) error {
	if p.CAS.Has(digest) {
		return p.verifyBlob(ctx, digest, size)
	}
	return p.CAS.Fetch(ctx, digest, func(ctx context.Context) (io.ReadCloser, int64, error) {
		if p.Registry == nil {
			return nil, 0, fmt.Errorf("%w: offline image is incomplete", ErrNotFound)
		}
		body, length, err := p.Registry.OpenBlob(ctx, ref, digest)
		if err != nil {
			return nil, 0, err
		}
		if length >= 0 && size >= 0 && length != size {
			_ = body.Close()
			return nil, 0, fmt.Errorf("%w: registry reports %d bytes, manifest says %d", ErrSizeMismatch, length, size)
		}
		return body, size, nil
	}, p.maxBlobBytes())
}

func (p *Puller) readBlob(digest string) ([]byte, error) {
	f, err := p.CAS.Open(digest)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return readLimited(f, DefaultMaxManifestBytes)
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
