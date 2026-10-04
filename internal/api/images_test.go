//go:build linux

// SPDX-License-Identifier: Apache-2.0

package api

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/albertize/grillo/internal/build"
	"github.com/albertize/grillo/internal/image"
)

type fakeImages struct {
	records []image.Record
	pinned  map[string]bool
	built   build.Request
}

func (f *fakeImages) Images(context.Context) ([]image.Record, error) { return f.records, nil }

func (f *fakeImages) InspectImage(_ context.Context, reference string) (image.Record, error) {
	for _, record := range f.records {
		if record.Reference == reference {
			return record, nil
		}
	}
	return image.Record{}, fmt.Errorf("%w: %s", image.ErrNotFound, reference)
}

func (f *fakeImages) PruneImages(context.Context, []string) (image.PruneResult, error) {
	return image.PruneResult{RemovedBlobs: 1}, nil
}

func (f *fakeImages) PinImage(_ context.Context, reference string, pinned bool) error {
	if f.pinned == nil {
		f.pinned = map[string]bool{}
	}
	f.pinned[reference] = pinned
	return nil
}

func (f *fakeImages) Build(_ context.Context, request build.Request, progress func(string)) (build.Result, error) {
	f.built = request
	if progress != nil {
		progress("building")
	}
	return build.Result{Reference: request.Reference, ManifestDigest: "sha256:built"}, nil
}

func TestImageInventoryEndpoints(t *testing.T) {
	images := &fakeImages{records: []image.Record{{
		Reference:      "grillo.local/app:dev",
		ManifestDigest: "sha256:abc",
		Source:         image.SourceBuild,
		Size:           11,
	}}}
	socket, stop := startServer(t, Options{Core: &fakeCore{}, Images: images})
	defer stop()
	client := NewClient(socket)
	ctx := context.Background()

	list, err := client.Images(ctx)
	if err != nil || len(list) != 1 || list[0].Reference != "grillo.local/app:dev" {
		t.Fatalf("images = %+v err=%v", list, err)
	}
	record, err := client.InspectImage(ctx, "grillo.local/app:dev")
	if err != nil || record.ManifestDigest != "sha256:abc" {
		t.Fatalf("inspect = %+v err=%v", record, err)
	}
	if _, err := client.InspectImage(ctx, "missing:latest"); err == nil {
		t.Fatal("inspect of a missing image succeeded")
	}
	if err := client.PinImage(ctx, "grillo.local/app:dev", true); err != nil {
		t.Fatal(err)
	}
	if !images.pinned["grillo.local/app:dev"] {
		t.Fatal("pin not delivered")
	}
}

func TestBuildEndpointIsAsync(t *testing.T) {
	images := &fakeImages{}
	socket, stop := startServer(t, Options{Core: &fakeCore{}, Images: images})
	defer stop()
	client := NewClient(socket)
	ctx := context.Background()

	contextDir := t.TempDir()
	id, err := client.Build(ctx, build.Request{ContextDir: contextDir, Reference: "grillo.local/built:latest", Network: "none"})
	if err != nil {
		t.Fatal(err)
	}
	waitOperationState(t, ctx, client, id, "succeeded")
	if images.built.Reference != "grillo.local/built:latest" || images.built.Network != "none" {
		t.Fatalf("build request = %+v", images.built)
	}

	// Invalid requests are rejected synchronously.
	if _, err := client.Build(ctx, build.Request{ContextDir: contextDir}); err == nil {
		t.Fatal("build without a reference was accepted")
	}
}

func TestPruneEndpoint(t *testing.T) {
	images := &fakeImages{}
	socket, stop := startServer(t, Options{Core: &fakeCore{}, Images: images})
	defer stop()
	client := NewClient(socket)
	ctx := context.Background()
	id, err := client.PruneImages(ctx, []string{"grillo.local/app:dev"})
	if err != nil {
		t.Fatal(err)
	}
	waitOperationState(t, ctx, client, id, "succeeded")
}

func waitOperationState(t *testing.T, ctx context.Context, client *Client, id, want string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		operation, err := client.Operation(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if operation.State == want {
			return
		}
		if operation.State == "failed" || operation.State == "canceled" {
			t.Fatalf("operation %s ended as %s", id, operation.State)
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("operation %s did not reach %s", id, want)
}
