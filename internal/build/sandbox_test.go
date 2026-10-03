//go:build linux

// SPDX-License-Identifier: Apache-2.0

package build

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"grillo.local/grillo/internal/guestproto"
)

type fakeSandboxClient struct {
	requests []guestproto.RunRequest
	starts   []guestproto.StartRequest
	output   []string
	exitCode int
	closed   bool
}

func (f *fakeSandboxClient) Start(_ context.Context, request guestproto.StartRequest) (guestproto.StartResult, error) {
	f.starts = append(f.starts, request)
	return guestproto.StartResult{State: "ready"}, nil
}

func (f *fakeSandboxClient) Run(_ context.Context, request guestproto.RunRequest, stdout, _ io.Writer) (guestproto.RunResult, error) {
	f.requests = append(f.requests, request)
	if stdout != nil {
		for _, line := range f.output {
			_, _ = io.WriteString(stdout, line+"\n")
		}
	}
	return guestproto.RunResult{ExitCode: f.exitCode}, nil
}

func (f *fakeSandboxClient) Close() error {
	f.closed = true
	return nil
}

func TestSandboxRunnerBootsOncePerRoot(t *testing.T) {
	fake := &fakeSandboxClient{output: []string{"hi"}}
	boots := 0
	runner := &SandboxRunner{
		Share: "build",
		Boot: func(context.Context, string) (SandboxClient, func() error, error) {
			boots++
			return fake, func() error { return nil }, nil
		},
	}
	var lines []string
	progress := func(line string) { lines = append(lines, line) }
	for i := 0; i < 2; i++ {
		if err := runner.Run(context.Background(), RunStep{RootfsDir: "/root", Command: []string{"sh", "-c", "echo hi"}, WorkDir: "/work"}, progress); err != nil {
			t.Fatal(err)
		}
	}
	if boots != 1 {
		t.Fatalf("boots = %d, want 1", boots)
	}
	if len(fake.requests) != 2 || fake.requests[0].Share != "build" || fake.requests[0].WorkDir != "/work" {
		t.Fatalf("requests = %+v", fake.requests)
	}
	if len(lines) == 0 || !strings.Contains(lines[0], "hi") {
		t.Fatalf("progress lines = %v", lines)
	}
	if err := runner.Close(); err != nil {
		t.Fatal(err)
	}
	if !fake.closed {
		t.Fatal("client not closed")
	}
}

func TestSandboxRunnerFailure(t *testing.T) {
	runner := &SandboxRunner{
		Share: "build",
		Boot: func(context.Context, string) (SandboxClient, func() error, error) {
			return nil, nil, errors.New("boot failed")
		},
	}
	if err := runner.Run(context.Background(), RunStep{RootfsDir: "/root", Command: []string{"true"}}, nil); err == nil {
		t.Fatal("boot failure was ignored")
	}
	failing := &fakeSandboxClient{exitCode: 3}
	runner.Boot = func(context.Context, string) (SandboxClient, func() error, error) {
		return failing, func() error { return nil }, nil
	}
	err := runner.Run(context.Background(), RunStep{RootfsDir: "/root", Command: []string{"false"}}, nil)
	if err == nil || !strings.Contains(err.Error(), "exited with code 3") {
		t.Fatalf("error = %v", err)
	}
}

func TestParseUserSpec(t *testing.T) {
	spec, err := parseUserSpec("1000:1001")
	if err != nil || spec.UID != 1000 || spec.GID != 1001 {
		t.Fatalf("spec = %+v err=%v", spec, err)
	}
	spec, err = parseUserSpec("")
	if err != nil || spec.UID != 0 {
		t.Fatalf("empty spec = %+v err=%v", spec, err)
	}
	if _, err := parseUserSpec("alice"); err == nil {
		t.Fatal("non-numeric user was accepted")
	}
}
