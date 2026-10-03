// SPDX-License-Identifier: Apache-2.0

// Package cli implements the native-runtime command line. It parses flags before
// and after positionals, repeated flags, and an exec `--` terminator, and it
// talks to the daemon through the local API. It never lets CLI exit stop a
// workload.
package cli

import (
	"flag"
	"fmt"
	"strings"
)

// SplitForFlagSet separates interspersed flags from positionals so the standard
// flag package can parse them. Flags may appear before or after positionals.
// `--` terminates flag parsing; everything after it is returned in afterDD.
func SplitForFlagSet(args []string, fs *flag.FlagSet) (flags, positionals, afterDD []string, err error) {
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			return flags, positionals, append([]string(nil), args[i+1:]...), nil
		}
		if len(arg) > 1 && arg[0] == '-' {
			name := strings.TrimLeft(arg, "-")
			if name == "" {
				positionals = append(positionals, arg)
				continue
			}
			key, _, hasValue := strings.Cut(name, "=")
			f := fs.Lookup(key)
			if f == nil {
				return nil, nil, nil, fmt.Errorf("unknown flag %q", arg)
			}
			flags = append(flags, arg)
			if !hasValue && !isBoolFlag(f) {
				if i+1 >= len(args) {
					return nil, nil, nil, fmt.Errorf("flag %q needs a value", arg)
				}
				flags = append(flags, args[i+1])
				i++
			}
			continue
		}
		positionals = append(positionals, arg)
	}
	return flags, positionals, nil, nil
}

func isBoolFlag(f *flag.Flag) bool {
	bf, ok := f.Value.(interface{ IsBoolFlag() bool })
	return ok && bf.IsBoolFlag()
}

// stringSlice is a repeatable string flag.
type stringSlice []string

func (s *stringSlice) String() string { return strings.Join(*s, ",") }
func (s *stringSlice) Set(value string) error {
	*s = append(*s, value)
	return nil
}
