// SPDX-License-Identifier: Apache-2.0

package dockerfile

import "testing"

func TestIgnorePatterns(t *testing.T) {
	ignore := ParseIgnore([]byte(`
# comment
*.md
!keep.md
vendor/
**/node_modules
/root-only.txt
docs/**/*.png
`))
	cases := []struct {
		path     string
		excluded bool
	}{
		{"README.md", true},
		{"docs/README.md", false},
		{"keep.md", false},
		{"vendor/lib/x.go", true},
		{"src/vendor/x", false},
		{"node_modules/x.js", true},
		{"a/b/node_modules/x.js", true},
		{"root-only.txt", true},
		{"sub/root-only.txt", false},
		{"docs/img/a.png", true},
		{"docs/a.png", true},
		{"src/a.go", false},
	}
	for _, tc := range cases {
		if got := ignore.Excluded(tc.path); got != tc.excluded {
			t.Errorf("Excluded(%q) = %v want %v", tc.path, got, tc.excluded)
		}
	}
}

func TestIgnoreEmpty(t *testing.T) {
	if !(*Ignore)(nil).Empty() {
		t.Fatal("nil ignore should be empty")
	}
	if !ParseIgnore(nil).Empty() {
		t.Fatal("empty ignore should be empty")
	}
	if (*Ignore)(nil).Excluded("anything") {
		t.Fatal("nil ignore should exclude nothing")
	}
}

func TestIgnoreNegationReincludes(t *testing.T) {
	ignore := ParseIgnore([]byte("*.log\n!important.log\n"))
	if !ignore.Excluded("debug.log") {
		t.Fatal("debug.log should be excluded")
	}
	if ignore.Excluded("important.log") {
		t.Fatal("important.log should be re-included")
	}
}
