//go:build linux

// SPDX-License-Identifier: Apache-2.0

// Command stage assembles an experimental local prefix, not a public release.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/albertize/grillo/internal/distribution"
)

func main() {
	cfg := distribution.StageConfig{}
	flag.StringVar(&cfg.Output, "out", "", "new immutable output prefix (must not exist)")
	flag.StringVar(&cfg.Binaries, "binaries", "bin", "trusted locally built host binaries")
	flag.StringVar(&cfg.Guest, "guest", "", "immutable runtime guest directory")
	selection := flag.String("guest-selection", "", "development guest selection file, alternative to -guest")
	flag.StringVar(&cfg.Helm, "helm", "", "explicit trusted exact Helm helper")
	flag.StringVar(&cfg.HelmSHA256, "helm-sha256", "", "explicit expected helper SHA-256 (local pin, not publisher proof)")
	flag.StringVar(&cfg.Examples, "examples", "examples/distribution", "self-contained demo inputs")
	flag.StringVar(&cfg.Documents, "documents", ".", "Grillo LICENSE/NOTICE directory")
	flag.Parse()
	if flag.NArg() != 0 || (*selection != "" && cfg.Guest != "") {
		fmt.Fprintln(os.Stderr, "stage: unexpected/conflicting arguments")
		os.Exit(2)
	}
	if *selection != "" {
		data, err := os.ReadFile(*selection)
		if err != nil || len(data) > 4096 {
			fmt.Fprintln(os.Stderr, "stage: invalid guest selection")
			os.Exit(1)
		}
		cfg.Guest = strings.TrimSpace(string(data))
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	ctx, deadline := context.WithTimeout(ctx, 3*time.Minute)
	defer deadline()
	if err := distribution.Stage(ctx, cfg); err != nil {
		fmt.Fprintln(os.Stderr, "stage:", err)
		os.Exit(1)
	}
	fmt.Println("Staged experimental local prefix; redistribution review remains pending.")
}
