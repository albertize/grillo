// SPDX-License-Identifier: Apache-2.0

// Command manifest writes a guest artifact manifest. It is invoked by
// guest/build-image.sh with name=version=path arguments.
package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"grillo.local/grillo/internal/guest"
)

func main() {
	out := flag.String("out", "manifest.json", "output manifest path")
	flag.Parse()
	var specs []guest.ArtifactSpec
	for _, arg := range flag.Args() {
		parts := strings.SplitN(arg, "=", 3)
		if len(parts) != 3 || parts[0] == "" || parts[2] == "" {
			fmt.Fprintf(os.Stderr, "manifest: want name=version=path, got %q\n", arg)
			os.Exit(2)
		}
		specs = append(specs, guest.ArtifactSpec{Name: parts[0], Version: parts[1], Path: parts[2]})
	}
	manifest, err := guest.BuildManifest(specs)
	if err != nil {
		fmt.Fprintln(os.Stderr, "manifest:", err)
		os.Exit(1)
	}
	if err := manifest.Write(*out); err != nil {
		fmt.Fprintln(os.Stderr, "manifest:", err)
		os.Exit(1)
	}
}
