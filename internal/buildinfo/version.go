// SPDX-License-Identifier: Apache-2.0

// Package buildinfo holds shared host build identity injected with Go -X flags.
package buildinfo

import "github.com/albertize/grillo/internal/runtimeassets"

var Version = "dev"
var Commit = "unknown"
var BuildTime = "unknown"

type Info struct {
	Version   string `json:"version"`
	Commit    string `json:"commit"`
	BuildTime string `json:"build_time"`
	GuestABI  string `json:"guest_abi"`
}

func Current() Info { return Info{Version, Commit, BuildTime, runtimeassets.GuestABI} }
