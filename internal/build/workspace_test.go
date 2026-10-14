//go:build linux

// SPDX-License-Identifier: Apache-2.0

package build

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/albertize/grillo/internal/dockerfile"
)

func TestHostCopyReservesBeforeCreationAndCountsOverwrites(t *testing.T) {
	source := writeContext(t, map[string]string{"data": "abcd", "ignored": "large"})
	root := t.TempDir()
	budget, err := newWorkspaceBudget(context.Background(), 4, 10)
	if err != nil {
		t.Fatal(err)
	}
	opts := copyOptions{base: source, destRoot: root, budget: budget}
	if err := copyTree(filepath.Join(source, "data"), filepath.Join(root, "data"), opts); err != nil {
		t.Fatal(err)
	}
	if err := copyTree(filepath.Join(source, "data"), filepath.Join(root, "data"), opts); !errors.Is(err, ErrWorkspaceLimit) {
		t.Fatalf("overwrite: %v", err)
	}
	if got := readFile(t, root, "data"); got != "abcd" {
		t.Fatal("rejected overwrite modified destination")
	}
	if err := copyTree(filepath.Join(source, "data"), filepath.Join(root, "other"), opts); !errors.Is(err, ErrWorkspaceLimit) {
		t.Fatalf("new copy: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(root, "other")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("rejected copy created a file", err)
	}
	opts.ignore = dockerfile.ParseIgnore([]byte("ignored\n"))
	if err := copyTree(filepath.Join(source, "ignored"), filepath.Join(root, "ignored"), opts); err != nil {
		t.Fatal("ignored source charged budget", err)
	}
}

func TestHostCopyBoundsImplicitParentsAndCancellation(t *testing.T) {
	source := writeContext(t, map[string]string{"data": "abcd"})
	root := t.TempDir()
	budget, _ := newWorkspaceBudget(context.Background(), 10, 2)
	err := copyTree(filepath.Join(source, "data"), filepath.Join(root, "a/b/data"), copyOptions{base: source, destRoot: root, budget: budget})
	if !errors.Is(err, ErrWorkspaceLimit) {
		t.Fatal("parent admission", err)
	}
	if entries, _ := os.ReadDir(root); len(entries) != 0 {
		t.Fatal("rejected copy created parents")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err = copyTree(filepath.Join(source, "data"), filepath.Join(root, "data"), copyOptions{ctx: ctx, base: source, destRoot: root})
	if !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation", err)
	}
	if _, err := snapshotTreeContext(ctx, source, 10); !errors.Is(err, context.Canceled) {
		t.Fatal("snapshot cancellation", err)
	}
	if _, err := snapshotTreeContext(context.Background(), source, 1); err != nil {
		t.Fatal("exact snapshot boundary", err)
	}
	if err := os.WriteFile(filepath.Join(source, "extra"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := snapshotTreeContext(context.Background(), source, 1); !errors.Is(err, ErrWorkspaceLimit) {
		t.Fatal("snapshot entry limit", err)
	}
}

func TestHostCopyRootAndSymlinkContracts(t *testing.T) {
	source := writeContext(t, map[string]string{"data": "abcd"})
	root := t.TempDir()
	outside := writeContext(t, map[string]string{"keep": "unchanged"})
	if err := os.Symlink(outside, filepath.Join(root, "redirect")); err != nil {
		t.Fatal(err)
	}
	if err := copyTree(filepath.Join(source, "data"), filepath.Join(root, "redirect/keep"), copyOptions{base: source, destRoot: root}); err == nil {
		t.Fatal("destination escaped root")
	}
	if got := readFile(t, outside, "keep"); got != "unchanged" {
		t.Fatal("outside destination changed")
	}
	if err := os.Symlink(outside, filepath.Join(source, "redirect")); err != nil {
		t.Fatal(err)
	}
	if err := copyTree(filepath.Join(source, "redirect/keep"), filepath.Join(root, "data"), copyOptions{base: source, destRoot: root}); err == nil {
		t.Fatal("source escaped root")
	}
	if err := os.Symlink("data", filepath.Join(source, "relative")); err != nil {
		t.Fatal(err)
	}
	if err := copyTree(filepath.Join(source, "relative"), filepath.Join(root, "relative"), copyOptions{base: source, destRoot: root, mode: 0755}); err != nil {
		t.Fatal(err)
	}
	if link, err := os.Readlink(filepath.Join(root, "relative")); err != nil || link != "data" {
		t.Fatal("symlink was not preserved", err)
	}
	if err := copyTree(filepath.Join(outside, "keep"), filepath.Join(root, "data"), copyOptions{base: source, destRoot: root}); err == nil {
		t.Fatal("parent traversal accepted")
	}
}

func TestHostCopyGlobBoundsAndOrdering(t *testing.T) {
	root := writeContext(t, map[string]string{"z.txt": "", "a.txt": "", "nested/b.txt": ""})
	matches, err := copyMatches(context.Background(), root, "/*.txt", 10)
	if err != nil || len(matches) != 2 || filepath.Base(matches[0]) != "a.txt" || filepath.Base(matches[1]) != "z.txt" {
		t.Fatal("matches", matches, err)
	}
	if _, err := copyMatches(context.Background(), root, "*.txt", 1); !errors.Is(err, ErrWorkspaceLimit) {
		t.Fatal("glob scan limit", err)
	}
	for _, pattern := range []string{"../*", "[", strings.Repeat("a/", maxWorkspaceDepth) + "*"} {
		if _, err := copyMatches(context.Background(), root, pattern, 10); err == nil {
			t.Fatalf("accepted pattern %q", pattern)
		}
	}
}

func TestHostLayerWriterLimitsBeforeWriteAndCancels(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	budget, _ := newWorkspaceBudget(ctx, 4, 10)
	var out bytes.Buffer
	w := &workspaceWriter{budget: budget, remaining: 3, writer: &out}
	if _, err := w.Write([]byte("abcd")); !errors.Is(err, ErrWorkspaceLimit) || out.Len() != 0 {
		t.Fatal("per-blob limit", err)
	}
	if _, err := w.Write([]byte("abc")); err != nil {
		t.Fatal("exact boundary", err)
	}
	cancel()
	if _, err := w.Write([]byte("x")); !errors.Is(err, context.Canceled) || out.String() != "abc" {
		t.Fatal("writer cancellation", err)
	}
	if _, err := io.ReadAll(contextReader{ctx, strings.NewReader("abc")}); !errors.Is(err, context.Canceled) {
		t.Fatal("reader cancellation", err)
	}
	for _, policy := range [][2]int64{{-1, 1}, {1, -1}} {
		if _, err := newWorkspaceBudget(context.Background(), policy[0], policy[1]); err == nil {
			t.Fatal("negative policy accepted")
		}
	}
}

func TestNativeHostBudgetFailureDoesNotPublishOrLeakScratch(t *testing.T) {
	for _, scenario := range []string{"copy", "entries", "blob", "stages"} {
		t.Run(scenario, func(t *testing.T) {
			b, _, _ := newNativeBuilder(t, nil)
			file := "FROM scratch\nCOPY data /data\n"
			switch scenario {
			case "copy":
				b.MaxHostWriteBytes = 3
			case "entries":
				b.MaxHostEntries = 2
			case "blob":
				b.MaxBlobBytes = 1
			case "stages":
				b.MaxHostEntries = 6
				file += "FROM scratch\nCOPY data /data\n"
			}
			root := writeContext(t, map[string]string{"Dockerfile": file, "data": "abcd"})
			ref := "grillo.local/budget:1"
			_, err := b.Build(context.Background(), Request{ContextDir: root, Reference: ref}, nil)
			if !errors.Is(err, ErrWorkspaceLimit) {
				t.Fatal("expected budget failure", err)
			}
			if _, ok, err := b.Store.Get(ref); err != nil || ok {
				t.Fatal("failed build published an image", err)
			}
			if entries, err := os.ReadDir(b.ScratchDir); err != nil || len(entries) != 0 {
				t.Fatal("owned scratch leaked", err)
			}
		})
	}
}

func TestNativeImportedBaseSharesHostBudget(t *testing.T) {
	b, _, _ := newNativeBuilder(t, nil)
	root := writeContext(t, map[string]string{"Dockerfile": "FROM scratch\nCOPY data /data\n", "data": strings.Repeat("x", 4096)})
	ref := "grillo.local/base-budget:1"
	prior, err := b.Build(context.Background(), Request{ContextDir: root, Reference: ref}, nil)
	if err != nil {
		t.Fatal(err)
	}
	b.MaxHostWriteBytes = 4095
	root = writeContext(t, map[string]string{"Dockerfile": "FROM " + ref + "\n"})
	if _, err := b.Build(context.Background(), Request{ContextDir: root, Reference: ref}, nil); !errors.Is(err, ErrWorkspaceLimit) {
		t.Fatal("import escaped aggregate budget", err)
	}
	record, ok, err := b.Store.Get(ref)
	if err != nil || !ok || record.ManifestDigest != prior.ManifestDigest {
		t.Fatal("failed import replaced prior image", err)
	}
}

func TestNativeMetadataLimits(t *testing.T) {
	for _, name := range []string{"Dockerfile", ".dockerignore"} {
		t.Run(name, func(t *testing.T) {
			b, _, _ := newNativeBuilder(t, nil)
			root := writeContext(t, map[string]string{"Dockerfile": "FROM scratch\n", name: strings.Repeat("x", (1<<20)+1)})
			if _, err := b.Build(context.Background(), Request{ContextDir: root, Reference: "grillo.local/metadata:1"}, nil); err == nil || !strings.Contains(err.Error(), "1 MiB") {
				t.Fatal("metadata limit", err)
			}
		})
	}
}

func TestNativeExactHostWriteBudgetIncludesLayerTrailer(t *testing.T) {
	b, _, _ := newNativeBuilder(t, nil)
	root := writeContext(t, map[string]string{"Dockerfile": "FROM scratch\nWORKDIR /\nCOPY data /data\n", "data": "abcd"})
	if _, err := b.Build(context.Background(), Request{ContextDir: root, Reference: "grillo.local/root-workdir:1"}, nil); err != nil {
		t.Fatal("root workdir", err)
	}

	// Use the same tree for each archive so timestamps do not make this exact
	// byte-boundary test depend on crossing a wall-clock second between builds.
	root = writeContext(t, map[string]string{"data": "abcd"})
	scratch := t.TempDir()
	makeContext := func(limit int64) *buildContext {
		budget, err := newWorkspaceBudget(context.Background(), limit, 10)
		if err != nil {
			t.Fatal(err)
		}
		if err := budget.reserve("data", 4); err != nil {
			t.Fatal(err)
		}
		return &buildContext{scratch: scratch, budget: budget}
	}
	first, _, err := b.commitLayer(makeContext(0), "layer", nil, root, nil)
	if err != nil || first == nil {
		t.Fatal("initial layer", err)
	}
	exact, _, err := b.commitLayer(makeContext(4+first.Size), "layer", nil, root, nil)
	if err != nil || exact == nil || exact.Digest != first.Digest {
		t.Fatal("exact host budget", err)
	}
	if _, _, err := b.commitLayer(makeContext(4+first.Size-1), "layer", nil, root, nil); !errors.Is(err, ErrWorkspaceLimit) {
		t.Fatal("layer trailer escaped budget", err)
	}
	if entries, err := os.ReadDir(scratch); err != nil || len(entries) != 0 {
		t.Fatal("layer scratch leaked", err)
	}
}
