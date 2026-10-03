// SPDX-License-Identifier: Apache-2.0

// Package command contains the side-effect-free command scaffold shared by the
// host CLI and guest binary. It does not start a runtime or inspect the host.
package command

import (
	"fmt"
	"io"
)

// Run returns a process exit code: 0 for success, 1 for unavailable functionality
// or output failure, and 2 for invalid usage. Arguments are never echoed, since
// they may contain secrets. Writers belong to the caller.
func Run(name string, args []string, stdout, stderr io.Writer) int {
	usage := "usage: " + name + " <version|help>\n"
	if name == "grillo" {
		usage = "usage: grillo <version|doctor|help>\n"
	}
	if len(args) == 0 {
		if name != "grillo" {
			return message(stderr, name+": runtime not implemented; do not use as guest PID 1\n", 1)
		}
		return message(stdout, usage, 0)
	}
	if len(args) != 1 {
		return message(stderr, usage, 2)
	}
	switch args[0] {
	case "help", "-h", "--help":
		return message(stdout, usage, 0)
	case "version", "--version":
		return message(stdout, name+" dev (scaffold; runtime unavailable)\n", 0)
	case "doctor":
		if name == "grillo" {
			return message(stdout, "NOT CHECKED: KVM, namespaces, VMM, networking, and guest artifacts.\nDoctor is not implemented; runtime readiness is unknown.\n", 1)
		}
	}
	return message(stderr, usage, 2)
}

func message(w io.Writer, text string, code int) int {
	if _, err := fmt.Fprint(w, text); err != nil {
		return 1
	}
	return code
}
