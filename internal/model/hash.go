// SPDX-License-Identifier: Apache-2.0

package model

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
)

// stripVolatile returns a deep copy with runtime status and source locations
// removed: the source descriptor, the runtime-assigned identity ID, and the
// observed route endpoint. Secret reference versions are kept so secret changes
// are visible, but secret values never appear in the IR.
func stripVolatile(app Application) (Application, error) {
	clone, err := cloneApplication(app)
	if err != nil {
		return Application{}, err
	}
	clone.Source = nil
	clone.Identity.ID = ""
	for i := range clone.Routes {
		clone.Routes[i].Endpoint = ""
	}
	return clone, nil
}

// canonicalBytes returns deterministic compact JSON for hashing.
func canonicalBytes(app Application) ([]byte, error) {
	stripped, err := stripVolatile(app)
	if err != nil {
		return nil, err
	}
	Normalize(&stripped)
	return json.Marshal(stripped)
}

// CanonicalJSON returns indented canonical JSON for inspection and golden tests.
func CanonicalJSON(app Application) ([]byte, error) {
	stripped, err := stripVolatile(app)
	if err != nil {
		return nil, err
	}
	Normalize(&stripped)
	return json.MarshalIndent(stripped, "", "  ")
}

// Hash returns a stable "sha256:<hex>" digest of the canonical application. Two
// applications with equal hashes must produce identical runtime plans.
func Hash(app Application) (string, error) {
	data, err := canonicalBytes(app)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

func cloneApplication(app Application) (Application, error) {
	data, err := json.Marshal(app)
	if err != nil {
		return Application{}, fmt.Errorf("clone application: %w", err)
	}
	var out Application
	if err := json.Unmarshal(data, &out); err != nil {
		return Application{}, fmt.Errorf("clone application: %w", err)
	}
	return out, nil
}
