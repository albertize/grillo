// SPDX-License-Identifier: Apache-2.0

// Package oci implements the OCI Distribution API subset, content-addressed
// blob storage, and safe layer unpacking. It is host-side: it never executes
// image content and never imports the sandbox or guest packages.
package oci

import (
	"fmt"
	"regexp"
	"strings"
)

// DefaultRegistry is used when a reference has no explicit registry.
const DefaultRegistry = "docker.io"

// Reference is a normalized image reference.
type Reference struct {
	Registry   string
	Repository string
	Tag        string
	Digest     string // sha256:... when pinned by digest
}

var (
	digestRe = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)
	tagRe    = regexp.MustCompile(`^[a-zA-Z0-9_][a-zA-Z0-9._-]{0,127}$`)
	repoRe   = regexp.MustCompile(`^[a-z0-9]+(?:(?:[._]|__|[-]+)[a-z0-9]+)*(?:/[a-z0-9]+(?:(?:[._]|__|[-]+)[a-z0-9]+)*)*$`)
)

// ParseReference normalizes an image reference, applying Docker's default
// registry and library namespace. A missing tag becomes "latest".
func ParseReference(input string) (Reference, error) {
	if input == "" {
		return Reference{}, fmt.Errorf("oci: empty reference")
	}
	if strings.ContainsAny(input, " \t\n\r") {
		return Reference{}, fmt.Errorf("oci: reference contains whitespace")
	}
	ref := Reference{}
	rest := input
	if at := strings.LastIndexByte(rest, '@'); at >= 0 {
		ref.Digest = rest[at+1:]
		rest = rest[:at]
		if !digestRe.MatchString(ref.Digest) {
			return Reference{}, fmt.Errorf("oci: invalid digest %q", ref.Digest)
		}
	}
	// Split registry from repository: the first path element is a registry when
	// it contains a '.' or ':' or is exactly "localhost".
	first, remainder, hasSlash := strings.Cut(rest, "/")
	if hasSlash && (strings.ContainsAny(first, ".:") || first == "localhost") {
		ref.Registry = first
		rest = remainder
	} else {
		ref.Registry = DefaultRegistry
	}
	// Split tag from repository.
	if colon := strings.LastIndexByte(rest, ':'); colon >= 0 {
		if slash := strings.LastIndexByte(rest, '/'); colon > slash {
			ref.Tag = rest[colon+1:]
			rest = rest[:colon]
			if !tagRe.MatchString(ref.Tag) {
				return Reference{}, fmt.Errorf("oci: invalid tag %q", ref.Tag)
			}
		}
	}
	if ref.Registry == DefaultRegistry && !strings.Contains(rest, "/") {
		rest = "library/" + rest
	}
	if !repoRe.MatchString(rest) {
		return Reference{}, fmt.Errorf("oci: invalid repository %q", rest)
	}
	ref.Repository = rest
	if ref.Tag == "" && ref.Digest == "" {
		ref.Tag = "latest"
	}
	return ref, nil
}

// Name returns registry/repository.
func (r Reference) Name() string { return r.Registry + "/" + r.Repository }

// ManifestRef returns the tag when set, otherwise the digest.
func (r Reference) ManifestRef() string {
	if r.Tag != "" {
		return r.Tag
	}
	return r.Digest
}

// String renders the canonical reference.
func (r Reference) String() string {
	out := r.Name()
	if r.Tag != "" {
		out += ":" + r.Tag
	}
	if r.Digest != "" {
		out += "@" + r.Digest
	}
	return out
}

// RegistryHost returns the host:port for HTTP requests.
func (r Reference) RegistryHost() string {
	if r.Registry == DefaultRegistry {
		return "registry-1.docker.io"
	}
	return r.Registry
}
