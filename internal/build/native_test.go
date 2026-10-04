//go:build linux

// SPDX-License-Identifier: Apache-2.0

package build

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/albertize/grillo/internal/image"
	"github.com/albertize/grillo/internal/oci"
)

// testRunner is a host-side test double for a sandbox Runner. It runs the
// command in the build root without isolation, which is acceptable only in
// tests. Production RUN steps use the sandboxed runner.
type testRunner struct {
	calls []RunStep
}

func (r *testRunner) Run(ctx context.Context, step RunStep, progress Progress) error {
	r.calls = append(r.calls, step)
	if len(step.Command) == 0 {
		return nil
	}
	dir := step.RootfsDir
	if step.WorkDir != "" {
		dir = filepath.Join(step.RootfsDir, strings.TrimPrefix(filepath.ToSlash(step.WorkDir), "/"))
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	command := exec.CommandContext(ctx, step.Command[0], step.Command[1:]...)
	command.Dir = dir
	command.Env = append(os.Environ(), step.Env...)
	output, err := command.CombinedOutput()
	if progress != nil && len(output) > 0 {
		progress(strings.TrimSpace(string(output)))
	}
	if err != nil {
		return fmt.Errorf("test runner: %w: %s", err, output)
	}
	return nil
}

func (r *testRunner) Close() error { return nil }

func newNativeBuilder(t *testing.T, runner Runner) (*NativeBuilder, *oci.CAS, *oci.Puller) {
	t.Helper()
	cas, err := oci.OpenCAS(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	store, err := image.Open(filepath.Join(t.TempDir(), "images"), cas)
	if err != nil {
		t.Fatal(err)
	}
	puller := &oci.Puller{CAS: cas}
	return &NativeBuilder{
		CAS:        cas,
		Store:      store,
		Images:     puller,
		Runner:     runner,
		ScratchDir: t.TempDir(),
		Platform:   oci.Platform{OS: "linux", Architecture: "amd64"},
		Now:        func() time.Time { return time.Unix(0, 0).UTC() },
	}, cas, puller
}

func writeContext(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func unpackImage(t *testing.T, puller *oci.Puller, pulled oci.PulledImage) string {
	t.Helper()
	root := t.TempDir()
	if err := puller.Unpack(pulled, root, oci.UnpackOptions{}); err != nil {
		t.Fatal(err)
	}
	return root
}

func readFile(t *testing.T, root, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(name)))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return string(data)
}

func TestNativeCopyOnlyBuild(t *testing.T) {
	builder, _, puller := newNativeBuilder(t, nil)
	contextDir := writeContext(t, map[string]string{
		"Dockerfile": `FROM scratch
COPY hello.txt /hello.txt
COPY sub/ /sub/
COPY ignored.txt /ignored.txt
ENV GREETING=hi
WORKDIR /app
CMD ["echo", "done"]
`,
		"hello.txt":     "grillo-native-ok",
		"ignored.txt":   "should-not-appear",
		"sub/a.txt":     "a",
		"sub/b.txt":     "b",
		".dockerignore": "ignored.txt\n",
	})
	result, err := builder.Build(context.Background(), Request{
		ContextDir: contextDir,
		Reference:  "grillo.local/native-copy:latest",
		Network:    "none",
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.ManifestDigest == "" || result.Record.Source != image.SourceBuild {
		t.Fatalf("result = %+v", result)
	}
	root := unpackImage(t, puller, result.Image)
	if got := readFile(t, root, "hello.txt"); got != "grillo-native-ok" {
		t.Fatalf("hello.txt = %q", got)
	}
	if got := readFile(t, root, "sub/a.txt"); got != "a" {
		t.Fatalf("sub/a.txt = %q", got)
	}
	if _, err := os.Stat(filepath.Join(root, "ignored.txt")); err == nil {
		t.Fatal(".dockerignore was not honored")
	}
	if result.Image.Config.Config.WorkingDir != "/app" {
		t.Fatalf("workdir = %q", result.Image.Config.Config.WorkingDir)
	}
	if len(result.Image.Config.Config.Cmd) != 2 || result.Image.Config.Config.Cmd[0] != "echo" {
		t.Fatalf("cmd = %v", result.Image.Config.Config.Cmd)
	}
	found := false
	for _, entry := range result.Image.Config.Config.Env {
		if entry == "GREETING=hi" {
			found = true
		}
	}
	if !found {
		t.Fatalf("env = %v", result.Image.Config.Config.Env)
	}
}

func TestNativeRunBuild(t *testing.T) {
	runner := &testRunner{}
	builder, _, puller := newNativeBuilder(t, runner)
	contextDir := writeContext(t, map[string]string{
		"Dockerfile": `FROM scratch
WORKDIR /work
RUN sh -c "echo built > out.txt"
RUN ["sh", "-c", "echo more >> out.txt"]
COPY extra.txt /work/extra.txt
`,
		"extra.txt": "extra",
	})
	result, err := builder.Build(context.Background(), Request{
		ContextDir: contextDir,
		Reference:  "grillo.local/native-run:latest",
		Network:    "none",
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(runner.calls) != 2 {
		t.Fatalf("runner calls = %d", len(runner.calls))
	}
	if runner.calls[0].WorkDir != "/work" || !runner.calls[0].Shell {
		t.Fatalf("first run step = %+v", runner.calls[0])
	}
	root := unpackImage(t, puller, result.Image)
	if got := readFile(t, root, "work/out.txt"); !strings.Contains(got, "built") || !strings.Contains(got, "more") {
		t.Fatalf("out.txt = %q", got)
	}
	if got := readFile(t, root, "work/extra.txt"); got != "extra" {
		t.Fatalf("extra.txt = %q", got)
	}
}

func TestNativeRunRequiresRunner(t *testing.T) {
	builder, _, _ := newNativeBuilder(t, nil)
	contextDir := writeContext(t, map[string]string{"Dockerfile": "FROM scratch\nRUN echo hi\n"})
	_, err := builder.Build(context.Background(), Request{ContextDir: contextDir, Reference: "x:1"}, nil)
	if err == nil || !strings.Contains(err.Error(), "runner") {
		t.Fatalf("error = %v", err)
	}
}

func TestNativeUnsupportedInstruction(t *testing.T) {
	builder, _, _ := newNativeBuilder(t, nil)
	contextDir := writeContext(t, map[string]string{"Dockerfile": "FROM scratch\nHEALTHCHECK CMD true\n"})
	_, err := builder.Build(context.Background(), Request{ContextDir: contextDir, Reference: "x:1"}, nil)
	if err == nil || !strings.Contains(err.Error(), "unsupported") {
		t.Fatalf("error = %v", err)
	}
}

func TestNativeMultiStageAndWhiteout(t *testing.T) {
	runner := &testRunner{}
	builder, _, puller := newNativeBuilder(t, runner)
	contextDir := writeContext(t, map[string]string{
		"Dockerfile": `FROM scratch AS base
COPY keep.txt /keep.txt
COPY remove.txt /remove.txt
FROM base
RUN sh -c "rm remove.txt"
COPY --from=base /keep.txt /copied.txt
`,
		"keep.txt":   "keep",
		"remove.txt": "remove",
	})
	result, err := builder.Build(context.Background(), Request{
		ContextDir: contextDir,
		Reference:  "grillo.local/native-multi:latest",
		Network:    "none",
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	root := unpackImage(t, puller, result.Image)
	if got := readFile(t, root, "keep.txt"); got != "keep" {
		t.Fatalf("keep.txt = %q", got)
	}
	if got := readFile(t, root, "copied.txt"); got != "keep" {
		t.Fatalf("copied.txt = %q", got)
	}
	if _, err := os.Stat(filepath.Join(root, "remove.txt")); err == nil {
		t.Fatal("whiteout did not remove remove.txt")
	}
}

func TestNativeBuildFromLocalBase(t *testing.T) {
	builder, _, puller := newNativeBuilder(t, &testRunner{})
	baseContext := writeContext(t, map[string]string{
		"Dockerfile": "FROM scratch\nCOPY base.txt /base.txt\n",
		"base.txt":   "base",
	})
	if _, err := builder.Build(context.Background(), Request{ContextDir: baseContext, Reference: "grillo.local/base:1"}, nil); err != nil {
		t.Fatal(err)
	}
	derived := writeContext(t, map[string]string{"Dockerfile": "FROM grillo.local/base:1\nCOPY extra.txt /extra.txt\n", "extra.txt": "extra"})
	result, err := builder.Build(context.Background(), Request{ContextDir: derived, Reference: "grillo.local/derived:1"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Image.Manifest.Layers) != 2 {
		t.Fatalf("layers = %d, want 2", len(result.Image.Manifest.Layers))
	}
	root := unpackImage(t, puller, result.Image)
	if readFile(t, root, "base.txt") != "base" || readFile(t, root, "extra.txt") != "extra" {
		t.Fatal("derived image content mismatch")
	}
}

func TestNativeChownAndChmod(t *testing.T) {
	builder, _, puller := newNativeBuilder(t, nil)
	contextDir := writeContext(t, map[string]string{
		"Dockerfile": "FROM scratch\nCOPY --chown=1000:1000 --chmod=755 script.sh /script.sh\n",
		"script.sh":  "#!/bin/sh\n",
	})
	result, err := builder.Build(context.Background(), Request{ContextDir: contextDir, Reference: "grillo.local/own:1"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	root := unpackImage(t, puller, result.Image)
	info, err := os.Stat(filepath.Join(root, "script.sh"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o755 {
		t.Fatalf("mode = %v", info.Mode().Perm())
	}
}

func nonEmptyHistory(config oci.ImageConfig) int {
	count := 0
	for _, entry := range config.History {
		if !entry.EmptyLayer {
			count++
		}
	}
	return count
}

func TestNativeMultiStageHistoryConsistent(t *testing.T) {
	runner := &testRunner{}
	builder, _, _ := newNativeBuilder(t, runner)
	ctx := context.Background()
	base := writeContext(t, map[string]string{"Dockerfile": "FROM scratch\nCOPY base.txt /base.txt\n", "base.txt": "base"})
	if _, err := builder.Build(ctx, Request{ContextDir: base, Reference: "grillo.local/hb:1"}, nil); err != nil {
		t.Fatal(err)
	}

	// COPY --from=0 must resolve a named stage by index, and history must stay
	// consistent with the layer count.
	final := writeContext(t, map[string]string{
		"Dockerfile": "FROM grillo.local/hb:1 AS build\nRUN sh -c \"echo x > x.txt\"\nFROM scratch\nCOPY --from=0 /x.txt /x.txt\nENV A=1\n",
	})
	result, err := builder.Build(ctx, Request{ContextDir: final, Reference: "grillo.local/hf:1"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := len(result.Image.Manifest.Layers); got != 1 {
		t.Fatalf("final layers = %d, want 1", got)
	}
	if got := nonEmptyHistory(result.Image.Config); got != 1 {
		t.Fatalf("final non-empty history = %d, want 1 (config %+v)", got, result.Image.Config.History)
	}

	// A stage that inherits an image base must carry the base history forward.
	inherited := writeContext(t, map[string]string{"Dockerfile": "FROM grillo.local/hb:1\nRUN sh -c \"echo y > y.txt\"\n"})
	derived, err := builder.Build(ctx, Request{ContextDir: inherited, Reference: "grillo.local/hi:1"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := len(derived.Image.Manifest.Layers); got != 2 {
		t.Fatalf("inherited layers = %d, want 2", got)
	}
	if got := nonEmptyHistory(derived.Image.Config); got != 2 {
		t.Fatalf("inherited non-empty history = %d, want 2 (config %+v)", got, derived.Image.Config.History)
	}
}

func TestNativeCopyFromImage(t *testing.T) {
	builder, _, puller := newNativeBuilder(t, nil)
	ctx := context.Background()
	base := writeContext(t, map[string]string{"Dockerfile": "FROM scratch\nCOPY base.txt /base.txt\n", "base.txt": "from-image"})
	if _, err := builder.Build(ctx, Request{ContextDir: base, Reference: "grillo.local/cfi:1"}, nil); err != nil {
		t.Fatal(err)
	}
	final := writeContext(t, map[string]string{"Dockerfile": "FROM scratch\nCOPY --from=grillo.local/cfi:1 /base.txt /copied.txt\n"})
	result, err := builder.Build(ctx, Request{ContextDir: final, Reference: "grillo.local/cfi-final:1"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	root := unpackImage(t, puller, result.Image)
	if got := readFile(t, root, "copied.txt"); got != "from-image" {
		t.Fatalf("copied.txt = %q", got)
	}
}
