//go:build linux

// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"flag"
	"reflect"
	"testing"
)

func TestSplitForFlagSetInterspersed(t *testing.T) {
	fs := flag.NewFlagSet("up", flag.ContinueOnError)
	var files stringSlice
	fs.Var(&files, "f", "file")
	verbose := fs.Bool("verbose", false, "")

	flags, positionals, afterDD, err := SplitForFlagSet([]string{"-f", "a.yaml", "app.yaml", "--verbose", "-f=b.yaml"}, fs)
	if err != nil {
		t.Fatal(err)
	}
	if len(afterDD) != 0 {
		t.Fatalf("afterDD = %v", afterDD)
	}
	if !reflect.DeepEqual(positionals, []string{"app.yaml"}) {
		t.Fatalf("positionals = %v", positionals)
	}
	if err := fs.Parse(flags); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual([]string(files), []string{"a.yaml", "b.yaml"}) {
		t.Fatalf("repeated -f = %v", files)
	}
	if !*verbose {
		t.Fatal("verbose flag not parsed")
	}
}

func TestSplitForFlagSetExecTerminator(t *testing.T) {
	fs := flag.NewFlagSet("exec", flag.ContinueOnError)
	flags, positionals, afterDD, err := SplitForFlagSet([]string{"pod/backend-0", "api", "--", "sh", "-c", "echo hi"}, fs)
	if err != nil {
		t.Fatal(err)
	}
	if len(flags) != 0 {
		t.Fatalf("flags = %v", flags)
	}
	if !reflect.DeepEqual(positionals, []string{"pod/backend-0", "api"}) {
		t.Fatalf("positionals = %v", positionals)
	}
	if !reflect.DeepEqual(afterDD, []string{"sh", "-c", "echo hi"}) {
		t.Fatalf("afterDD = %v", afterDD)
	}
}

func TestSplitForFlagSetErrors(t *testing.T) {
	fs := flag.NewFlagSet("x", flag.ContinueOnError)
	var files stringSlice
	fs.Var(&files, "f", "file")
	if _, _, _, err := SplitForFlagSet([]string{"--nope"}, fs); err == nil {
		t.Fatal("unknown flag not rejected")
	}
	if _, _, _, err := SplitForFlagSet([]string{"-f"}, fs); err == nil {
		t.Fatal("missing value not rejected")
	}
}
