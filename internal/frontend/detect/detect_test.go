// SPDX-License-Identifier: Apache-2.0

package detect

import (
	"strings"
	"testing"

	"grillo.local/grillo/internal/source"
)

func TestFormat(t *testing.T) {
	cases := []struct {
		name string
		data string
		path string
		want source.Kind
	}{
		{"compose", "services:\n  web:\n    image: busybox\n", "compose.yaml", source.KindCompose},
		{"native", `{"apiVersion":"grillo.dev/v1alpha1","kind":"Application","metadata":{"name":"x"}}`, "app.json", source.KindNative},
		{"kubernetes", "apiVersion: apps/v1\nkind: Deployment\n", "deploy.yaml", source.KindKubernetes},
		{"chart", "apiVersion: v2\nname: x\n", "Chart.yaml", source.KindHelm},
		{"empty", "", "app.json", source.KindNative},
	}
	for _, tc := range cases {
		got, err := Format([]byte(tc.data), tc.path)
		if err != nil {
			t.Errorf("%s: %v", tc.name, err)
			continue
		}
		if got != tc.want {
			t.Errorf("%s: got %q want %q", tc.name, got, tc.want)
		}
	}
}

func TestFormatAmbiguousAndUnknown(t *testing.T) {
	if _, err := Format([]byte("services:\n  a:\napiVersion: x\nkind: y\n"), "x.yaml"); err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("ambiguous err = %v", err)
	}
	if _, err := Format([]byte("foo: bar\n"), "x.yaml"); err == nil {
		t.Fatal("unknown input did not error")
	}
	if _, err := Format([]byte("[1,2,3]\n"), "x.yaml"); err == nil {
		t.Fatal("non-mapping input did not error")
	}
}
