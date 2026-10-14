//go:build linux

// SPDX-License-Identifier: Apache-2.0

package guest

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/albertize/grillo/internal/guestproto"
)

func TestAgentOwnedExportBoundsMetadataAndTrailer(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "file"), []byte("abcd"), 0640); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/container/path", filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	result, err := exportBuildRoot(context.Background(), root, 32768, 2, &output)
	if err != nil || result.Entries != 2 || result.Bytes != int64(output.Len()) {
		t.Fatal("export", result, err)
	}
	sum := sha256.Sum256(output.Bytes())
	if result.SHA256 != hex.EncodeToString(sum[:]) {
		t.Fatal("archive receipt mismatch")
	}
	tr := tar.NewReader(bytes.NewReader(output.Bytes()))
	seen := map[string]*tar.Header{}
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		seen[header.Name] = header
		if header.Name == "file" {
			body, err := io.ReadAll(tr)
			if err != nil || string(body) != "abcd" || header.Mode != 0640 {
				t.Fatal("file metadata/body", err)
			}
		}
	}
	if seen["link"] == nil || seen["link"].Linkname != "/container/path" {
		t.Fatal("symlink metadata not preserved")
	}
	for _, limit := range []int64{result.Bytes, result.Bytes - 1} {
		var out bytes.Buffer
		_, err := exportBuildRoot(context.Background(), root, limit, 2, &out)
		if limit == result.Bytes && err != nil {
			t.Fatal("exact archive size", err)
		}
		if limit < result.Bytes && !errors.Is(err, errExportLimit) {
			t.Fatal("trailer escaped bound", err)
		}
		if int64(out.Len()) > limit {
			t.Fatal("wrote past byte limit")
		}
	}
	if _, err := exportBuildRoot(context.Background(), root, 32768, 1, io.Discard); !errors.Is(err, errExportLimit) {
		t.Fatal("entry limit", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := exportBuildRoot(ctx, root, 32768, 2, io.Discard); !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation", err)
	}
	if err := os.WriteFile(filepath.Join(root, ".wh.reserved"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := exportBuildRoot(context.Background(), root, 32768, 3, io.Discard); err == nil {
		t.Fatal("reserved whiteout name exported as snapshot")
	}
}

func exportAgentPair(t *testing.T, agent *Agent) *guestproto.Client {
	t.Helper()
	serverConn, clientConn := net.Pipe()
	cfg := guestproto.HandshakeConfig{Sandbox: "export-test", Key: testAgentKey(), Capabilities: []guestproto.Capability{guestproto.CapBuildExport}}
	ready := make(chan *guestproto.Server, 1)
	go func() {
		server, err := guestproto.NewServer(context.Background(), serverConn, cfg, agent)
		if err != nil {
			return
		}
		ready <- server
		_ = server.Serve(context.Background())
	}()
	client, err := guestproto.NewClient(context.Background(), clientConn, cfg)
	if err != nil {
		t.Fatal(err)
	}
	server := <-ready
	t.Cleanup(func() { _ = client.Close(); _ = server.Close() })
	return client
}

func TestBuildExportAgentSelectionAndBusyContracts(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "file"), []byte("abcd"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, scenario := range []string{"success", "ordinary", "busy", "unknown", "not-started"} {
		t.Run(scenario, func(t *testing.T) {
			agent := NewAgent(newFakeRuntime(), t.TempDir())
			agent.started = true
			agent.sandbox = guestproto.SandboxSpec{ID: "build", Build: true, Shares: []guestproto.ShareSpec{{Tag: "build", Target: root}}}
			switch scenario {
			case "ordinary":
				agent.sandbox.Build = false
			case "busy":
				agent.buildBusy = true
			case "not-started":
				agent.started = false
			}
			client := exportAgentPair(t, agent)
			share := "build"
			if scenario == "unknown" {
				share = "/etc"
			}
			var output bytes.Buffer
			result, err := client.Export(context.Background(), guestproto.ExportRequest{Share: share, MaxBytes: 32768, MaxEntries: 10}, &output)
			if scenario == "success" {
				if err != nil || result.Entries != 1 || output.Len() == 0 {
					t.Fatal("authenticated agent export", err)
				}
			} else if err == nil || output.Len() != 0 {
				t.Fatal("invalid selection exported bytes", err)
			}
		})
	}
}

type failingBuildCleanup struct{ *fakeRuntime }

func (*failingBuildCleanup) Delete(context.Context, string, bool) error {
	return errors.New("cleanup failed")
}

func TestBuildRunCleanupFailureBlocksExport(t *testing.T) {
	agent := NewAgent(&failingBuildCleanup{newFakeRuntime()}, t.TempDir())
	agent.started = true
	agent.sandbox = guestproto.SandboxSpec{ID: "build", Build: true, Shares: []guestproto.ShareSpec{{Tag: "build", Target: t.TempDir()}}}
	raw, _ := guestproto.MarshalPayload(guestproto.RunRequest{Share: "build", Args: []string{"true"}})
	result, perr := agent.run(context.Background(), guestproto.Message{Payload: raw}, nil)
	if result != nil || perr == nil || !strings.Contains(perr.Message, "cleanup failed") || !agent.buildBusy {
		t.Fatal("success overrode failed cleanup", result, perr)
	}
	client := exportAgentPair(t, agent)
	if _, err := client.Export(context.Background(), guestproto.ExportRequest{Share: "build", MaxBytes: 32768, MaxEntries: 10}, io.Discard); err == nil {
		t.Fatal("export allowed after failed RUN cleanup")
	}
}
