// SPDX-License-Identifier: Apache-2.0

package kubernetes

import (
	"context"
	"testing"
)

// FuzzCompile exercises YAML decoding and the compiler without host effects.
func FuzzCompile(f *testing.F) {
	for _, seed := range []string{
		"apiVersion: v1\nkind: Pod\nmetadata: {name: demo}\nspec:\n  containers: [{name: app, image: busybox:1.37}]\n",
		"apiVersion: v1\nkind: List\nitems: []\n",
		"a: &a [*a]\n", "---\n[]\n---\nnull\n", "",
	} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		// Bound each campaign input independently of production parser limits.
		if len(data) > 64<<10 {
			t.Skip()
		}
		_, _ = Compile(context.Background(), data, Options{Path: "fuzz.yaml"})
	})
}
