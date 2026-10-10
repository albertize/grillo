//go:build linux

// SPDX-License-Identifier: Apache-2.0

package distribution

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func TestStageNeverOverwritesOrPublishesUnverifiedHelpers(t *testing.T) {
	root := t.TempDir()
	bin := filepath.Join(root, "bin")
	if err := os.Mkdir(bin, 0700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"grillo", "grillod", "grillo-netns", "helm"} {
		if err := os.WriteFile(filepath.Join(bin, name), []byte("never executed"), 0700); err != nil {
			t.Fatal(err)
		}
	}
	output := filepath.Join(root, "prefix")
	cfg := StageConfig{Output: output, Binaries: bin, Guest: root, Helm: filepath.Join(bin, "helm"), HelmSHA256: strings.Repeat("0", 64), Examples: root, Documents: root}
	if err := Stage(context.Background(), cfg); err == nil {
		t.Fatal("wrong renderer digest accepted")
	}
	if _, err := os.Stat(output); !os.IsNotExist(err) {
		t.Fatal("failed stage published")
	}
	entries, _ := os.ReadDir(root)
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".grillo-") {
			t.Fatal("stage leaked", entry.Name())
		}
	}
	if err := os.Mkdir(output, 0700); err != nil {
		t.Fatal(err)
	}
	sentinel := filepath.Join(output, "user-owned")
	if err := os.WriteFile(sentinel, []byte("preserve"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := Stage(context.Background(), cfg); err == nil {
		t.Fatal("preexisting prefix accepted")
	}
	data, err := os.ReadFile(sentinel)
	if err != nil || string(data) != "preserve" {
		t.Fatal("user prefix changed")
	}
}

func TestStageExampleSymlinksAndSpecialInputsFailClosed(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "examples")
	target := filepath.Join(root, "stage")
	if err := os.Mkdir(source, 0700); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(root, "outside")
	if err := os.WriteFile(outside, []byte("private"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(source, "link")); err != nil {
		t.Fatal(err)
	}
	if err := copyExamples(context.Background(), source, target, &Inventory{}); err == nil {
		t.Fatal("example symlink accepted")
	}
	if _, err := openSource(filepath.Join(source, "link")); err == nil {
		t.Fatal("source symlink accepted")
	}
	fifo := filepath.Join(source, "fifo")
	if err := syscall.Mkfifo(fifo, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := openSource(fifo); err == nil {
		t.Fatal("FIFO source accepted")
	}
}

func TestStageDirectoryCountAndCanceledWalk(t *testing.T) {
	source := t.TempDir()
	target := t.TempDir()
	for i := 0; i < 257; i++ {
		if err := os.Mkdir(filepath.Join(source, fmt.Sprint(i)), 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err := copyExamples(context.Background(), source, target, &Inventory{}); err == nil {
		t.Fatal("unbounded empty directories accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := copyExamples(ctx, t.TempDir(), target, &Inventory{}); !errors.Is(err, context.Canceled) {
		t.Fatal("walk cancellation ignored", err)
	}
}

func TestStageToolOutputBound(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tool")
	if err := os.WriteFile(path, []byte("#!/bin/sh\ni=0; while [ $i -lt 10000 ]; do printf 'xxxxxxxxxxxxxxxx'; i=$((i+1)); done\n"), 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := toolOutput(context.Background(), path, nil, filepath.Dir(path)); err == nil {
		t.Fatal("oversized identity output accepted through io.Copy fast path")
	}
}

func TestStageCopiesBoundedBytesAndCancellation(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "source")
	if err := os.WriteFile(path, []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	source, err := openSource(path)
	if err != nil {
		t.Fatal(err)
	}
	inventory := Inventory{}
	if err := copyFile(context.Background(), source, filepath.Join(root, "stage/file"), 0444, "", "file", &inventory); err != nil {
		t.Fatal(err)
	}
	if len(inventory.Files) != 1 || inventory.Files[0].Size != 7 || len(inventory.Files[0].SHA256) != 64 {
		t.Fatal(inventory)
	}
	source, err = openSource(path)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := copyFile(ctx, source, filepath.Join(root, "stage/canceled"), 0444, "", "canceled", &inventory); err == nil {
		t.Fatal("canceled copy succeeded")
	}
}
