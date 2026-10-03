// SPDX-License-Identifier: Apache-2.0

package oci

import "testing"

func TestMergeConfigOverrides(t *testing.T) {
	image := ImageConfig{}
	image.Config.Entrypoint = []string{"/entry"}
	image.Config.Cmd = []string{"--flag", "image"}
	image.Config.Env = []string{"A=1", "B=2"}
	image.Config.User = "1000"
	image.Config.WorkingDir = "/srv"
	image.Config.StopSignal = "SIGTERM"

	// No overrides: image config wins.
	got := MergeConfig(image, RuntimeOptions{})
	if len(got.Args) != 3 || got.Args[0] != "/entry" || got.User != "1000" || got.WorkingDir != "/srv" {
		t.Fatalf("image-only merge = %+v", got)
	}
	if got.StopSignal != "SIGTERM" {
		t.Fatalf("stop signal = %q", got.StopSignal)
	}

	// Explicit overrides win, and env merges by key.
	got = MergeConfig(image, RuntimeOptions{
		Entrypoint: []string{"/runtime"},
		Cmd:        []string{"serve"},
		Env:        []string{"B=override", "C=3"},
		User:       "2000",
		WorkingDir: "/work",
	})
	if len(got.Args) != 2 || got.Args[0] != "/runtime" || got.Args[1] != "serve" {
		t.Fatalf("args = %v", got.Args)
	}
	if got.User != "2000" || got.WorkingDir != "/work" {
		t.Fatalf("user/workdir = %q %q", got.User, got.WorkingDir)
	}
	wantEnv := map[string]string{"A": "1", "B": "override", "C": "3"}
	if len(got.Env) != len(wantEnv) {
		t.Fatalf("env = %v", got.Env)
	}
	for _, kv := range got.Env {
		key, value, _ := cutKV(kv)
		if wantEnv[key] != value {
			t.Fatalf("env %q = %q, want %q", key, value, wantEnv[key])
		}
	}
}

func TestParseImageConfigRejectsBadDiffID(t *testing.T) {
	if _, err := ParseImageConfig([]byte(`{"rootfs":{"type":"layers","diff_ids":["sha256:nope"]}}`)); err == nil {
		t.Fatal("expected invalid diff_id rejection")
	}
}

func cutKV(s string) (string, string, bool) {
	for i := 0; i < len(s); i++ {
		if s[i] == '=' {
			return s[:i], s[i+1:], true
		}
	}
	return s, "", false
}
