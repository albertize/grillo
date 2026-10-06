//go:build linux

// SPDX-License-Identifier: Apache-2.0

package executor

import (
	"bytes"
	"context"
	"io"
	"math"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/albertize/grillo/internal/guestproto"
	"github.com/albertize/grillo/internal/model"
	"github.com/albertize/grillo/internal/plan"
)

func TestCPUQuotaPrecisionAndOverflow(t *testing.T) {
	for _, milli := range []int64{1, 10, 100, 500, 1000, 2500, math.MaxInt64 / 1000} {
		got, err := resources(model.Resources{Limits: model.ResourceList{CPU: model.Millicores(milli)}})
		if err != nil || got.CPUQuotaMicros != milli*1000 || got.CPUPeriodMicros != 1000000 {
			t.Fatalf("%d: %+v %v", milli, got, err)
		}
	}
	for _, milli := range []int64{-1, math.MaxInt64/1000 + 1} {
		if _, err := resources(model.Resources{Limits: model.ResourceList{CPU: model.Millicores(milli)}}); err == nil {
			t.Fatalf("accepted %d", milli)
		}
	}
}

func TestSecurityMappingAndRejection(t *testing.T) {
	uid, gid := int64(1000), int64(2000)
	p := &model.SecurityProfile{RunAsUser: &uid, RunAsGroup: &gid, ReadOnlyRootFilesystem: true}
	c := model.Container{Name: "app", Command: &[]string{"/bin/app"}, SecurityProfile: p}
	spec, err := (&Executor{}).containerSpec(model.Application{}, c, "/root", nil)
	if err != nil || spec.User.UID != 1000 || spec.User.GID != 2000 || !spec.ReadOnlyRootFilesystem {
		t.Fatalf("%+v %v", spec, err)
	}
	for _, p := range []model.SecurityProfile{{Privileged: true}, {SeccompProfile: "custom"}, {CapabilitiesDrop: []string{"ALL"}}, {RunAsUser: ptr(-1)}, {RunAsGroup: ptr(1 << 32)}} {
		c.SecurityProfile = &p
		if _, err := (&Executor{}).containerSpec(model.Application{}, c, "/root", nil); err == nil {
			t.Fatalf("accepted %+v", p)
		}
	}
	for _, s := range []string{"-1", "4294967296", "1:-2"} {
		if _, err := parseUser(s); err == nil {
			t.Fatalf("accepted %q", s)
		}
	}
}
func ptr(v int64) *int64 { return &v }

type reviewImages struct{}

func (reviewImages) Resolve(context.Context, model.ImageRef) (Image, error) {
	return Image{HostPath: "/cache/immutable"}, nil
}

func TestImageSharesAreReadOnlyAndRootsPrivate(t *testing.T) {
	e := &Executor{cfg: Config{Images: reviewImages{}}, nextCID: 20}
	w := model.Workload{ID: "w", Template: model.SandboxTemplate{Containers: []model.Container{{Name: "a", Command: &[]string{"x"}}, {Name: "b", Command: &[]string{"x"}}}}}
	host, guest, err := e.buildSpecs(context.Background(), "app", model.Application{}, w, plan.Descriptor{ID: "w-0"})
	if err != nil {
		t.Fatal(err)
	}
	for i, s := range host.Shares {
		if !s.ReadOnly || !guest.Shares[i].ReadOnly || !guest.Containers[i].PrivateRoot {
			t.Fatalf("unsafe share %+v", s)
		}
	}
	if guest.Containers[0].Rootfs == guest.Containers[1].Rootfs {
		t.Fatal("shared container target")
	}
}

func reviewClient(t *testing.T, output string) *guestproto.Client {
	t.Helper()
	a, b := net.Pipe()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	cfg := guestproto.HandshakeConfig{Key: bytes.Repeat([]byte{1}, 32)}
	done := make(chan *guestproto.Server, 1)
	go func() {
		s, err := guestproto.NewServer(ctx, a, cfg, guestproto.HandlerFunc(func(_ context.Context, m guestproto.Message, stream *guestproto.Stream) (any, *guestproto.Error) {
			var req guestproto.ExecRequest
			if err := guestproto.UnmarshalPayload(m.Payload, &req); err != nil {
				return nil, guestproto.Errorf(guestproto.CodeBadRequest, "bad request")
			}
			_, _ = stream.WriteStdout([]byte(output + ":" + req.Container))
			_ = stream.Exit(0)
			return guestproto.ExecResult{}, nil
		}))
		if err != nil {
			a.Close()
			done <- nil
			return
		}
		done <- s
		_ = s.Serve(ctx)
	}()
	c, err := guestproto.NewClient(ctx, b, cfg)
	if err != nil {
		t.Fatal(err)
	}
	s := <-done
	t.Cleanup(func() {
		c.Close()
		if s != nil {
			s.Close()
		}
	})
	return c
}

func TestExecSelectsUniqueTargetAndRejectsAmbiguity(t *testing.T) {
	e := &Executor{cfg: Config{Dial: func(_ context.Context, cid, _ uint32) (*guestproto.Client, error) {
		return reviewClient(t, map[uint32]string{1: "web", 2: "db", 3: "replica"}[cid]), nil
	}}, runtimes: map[string]*sandboxRuntime{
		"app-web-0": {app: "app", cid: 1, guest: reviewClient(t, "web"), containers: map[string]bool{"web": true}},
		"app-db-0":  {app: "app", cid: 2, guest: reviewClient(t, "db"), containers: map[string]bool{"db": true}},
	}}
	var out bytes.Buffer
	if _, err := e.Exec(context.Background(), "app", "db", []string{"x"}, &out, io.Discard); err != nil || out.String() != "db:db" {
		t.Fatalf("%q %v", out.String(), err)
	}
	e.runtimes["app-db-1"] = &sandboxRuntime{app: "app", cid: 3, guest: reviewClient(t, "replica"), containers: map[string]bool{"db": true}}
	if _, err := e.Exec(context.Background(), "app", "db", []string{"x"}, io.Discard, io.Discard); err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("%v", err)
	}
	out.Reset()
	if _, err := e.Exec(context.Background(), "app", "app-db-1/db", []string{"x"}, &out, io.Discard); err != nil || out.String() != "replica:db" {
		t.Fatalf("%q %v", out.String(), err)
	}
	if _, err := e.Exec(context.Background(), "other", "app-db-1/db", []string{"x"}, io.Discard, io.Discard); err == nil {
		t.Fatal("cross-application target accepted")
	}
}
