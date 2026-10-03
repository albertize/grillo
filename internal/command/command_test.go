// SPDX-License-Identifier: Apache-2.0

package command

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func TestRun(t *testing.T) {
	tests := []struct {
		name string
		args []string
		code int
		out  string
		err  string
	}{
		{"grillo", nil, 0, "usage: grillo <version|doctor|help>\n", ""},
		{"grillo", []string{"version"}, 0, "grillo dev (scaffold; runtime unavailable)\n", ""},
		{"grillo", []string{"--version"}, 0, "grillo dev (scaffold; runtime unavailable)\n", ""},
		{"grillo", []string{"doctor"}, 1, "NOT CHECKED: KVM, namespaces, VMM, networking, and guest artifacts.\nDoctor is not implemented; runtime readiness is unknown.\n", ""},
		{"grillo", []string{"up"}, 2, "", "usage: grillo <version|doctor|help>\n"},
		{"grillo", []string{"version", "secret-value"}, 2, "", "usage: grillo <version|doctor|help>\n"},
		{"grillo", []string{"secret-value"}, 2, "", "usage: grillo <version|doctor|help>\n"},
		{"grillo-agent", nil, 1, "", "grillo-agent: runtime not implemented; do not use as guest PID 1\n"},
		{"grillo-agent", []string{"version"}, 0, "grillo-agent dev (scaffold; runtime unavailable)\n", ""},
		{"grillo-agent", []string{"doctor"}, 2, "", "usage: grillo-agent <version|help>\n"},
	}
	for _, name := range []string{"grillo", "grillo-agent"} {
		for _, arg := range []string{"help", "-h", "--help"} {
			t.Run(name+"/"+arg, func(t *testing.T) {
				var out, err bytes.Buffer
				if code := Run(name, []string{arg}, &out, &err); code != 0 || !strings.HasPrefix(out.String(), "usage: "+name) || err.Len() != 0 {
					t.Fatalf("code=%d stdout=%q stderr=%q", code, out.String(), err.String())
				}
			})
		}
	}
	for _, tt := range tests {
		t.Run(tt.name+"/"+strings.Join(tt.args, "/"), func(t *testing.T) {
			t.Parallel()
			var out, err bytes.Buffer
			code := Run(tt.name, tt.args, &out, &err)
			if code != tt.code || out.String() != tt.out || err.String() != tt.err {
				t.Fatalf("code=%d stdout=%q stderr=%q; want %d %q %q", code, out.String(), err.String(), tt.code, tt.out, tt.err)
			}
		})
	}
}

type failedWriter struct{}

func (failedWriter) Write([]byte) (int, error) { return 0, errors.New("output unavailable") }

func TestOutputFailure(t *testing.T) {
	for _, args := range [][]string{nil, {"help"}, {"version"}, {"doctor"}, {"invalid"}, {"version", "extra"}} {
		if code := Run("grillo", args, failedWriter{}, failedWriter{}); code != 1 {
			t.Errorf("Run(%q) = %d; want output failure", args, code)
		}
	}
}
