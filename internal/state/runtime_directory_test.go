// SPDX-License-Identifier: Apache-2.0

package state

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPrepareRuntimeDirectoryCreatesPrivateAndRejectsUnsafePaths(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "runtime/grillo")
	if err := PrepareRuntimeDirectory(dir); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(dir)
	if err != nil || info.Mode().Perm() != 0700 {
		t.Fatal(info, err)
	}
	if err := PrepareRuntimeDirectory(dir); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := PrepareRuntimeDirectory(dir); err == nil {
		t.Fatal("public runtime directory accepted")
	}
	info, _ = os.Stat(dir)
	if info.Mode().Perm() != 0755 {
		t.Fatal("permission remediation was automatic")
	}
	link := filepath.Join(root, "link")
	if err := os.Symlink(filepath.Join(root, "runtime"), link); err != nil {
		t.Fatal(err)
	}
	if err := PrepareRuntimeDirectory(filepath.Join(link, "new")); err == nil {
		t.Fatal("symlink ancestor accepted")
	}
	if _, err := os.Stat(filepath.Join(root, "runtime/new")); !os.IsNotExist(err) {
		t.Fatal("unsafe ancestor created files")
	}
}
