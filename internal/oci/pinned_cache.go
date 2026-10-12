// SPDX-License-Identifier: Apache-2.0

package oci

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
)

// manifest reuses only explicit digest pins. Mutable tags still require the
// registry; unavailable/corrupt local content is never disguised as fresh data.
func (p *Puller) manifest(ctx context.Context, ref Reference) ([]byte, string, error) {
	if err := ctx.Err(); err != nil {
		return nil, "", err
	}
	if ref.Digest != "" {
		f, err := p.CAS.Open(ref.Digest)
		if err == nil {
			defer f.Close()
			data, err := readLimited(f, DefaultMaxManifestBytes)
			if err != nil {
				return nil, "", err
			}
			if "sha256:"+sha256Hex(data) != ref.Digest {
				return nil, "", ErrBlobCorrupt
			}
			var meta struct {
				MediaType string `json:"mediaType"`
			}
			if err := json.Unmarshal(data, &meta); err != nil {
				return nil, "", err
			}
			return data, meta.MediaType, nil
		}
		if !errors.Is(err, ErrNotFound) {
			return nil, "", err
		}
	}
	if p.Registry == nil {
		return nil, "", fmt.Errorf("%w: offline pinned manifest missing", ErrNotFound)
	}
	data, media, err := p.Registry.FetchManifest(ctx, ref)
	if err != nil {
		return nil, "", err
	}
	if ref.Digest != "" && "sha256:"+sha256Hex(data) != ref.Digest {
		return nil, "", ErrDigestMismatch
	}
	return data, media, nil
}

// verifyBlob checks descriptor size and bytes on every cached pinned pull.
// This is local integrity, not publisher attestation. Regular disk I/O still
// depends on the host kernel; context is checked between bounded chunks.
func (p *Puller) verifyBlob(ctx context.Context, digest string, size int64) error {
	f, err := p.CAS.Open(digest)
	if err != nil {
		return err
	}
	defer f.Close()
	hash := sha256.New()
	reader := &quotaReader{reader: f, ctx: ctx, remaining: p.maxBlobBytes()}
	n, err := io.Copy(hash, reader)
	if err != nil {
		return err
	}
	if size >= 0 && n != size {
		return ErrSizeMismatch
	}
	if "sha256:"+hex.EncodeToString(hash.Sum(nil)) != digest {
		return ErrBlobCorrupt
	}
	return nil
}

// CopyPinned copies just the verified platform graph, including the index that
// proves the original requested pin. It never reads unrelated files/credentials
// from a source CAS and never contacts a registry. Destination publication uses
// the ordinary quota/atomic CAS contract.
func CopyPinned(ctx context.Context, source, destination *CAS, ref Reference, platform Platform) (PulledImage, error) {
	if ref.Digest == "" || source == nil || destination == nil {
		return PulledImage{}, fmt.Errorf("oci: fixture transfer requires an explicit digest and both CAS stores")
	}
	p := &Puller{CAS: source, Platform: platform}
	image, err := p.Pull(ctx, ref)
	if err != nil {
		return PulledImage{}, err
	}
	digests := []string{ref.Digest, image.ManifestDigest, image.Manifest.Config.Digest}
	for _, layer := range image.Manifest.Layers {
		digests = append(digests, layer.Digest)
	}
	seen := map[string]bool{}
	for _, digest := range digests {
		if seen[digest] {
			continue
		}
		seen[digest] = true
		f, err := source.Open(digest)
		if err != nil {
			return PulledImage{}, err
		}
		info, err := f.(*os.File).Stat()
		if err == nil && !info.Mode().IsRegular() {
			err = fmt.Errorf("oci: fixture blob is not regular")
		}
		if err == nil {
			err = destination.commitContext(ctx, digest, f, info.Size(), DefaultMaxBlobBytes)
		}
		closeErr := f.Close()
		if err != nil {
			return PulledImage{}, err
		}
		if closeErr != nil {
			return PulledImage{}, closeErr
		}
	}
	return image, nil
}
