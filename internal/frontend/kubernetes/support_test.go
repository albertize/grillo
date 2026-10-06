// SPDX-License-Identifier: Apache-2.0

package kubernetes

import (
	"context"
	"testing"

	"github.com/albertize/grillo/internal/source"
)

func TestValidateOnlyKindsAndFieldsAreDiagnosed(t *testing.T) {
	for kind, version := range map[string]string{"ServiceAccount": "v1", "PodDisruptionBudget": "policy/v1"} {
		result, err := Compile(context.Background(), []byte("apiVersion: "+version+"\nkind: "+kind+"\nmetadata: {name: metadata-only}\n"), Options{})
		if err != nil || result.Diagnostics.HasErrors() {
			t.Fatalf("%s rejected: %v", kind, err)
		}
		if len(result.Diagnostics) != 1 || result.Diagnostics[0].Compatibility != source.ValidateOnly || result.Diagnostics[0].Consequence == "" {
			t.Fatalf("%s silently accepted", kind)
		}
		if len(result.Application.Workloads) != 0 {
			t.Fatal("metadata-only kind created workload")
		}
	}
	result, err := Compile(context.Background(), []byte("apiVersion: v1\nkind: Pod\nmetadata: {name: p}\nspec:\n  serviceAccountName: metadata-only\n  containers: [{name: app, image: busybox:1.37}]\n"), Options{})
	if err != nil || result.Diagnostics.HasErrors() {
		t.Fatal("Pod rejected", err)
	}
	found := false
	for _, d := range result.Diagnostics {
		if d.Field == "spec.serviceAccountName" && d.Compatibility == source.ValidateOnly {
			found = true
		}
	}
	if !found {
		t.Fatal("validate-only Pod field silently discarded")
	}
}

func TestSupportSnapshotCannotMutateValidator(t *testing.T) {
	entries := SupportEntries()
	for i := range entries {
		entries[i].State = source.Unsupported
	}
	for _, entry := range SupportEntries() {
		if entry.Scope == "kind" && entry.Field == "Pod" && entry.State != source.Supported {
			t.Fatal("snapshot mutated validator")
		}
	}
}
