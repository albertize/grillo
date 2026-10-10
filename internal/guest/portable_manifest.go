// SPDX-License-Identifier: Apache-2.0

package guest

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"

	"github.com/albertize/grillo/internal/guestproto"
)

// ManifestSchemaVersion identifies the portable boot inventory, not an SBOM.
const ManifestSchemaVersion = 1
const GuestPlatform = "linux/amd64"

func (m Manifest) validateFormat() error {
	switch m.SchemaVersion {
	case 0:
		if m.GuestABI != "" || m.Platform != "" {
			return fmt.Errorf("guest: ABI/platform metadata requires manifest schema_version 1")
		}
	case ManifestSchemaVersion:
		if m.GuestABI != guestproto.CurrentVersion.String() {
			return fmt.Errorf("guest: incompatible guest ABI %q; require %s", m.GuestABI, guestproto.CurrentVersion.String())
		}
		if m.Platform != GuestPlatform {
			return fmt.Errorf("guest: incompatible platform %q; require %s", m.Platform, GuestPlatform)
		}
	default:
		return fmt.Errorf("guest: unsupported manifest schema_version %d", m.SchemaVersion)
	}
	return nil
}

func validatePortablePath(path string) error {
	if !fs.ValidPath(path) || path == "." || strings.ContainsAny(path, "\\\x00") || strings.Contains(path, ":") {
		return fmt.Errorf("guest: artifact path must be canonical manifest-relative slash-separated path")
	}
	return nil
}

// ReadBootManifest requires versioned metadata for installed assets. A caller
// must explicitly permit legacy development inventories; future/invalid schemas
// or mismatched ABI/platform never become legacy fallbacks.
func ReadBootManifest(path string, allowLegacy bool) (Manifest, error) {
	m, err := ReadManifest(path)
	if err != nil {
		return Manifest{}, err
	}
	if m.SchemaVersion == 0 && !allowLegacy {
		return Manifest{}, fmt.Errorf("guest: installed assets require portable manifest schema_version 1; legacy inventory is development-only")
	}
	return m, nil
}

// BuildPortableManifest records only boot artifacts staged underneath directory.
// It never copies/downloads files. Serialized paths remain relative after moving
// the directory; the host anchor is private in-memory state.
func BuildPortableManifest(directory string, specs []ArtifactSpec) (Manifest, error) {
	if len(specs) != 2 {
		return Manifest{}, fmt.Errorf("guest: portable boot inventory requires exactly two artifacts")
	}
	names := map[string]bool{}
	for _, spec := range specs {
		if (spec.Name != "kernel" && spec.Name != "initramfs") || names[spec.Name] || spec.Version == "" {
			return Manifest{}, fmt.Errorf("guest: invalid portable boot artifact specification")
		}
		names[spec.Name] = true
	}
	directory, err := filepath.Abs(directory)
	if err != nil {
		return Manifest{}, err
	}
	m := Manifest{SchemaVersion: ManifestSchemaVersion, GuestABI: guestproto.CurrentVersion.String(), Platform: GuestPlatform}
	for _, spec := range specs {
		source, err := filepath.Abs(spec.Path)
		if err != nil {
			return Manifest{}, err
		}
		relative, err := filepath.Rel(directory, source)
		if err != nil {
			return Manifest{}, err
		}
		relative = filepath.ToSlash(relative)
		if err := validatePortablePath(relative); err != nil {
			return Manifest{}, err
		}
		a := Artifact{Name: spec.Name, Version: spec.Version, Path: relative, directory: directory}
		file, err := a.Open()
		if err != nil {
			return Manifest{}, fmt.Errorf("guest: open staged %s: %w", a.Name, err)
		}
		info, err := file.Stat()
		file.Close()
		if err != nil || info.Size() <= 0 || info.Size() > 2<<30 {
			return Manifest{}, fmt.Errorf("guest: invalid staged artifact size")
		}
		a.Size = info.Size()
		sum, size, err := a.hash()
		if err != nil {
			return Manifest{}, fmt.Errorf("guest: hash staged %s: %w", a.Name, err)
		}
		if size != a.Size {
			return Manifest{}, fmt.Errorf("guest: staged artifact changed while hashing")
		}
		a.SHA256 = sum
		m.Artifacts = append(m.Artifacts, a)
	}
	// Keep legacy and portable builders equally deterministic.
	sort.SliceStable(m.Artifacts, func(i, j int) bool { return m.Artifacts[i].Name < m.Artifacts[j].Name })
	if err := m.Validate(); err != nil {
		return Manifest{}, err
	}
	return m, nil
}

// HostPath identifies the selected file for backend path matching. Opening a
// portable artifact must use Open, not this pathname, to retain root confinement.
func (a Artifact) HostPath() string {
	if a.directory != "" {
		return filepath.Join(a.directory, filepath.FromSlash(a.Path))
	}
	return a.Path
}

// Open rejects special files and symlinks. Portable reads are confined with
// os.Root even if a path component changes during traversal. Hash verification
// remains mandatory: confinement is not publisher authentication.
func (a Artifact) Open() (*os.File, error) {
	if a.directory == "" {
		return openRegular(a.Path)
	}
	if err := validatePortablePath(a.Path); err != nil {
		return nil, err
	}
	if err := rejectDirectorySymlinks(a.directory); err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(a.directory)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	relative := filepath.FromSlash(a.Path)
	for p := relative; p != "."; p = filepath.Dir(p) {
		info, err := root.Lstat(p)
		if err != nil {
			return nil, err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("guest: symlink in artifact path")
		}
	}
	file, err := root.OpenFile(relative, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	return requireRegular(file)
}

func rejectDirectorySymlinks(directory string) error {
	for p := directory; ; p = filepath.Dir(p) {
		info, err := os.Lstat(p)
		if err != nil {
			return err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("guest: unsafe manifest directory")
		}
		if p == filepath.Dir(p) {
			return nil
		}
	}
}

func openRegular(path string) (*os.File, error) {
	file, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	return requireRegular(file)
}

func requireRegular(file *os.File) (*os.File, error) {
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		file.Close()
		return nil, fmt.Errorf("guest: expected regular file")
	}
	return file, nil
}

func (a Artifact) hash() (string, int64, error) {
	if a.Size < 0 || a.Size > 2<<30 {
		return "", 0, fmt.Errorf("guest: invalid artifact size")
	}
	file, err := a.Open()
	if err != nil {
		return "", 0, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || info.Size() != a.Size {
		return "", 0, fmt.Errorf("guest: artifact size mismatch")
	}
	hash := sha256.New()
	size, err := io.Copy(hash, io.LimitReader(file, a.Size+1))
	if err != nil {
		return "", size, err
	}
	return hex.EncodeToString(hash.Sum(nil)), size, nil
}
