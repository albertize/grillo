//go:build linux

// SPDX-License-Identifier: Apache-2.0

package build

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"testing"

	"github.com/albertize/grillo/internal/guestproto"
)

func blockFormatter(t *testing.T) string {
	t.Helper()
	name, err := exec.LookPath("mke2fs")
	if err != nil {
		t.Skip("reviewed formatter unavailable")
	}
	name, err = filepath.Abs(name)
	if err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(name, "-V").CombinedOutput()
	if err != nil || !bytes.HasPrefix(out, []byte("mke2fs 1.47.3 ")) {
		t.Skip("reviewed formatter 1.47.3 unavailable")
	}
	return name
}

type logicalBuildFile struct {
	body     string
	uid, gid int
	mode     int64
}
type blockFixtureClient struct {
	files     map[string]logicalBuildFile
	root      guestproto.BuildRootMetadata
	exit      int
	exportErr error
}

func (c *blockFixtureClient) Start(context.Context, guestproto.StartRequest) (guestproto.StartResult, error) {
	return guestproto.StartResult{State: "ready"}, nil
}
func (c *blockFixtureClient) Close() error { return nil }
func (c *blockFixtureClient) Run(_ context.Context, req guestproto.RunRequest, _, _ io.Writer) (guestproto.RunResult, error) {
	if req.Args[0] == "first" {
		delete(c.files, "first")
		c.files["generated"] = logicalBuildFile{"made", 1234, 1235, 0750}
		c.files["overwritten"] = logicalBuildFile{"old", 4444, 4445, 0644}
		c.root = guestproto.BuildRootMetadata{UID: 2000, GID: 2001, Mode: 0750}
	}
	return guestproto.RunResult{ExitCode: c.exit}, nil
}
func (c *blockFixtureClient) Export(_ context.Context, _ guestproto.ExportRequest, w io.Writer) (guestproto.ExportResult, error) {
	var out bytes.Buffer
	tw := tar.NewWriter(&out)
	names := make([]string, 0, len(c.files))
	for name := range c.files {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		f := c.files[name]
		if err := tw.WriteHeader(&tar.Header{Name: name, Typeflag: tar.TypeReg, Size: int64(len(f.body)), Mode: f.mode, Uid: f.uid, Gid: f.gid}); err != nil {
			return guestproto.ExportResult{}, err
		}
		if _, err := io.WriteString(tw, f.body); err != nil {
			return guestproto.ExportResult{}, err
		}
	}
	if err := tw.Close(); err != nil {
		return guestproto.ExportResult{}, err
	}
	if _, err := w.Write(out.Bytes()); err != nil {
		return guestproto.ExportResult{}, err
	}
	sum := sha256.Sum256(out.Bytes())
	return guestproto.ExportResult{Bytes: int64(out.Len()), Entries: int64(len(names)), SHA256: hex.EncodeToString(sum[:]), Root: &c.root}, c.exportErr
}

func readBlockFixture(t *testing.T, input string, seed *guestproto.BuildSeedSpec) *blockFixtureClient {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(input, "root.tar"))
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	if int64(len(data)) != seed.Bytes || hex.EncodeToString(sum[:]) != seed.SHA256 {
		t.Fatal("seed receipt")
	}
	c := &blockFixtureClient{files: map[string]logicalBuildFile{}}
	tr := tar.NewReader(bytes.NewReader(data))
	var count int64
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		count++
		if h.Name == "." {
			c.root = guestproto.BuildRootMetadata{UID: uint32(h.Uid), GID: uint32(h.Gid), Mode: uint32(h.Mode)}
			continue
		}
		body, err := io.ReadAll(tr)
		if err != nil {
			t.Fatal(err)
		}
		c.files[h.Name] = logicalBuildFile{string(body), h.Uid, h.Gid, h.Mode}
	}
	if count != seed.Entries {
		t.Fatal("seed entry count")
	}
	return c
}

func TestNativeBlockRunInterleavesCopyAndAdoptsMetadata(t *testing.T) {
	b, cas, puller := newNativeBuilder(t, nil)
	boots, cleanups := 0, 0
	runner := &BlockRunner{Formatter: blockFormatter(t), ImageBytes: 32 << 20}
	runner.Boot = func(_ context.Context, disk, input string, seed *guestproto.BuildSeedSpec, network bool) (SandboxClient, func() error, error) {
		boots++
		if network {
			t.Fatal("offline request changed")
		}
		if err := verifyWorkspaceDisk(disk, 32<<20); err != nil {
			t.Fatal(err)
		}
		c := readBlockFixture(t, input, seed)
		if boots == 1 {
			if c.files["first"].uid != 1000 {
				t.Fatal("COPY ownership lost")
			}
		}
		if boots == 2 {
			if c.files["overwritten"].uid != 0 || c.files["overwritten"].body != "second" {
				t.Fatal("stale owner survived context COPY", c.files)
			}
			if c.root.UID != 2000 || c.root.GID != 2001 || c.root.Mode != 0750 {
				t.Fatal("root metadata lost", c.root)
			}
			if cleanups != 1 || c.files["second"].body != "second" || c.files["generated"].uid != 1234 {
				t.Fatal("COPY/RUN state or cleanup ordering lost", c.files)
			}
			if _, ok := c.files["first"]; ok {
				t.Fatal("deleted file returned")
			}
		}
		return c, func() error { cleanups++; return nil }, nil
	}
	b.Runner = runner
	root := writeContext(t, map[string]string{"Dockerfile": "FROM scratch AS build\nCOPY --chown=1000:1001 first /first\nRUN [\"first\"]\nCOPY second /second\nCOPY second /overwritten\nRUN [\"second\"]\nFROM build\nCOPY --from=build /generated /copied\n", "first": "first", "second": "second"})
	result, err := b.Build(context.Background(), Request{ContextDir: root, Reference: "grillo.local/block:1", Network: "none"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if boots != 2 || cleanups != 2 {
		t.Fatal("lifecycle counts", boots, cleanups)
	}
	imageRoot := unpackImage(t, puller, result.Image)
	if readFile(t, imageRoot, "copied") != "made" {
		t.Fatal("output missing")
	}
	if _, err := os.Lstat(filepath.Join(imageRoot, "first")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("deletion was not retained")
	}
	last := result.Image.Manifest.Layers[len(result.Image.Manifest.Layers)-1]
	f, err := cas.Open(last.Digest)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	found := false
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if h.Name == "copied" {
			found = true
			if h.Uid != 1234 || h.Gid != 1235 {
				t.Fatal("COPY --from ownership lost", h)
			}
		}
	}
	if !found {
		t.Fatal("copied file metadata missing")
	}
}

func TestBlockRunFailureCannotPublishAndRetainsLiveDisk(t *testing.T) {
	for _, scenario := range []string{"boot", "run", "export", "cleanup"} {
		t.Run(scenario, func(t *testing.T) {
			b, _, _ := newNativeBuilder(t, nil)
			runner := &BlockRunner{Formatter: blockFormatter(t), ImageBytes: 32 << 20}
			var diskPath string
			runner.Boot = func(_ context.Context, disk, input string, seed *guestproto.BuildSeedSpec, _ bool) (SandboxClient, func() error, error) {
				diskPath = disk
				if scenario == "boot" {
					return nil, nil, errors.New("boot failed before ownership")
				}
				c := readBlockFixture(t, input, seed)
				if scenario == "run" {
					c.exit = 1
				}
				if scenario == "export" {
					c.exportErr = io.ErrUnexpectedEOF
				}
				cleanup := func() error {
					if scenario == "cleanup" {
						if _, err := os.Stat(disk); err != nil {
							t.Fatal("live disk deleted")
						}
						return errors.New("stop failed")
					}
					return nil
				}
				return c, cleanup, nil
			}
			b.Runner = runner
			root := writeContext(t, map[string]string{"Dockerfile": "FROM scratch\nRUN [\"first\"]\n"})
			ref := "grillo.local/failure:1"
			if _, err := b.Build(context.Background(), Request{ContextDir: root, Reference: ref, Network: "none"}, nil); err == nil {
				t.Fatal("failure reported success")
			}
			if _, ok, err := b.Store.Get(ref); err != nil || ok {
				t.Fatal("failed build published", err)
			}
			_, statErr := os.Stat(diskPath)
			if scenario == "cleanup" {
				if statErr != nil || !runner.PreserveScratch() {
					t.Fatal("failed cleanup lost retained disk", statErr)
				}
			} else if !errors.Is(statErr, os.ErrNotExist) {
				t.Fatal("owned temporary disk leaked", statErr)
			}
		})
	}
}

func TestBlockImageReservationRefusesBeforeBoot(t *testing.T) {
	b, _, _ := newNativeBuilder(t, nil)
	runner := &BlockRunner{Formatter: blockFormatter(t), ImageBytes: 5 << 30, Boot: func(context.Context, string, string, *guestproto.BuildSeedSpec, bool) (SandboxClient, func() error, error) {
		t.Fatal("over-budget disk booted")
		return nil, nil, nil
	}}
	b.Runner = runner
	root := writeContext(t, map[string]string{"Dockerfile": "FROM scratch\nRUN [\"first\"]\n"})
	if _, err := b.Build(context.Background(), Request{ContextDir: root, Reference: "grillo.local/reservation:1"}, nil); !errors.Is(err, ErrWorkspaceLimit) {
		t.Fatal("image reservation", err)
	}
	if entries, err := os.ReadDir(b.ScratchDir); err != nil || len(entries) != 0 {
		t.Fatal("rejected image left workspace", err)
	}
}

func TestBlockRunOutputIsBoundedEvenWithoutProgressConsumer(t *testing.T) {
	w := &blockRunOutput{ctx: context.Background(), remaining: 4}
	if n, err := w.Write([]byte("abcd")); err != nil || n != 4 {
		t.Fatal("exact output limit", err)
	}
	if n, err := w.Write([]byte("x")); err == nil || n != 0 {
		t.Fatal("output bypassed bound", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	w = &blockRunOutput{ctx: ctx, remaining: 4}
	if _, err := w.Write([]byte("x")); !errors.Is(err, context.Canceled) {
		t.Fatal("output cancellation", err)
	}
}
