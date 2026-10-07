//go:build linux

// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"bytes"
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestNonReadingOutputCancelsAndRestoresDescriptorFlags(t *testing.T) {
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	defer writer.Close()
	before, err := unix.FcntlInt(writer.Fd(), unix.F_GETFL, 0)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	output, restore, err := boundedTerminalOutput(ctx, writer)
	if err != nil {
		t.Fatal(err)
	}
	_, err = output.Write(bytes.Repeat([]byte("x"), 1<<20))
	restore()
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("stalled output error=%v", err)
	}
	after, err := unix.FcntlInt(writer.Fd(), unix.F_GETFL, 0)
	if err != nil || before != after {
		t.Fatal("descriptor flags not restored")
	}
}
