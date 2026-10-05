//go:build !linux

// SPDX-License-Identifier: Apache-2.0

package helm

import (
	"context"
	"errors"
	"os"
)

func openInput(root *os.Root, path string) (*os.File, error) { return root.Open(path) }

func ownedCacheEntry(os.FileInfo) bool { return true }

func withCacheLock(context.Context, string, func() error) error {
	return errors.New("chart cache publication requires Linux")
}
