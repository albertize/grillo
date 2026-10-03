//go:build linux

// SPDX-License-Identifier: Apache-2.0

package network

import (
	"errors"
	"testing"
)

func TestPeersAllowed(t *testing.T) {
	sameA := NetworkAttachment{Application: "app-a"}
	sameB := NetworkAttachment{Application: "app-a"}
	other := NetworkAttachment{Application: "app-b"}
	if !PeersAllowed(sameA, sameB) {
		t.Fatal("sandboxes in the same application must communicate")
	}
	if PeersAllowed(sameA, other) {
		t.Fatal("sandboxes in different applications must be isolated")
	}
	if PeersAllowed(NetworkAttachment{}, sameA) {
		t.Fatal("an unattached sandbox must not be allowed")
	}
}

func TestValidateRejectsMultipleNetworks(t *testing.T) {
	if err := Validate([]NetworkAttachment{{Application: "app", Networks: []string{"a", "b"}}}); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("err = %v, want ErrUnsupported", err)
	}
	if err := Validate([]NetworkAttachment{{Application: "app", Networks: []string{"a"}}}); err != nil {
		t.Fatalf("single network rejected: %v", err)
	}
}
