// SPDX-License-Identifier: Apache-2.0

package qemu

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"grillo.local/grillo/internal/sandbox"
)

const stateFileName = "sandbox.json"

// persistedProcess is the on-disk form of platform/linux.ProcessID.
type persistedProcess struct {
	PID        int    `json:"pid"`
	BootID     string `json:"bootId"`
	StartTime  uint64 `json:"startTime"`
	Executable string `json:"executable,omitempty"`
}

// persistedSandbox is the durable state of one sandbox. It stores the resolved
// spec (including the per-boot guest key) in a 0700 directory with a 0600 file.
type persistedSandbox struct {
	ID        string             `json:"id"`
	Operation string             `json:"operation,omitempty"`
	Spec      sandbox.Spec       `json:"spec"`
	CID       uint32             `json:"cid"`
	QEMU      *persistedProcess  `json:"qemu,omitempty"`
	VirtioFSD []persistedProcess `json:"virtiofsd,omitempty"`
	State     string             `json:"state"`
	UpdatedAt time.Time          `json:"updatedAt"`
}

func loadSandbox(dir string) (persistedSandbox, error) {
	data, err := os.ReadFile(filepath.Join(dir, stateFileName))
	if errors.Is(err, os.ErrNotExist) {
		return persistedSandbox{}, sandbox.ErrNotFound
	}
	if err != nil {
		return persistedSandbox{}, err
	}
	var ps persistedSandbox
	if err := json.Unmarshal(data, &ps); err != nil {
		return persistedSandbox{}, fmt.Errorf("qemu: decode %s: %w", stateFileName, err)
	}
	return ps, nil
}

func saveSandbox(dir string, ps persistedSandbox) error {
	ps.UpdatedAt = time.Now().UTC()
	data, err := json.MarshalIndent(ps, "", "  ")
	if err != nil {
		return err
	}
	tmp := filepath.Join(dir, "."+stateFileName+".tmp")
	if err := os.WriteFile(tmp, append(data, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(dir, stateFileName))
}
