//go:build linux

// SPDX-License-Identifier: Apache-2.0

// Command runtime-image builds an immutable fixture-free runtime guest using
// already-provisioned pinned inputs. It never downloads or executes those inputs.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/albertize/grillo/internal/guest"
)

func main() {
	cfg := guest.RuntimeImageConfig{}
	flag.StringVar(&cfg.Agent, "agent", "", "fresh static grillo-agent binary")
	flag.StringVar(&cfg.Kernel, "kernel", "", "prepared guest bzImage")
	flag.StringVar(&cfg.KernelVersion, "kernel-version", "", "pinned source kernel version")
	flag.StringVar(&cfg.Inputs, "inputs", "", "provisioned runc and pinned BusyBox/library inputs directory")
	flag.StringVar(&cfg.Store, "store", "", "content-identity runtime guest store")
	flag.StringVar(&cfg.Version, "version", "dev", "Grillo build version")
	flag.StringVar(&cfg.Commit, "commit", "unknown", "source commit")
	selection := flag.String("selection", "", "optional atomic development selection file (not an installed asset)")
	flag.Parse()
	if flag.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "runtime-image: unexpected arguments")
		os.Exit(2)
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	ctx, deadline := context.WithTimeout(ctx, 2*time.Minute)
	defer deadline()
	path, err := guest.BuildRuntimeImage(ctx, cfg)
	if err != nil {
		fmt.Fprintln(os.Stderr, "runtime-image:", err)
		os.Exit(1)
	}
	if *selection != "" {
		if err := publishSelection(*selection, path); err != nil {
			fmt.Fprintln(os.Stderr, "runtime-image:", err)
			os.Exit(1)
		}
	}
	fmt.Println(path)
}

func publishSelection(path, value string) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".runtime-selection-")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	defer f.Close()
	if _, err := f.WriteString(value + "\n"); err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}
