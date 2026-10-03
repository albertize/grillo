// SPDX-License-Identifier: Apache-2.0

package oci

import (
	"strings"
	"testing"
)

const exampleDigest = "sha256:1e2c6e4f4a3b1f4e5d6c7b8a90a1b2c3d4e5f60718293a4b5c6d7e8f90123456"

func TestParseReference(t *testing.T) {
	cases := []struct {
		in   string
		want Reference
	}{
		{"busybox", Reference{Registry: "docker.io", Repository: "library/busybox", Tag: "latest"}},
		{"busybox:1.37", Reference{Registry: "docker.io", Repository: "library/busybox", Tag: "1.37"}},
		{"library/busybox", Reference{Registry: "docker.io", Repository: "library/busybox", Tag: "latest"}},
		{"ghcr.io/foo/bar", Reference{Registry: "ghcr.io", Repository: "foo/bar", Tag: "latest"}},
		{"localhost:5000/foo/bar:tag", Reference{Registry: "localhost:5000", Repository: "foo/bar", Tag: "tag"}},
		{"quay.io/org/team/img:v1", Reference{Registry: "quay.io", Repository: "org/team/img", Tag: "v1"}},
		{"busybox@" + exampleDigest, Reference{Registry: "docker.io", Repository: "library/busybox", Digest: exampleDigest}},
		{"docker.io/library/busybox:1.37@" + exampleDigest, Reference{Registry: "docker.io", Repository: "library/busybox", Tag: "1.37", Digest: exampleDigest}},
	}
	for _, tc := range cases {
		got, err := ParseReference(tc.in)
		if err != nil {
			t.Errorf("ParseReference(%q): %v", tc.in, err)
			continue
		}
		if got != tc.want {
			t.Errorf("ParseReference(%q) = %+v, want %+v", tc.in, got, tc.want)
		}
	}
}

func TestParseReferenceInvalid(t *testing.T) {
	bad := []string{
		"",
		"busybox:bad tag",
		"busybox@sha256:short",
		"UPPER/case",
		"busybox@md5:abc",
		"busybox\n",
	}
	for _, in := range bad {
		if _, err := ParseReference(in); err == nil {
			t.Errorf("ParseReference(%q) succeeded, want error", in)
		}
	}
}

func TestReferenceStringAndRef(t *testing.T) {
	ref, _ := ParseReference("ghcr.io/foo/bar:1.2")
	if got := ref.Name(); got != "ghcr.io/foo/bar" {
		t.Errorf("Name = %q", got)
	}
	if got := ref.ManifestRef(); got != "1.2" {
		t.Errorf("ManifestRef = %q", got)
	}
	if got := ref.String(); got != "ghcr.io/foo/bar:1.2" {
		t.Errorf("String = %q", got)
	}
	pinned, _ := ParseReference("ghcr.io/foo/bar@" + exampleDigest)
	if got := pinned.ManifestRef(); got != exampleDigest {
		t.Errorf("pinned ManifestRef = %q", got)
	}
	if got := pinned.String(); !strings.Contains(got, "@sha256:") {
		t.Errorf("pinned String = %q", got)
	}
}
