//go:build linux

// SPDX-License-Identifier: Apache-2.0

package build

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/albertize/grillo/internal/image"
	"github.com/albertize/grillo/internal/oci"
)

func TestRequestValidate(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "file")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name string
		req  Request
		want error
	}{
		{"valid", Request{ContextDir: dir, Reference: "example/app:latest"}, nil},
		{"tags-only", Request{ContextDir: dir, Tags: []string{"example/app:latest"}}, nil},
		{"empty-context", Request{Reference: "x"}, ErrNoContext},
		{"missing-context", Request{ContextDir: filepath.Join(dir, "absent"), Reference: "x"}, ErrNoContext},
		{"context-is-file", Request{ContextDir: file, Reference: "x"}, ErrNoContext},
		{"no-reference", Request{ContextDir: dir}, ErrNoReference},
	}
	for _, tc := range cases {
		err := tc.req.Validate()
		if tc.want == nil {
			if err != nil {
				t.Errorf("%s: unexpected error %v", tc.name, err)
			}
			continue
		}
		if !errors.Is(err, tc.want) {
			t.Errorf("%s: got %v want %v", tc.name, err, tc.want)
		}
	}
}

func TestPodmanBuilderMissingToolIsActionable(t *testing.T) {
	builder := &PodmanBuilder{Binary: "grillo-nonexistent-builder-xyz"}
	err := builder.Available(context.Background())
	if !errors.Is(err, ErrToolMissing) {
		t.Fatalf("Available error = %v", err)
	}
	if !strings.Contains(err.Error(), "PATH") {
		t.Fatalf("error is not actionable: %v", err)
	}
}

func TestPodmanBuilderRequiresStoreAndCAS(t *testing.T) {
	dir := t.TempDir()
	if _, err := (&PodmanBuilder{}).Build(context.Background(), Request{ContextDir: dir, Reference: "x"}, nil); err == nil {
		t.Fatal("Build without a store succeeded")
	}
	cas, err := oci.OpenCAS(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := (&PodmanBuilder{CAS: cas}).Build(context.Background(), Request{ContextDir: dir, Reference: "x"}, nil); err == nil {
		t.Fatal("Build without a store succeeded")
	}
	store, err := image.Open(filepath.Join(t.TempDir(), "images"), cas)
	if err != nil {
		t.Fatal(err)
	}
	// The reference is validated before the builder is invoked.
	if _, err := (&PodmanBuilder{CAS: cas, Store: store, Binary: "grillo-nonexistent-builder-xyz"}).Build(context.Background(), Request{ContextDir: dir}, nil); !errors.Is(err, ErrNoReference) {
		t.Fatalf("Build error = %v", err)
	}
}
