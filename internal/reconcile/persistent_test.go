// SPDX-License-Identifier: Apache-2.0

package reconcile

import (
	"errors"
	"grillo.local/grillo/internal/model"
	"grillo.local/grillo/internal/plan"
	"os"
	"path/filepath"
	"testing"
)

func TestPersistentIntentProgressAndStoppedSurviveReopen(t *testing.T) {
	dir := t.TempDir()
	s, err := OpenPersistentStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	app := testApp("app", 1, "v1")
	if err := s.SetDesired(app); err != nil {
		t.Fatal(err)
	}
	obs := plan.Observed{Sandboxes: map[string]plan.ObservedSandbox{"web-0": {Workload: "web"}}, Volumes: map[string]bool{"data": true}}
	if err := s.Save("app", obs); err != nil {
		t.Fatal(err)
	}
	if err := s.SetStopped("app", true); err != nil {
		t.Fatal(err)
	}
	reopened, err := OpenPersistentStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	r := reopened.Records()[0]
	if !r.Stopped || !r.RemoveVolumes || !r.Observed.Volumes["data"] || len(r.Observed.Sandboxes) != 1 {
		t.Fatalf("%+v", r)
	}
	info, err := os.Stat(filepath.Join(dir, "applications.json"))
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("%v %v", info, err)
	}
	r.Desired.Identity.Name = "modified"
	if reopened.Records()[0].Desired.Identity.Name != "app" {
		t.Fatal("state aliases caller")
	}
}

func TestPersistentCommitFailureDoesNotPublishIntent(t *testing.T) {
	dir := t.TempDir()
	s, err := OpenPersistentStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetDesired(testApp("app", 1, "v1")); err != nil {
		t.Fatal(err)
	}
	s.ops.Rename = func(string, string) error { return errors.New("disk failure") }
	if err := s.SetDesired(model.Application{Identity: model.Identity{Name: "other"}}); err == nil {
		t.Fatal("commit succeeded")
	}
	if len(s.Records()) != 1 {
		t.Fatal("failed state published in memory")
	}
	reopened, err := OpenPersistentStore(dir)
	if err != nil || len(reopened.Records()) != 1 {
		t.Fatalf("%v", err)
	}
}

func TestPersistentRejectsFutureAndCorruptState(t *testing.T) {
	for _, data := range []string{`{"version":99,"applications":{}}`, `{broken`, `{"version":1,"applications":{"a":{"desired":{"metadata":{"name":"b"}}}}}`} {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "applications.json"), []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := OpenPersistentStore(dir); err == nil {
			t.Fatalf("accepted %s", data)
		}
	}
}
