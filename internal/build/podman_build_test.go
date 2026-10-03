//go:build builder

// SPDX-License-Identifier: Apache-2.0

// Real rootless Podman builds. Run with: make test-builder

package build

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"grillo.local/grillo/internal/image"
	"grillo.local/grillo/internal/oci"
)

func newBuilder(t *testing.T) (*PodmanBuilder, *oci.CAS, *image.Store) {
	t.Helper()
	cas, err := oci.OpenCAS(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	store, err := image.Open(filepath.Join(t.TempDir(), "images"), cas)
	if err != nil {
		t.Fatal(err)
	}
	builder := &PodmanBuilder{
		CAS:      cas,
		Store:    store,
		TempDir:  t.TempDir(),
		Platform: oci.Platform{OS: "linux", Architecture: "amd64"},
	}
	return builder, cas, store
}

func requirePodman(t *testing.T, builder *PodmanBuilder) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := builder.Available(ctx); err != nil {
		t.Skipf("SKIP: %v", err)
	}
}

func TestPodmanScratchBuildAndDockerignore(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	builder, cas, store := newBuilder(t)
	requirePodman(t, builder)

	contextDir := t.TempDir()
	write := func(name, content string) {
		if err := os.WriteFile(filepath.Join(contextDir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("hello.txt", "grillo-build-ok")
	write("ignored.txt", "should-not-appear")
	// Grillo never parses this file; the builder must honor it.
	write(".dockerignore", "ignored.txt\n")
	write("Containerfile", "FROM scratch\nCOPY . /ctx/\n")
	if err := os.MkdirAll(filepath.Join(contextDir, "build-only"), 0o755); err != nil {
		t.Fatal(err)
	}

	var output []string
	result, err := builder.Build(ctx, Request{
		ContextDir: contextDir,
		Dockerfile: "Containerfile",
		Reference:  "grillo.local/scratch:latest",
		Network:    "none",
		Labels:     map[string]string{"org.grillotest": "1"},
	}, func(line string) { output = append(output, line) })
	if err != nil {
		t.Fatalf("build failed: %v\n%s", err, strings.Join(output, "\n"))
	}
	if result.ManifestDigest == "" || result.Reference != "grillo.local/scratch:latest" {
		t.Fatalf("result = %+v", result)
	}

	record, ok, err := store.Get("grillo.local/scratch:latest")
	if err != nil || !ok {
		t.Fatalf("record = %+v ok=%v err=%v", record, ok, err)
	}
	if record.Source != image.SourceBuild || record.Labels["org.grillotest"] != "1" || record.LayerCount == 0 {
		t.Fatalf("record = %+v", record)
	}
	if verified, err := store.Verify(record.Reference); err != nil || !verified {
		t.Fatalf("verify = %v %v", verified, err)
	}

	puller := &oci.Puller{CAS: cas}
	root := t.TempDir()
	if err := puller.Unpack(result.Image, root, oci.UnpackOptions{}); err != nil {
		t.Fatalf("unpack: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(root, "ctx", "hello.txt"))
	if err != nil || string(data) != "grillo-build-ok" {
		t.Fatalf("hello.txt = %q err=%v", data, err)
	}
	if _, err := os.Stat(filepath.Join(root, "ctx", "ignored.txt")); err == nil {
		t.Fatal("the builder did not honor .dockerignore")
	}

	// A repeated build re-records the reference. Image configs embed a creation
	// timestamp, so the manifest digest is not expected to be stable.
	second, err := builder.Build(ctx, Request{
		ContextDir: contextDir,
		Dockerfile: "Containerfile",
		Reference:  "grillo.local/scratch:latest",
		Network:    "none",
		Labels:     map[string]string{"org.grillotest": "1"},
	}, nil)
	if err != nil {
		t.Fatalf("second build failed: %v", err)
	}
	if second.ManifestDigest == "" || second.Reference != result.Reference {
		t.Fatalf("second build = %+v", second)
	}
}

func TestPodmanBuildCancellation(t *testing.T) {
	builder, _, _ := newBuilder(t)
	requirePodman(t, builder)
	base := firstLocalImage(t)
	if base == "" {
		t.Skip("SKIP: no local base image to build a cancelable RUN step")
	}
	contextDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(contextDir, "Containerfile"), []byte("FROM "+base+"\nRUN sleep 30\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	_, err := builder.Build(ctx, Request{
		ContextDir: contextDir,
		Dockerfile: "Containerfile",
		Reference:  "grillo.local/cancel:latest",
		Network:    "none",
	}, nil)
	if err == nil {
		t.Fatal("a canceled build reported success")
	}
	if !strings.Contains(err.Error(), "cancel") && ctx.Err() == nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func firstLocalImage(t *testing.T) string {
	t.Helper()
	out, err := exec.Command("podman", "images", "--format", "{{.Repository}}:{{.Tag}}").Output()
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.Contains(line, "<none>") {
			continue
		}
		return line
	}
	return ""
}
