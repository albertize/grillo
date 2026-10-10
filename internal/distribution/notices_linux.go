//go:build linux

// SPDX-License-Identifier: Apache-2.0

package distribution

import (
	"context"
	"fmt"
	"path/filepath"
)

// Copy an explicit allowlist, never arbitrary checkout/host files. Text is copied
// verbatim and its identity recorded in the same payload inventory as binaries.
func copyRuntimeNotices(ctx context.Context, documents, stage string, inventory *Inventory) error {
	files := []struct{ source, target string }{
		{"internal/ui/assets/generated/licenses.txt", "frontend-licenses.txt"},
		{"web/licenses/font-awesome-LICENSE.txt", "font-awesome-LICENSE.txt"},
		{"web/licenses/redhatdisplay-OFL.txt", "redhatdisplay-OFL.txt"},
		{"web/licenses/redhatmono-OFL.txt", "redhatmono-OFL.txt"},
		{"web/licenses/redhattext-OFL.txt", "redhattext-OFL.txt"},
	}
	var total int64
	for _, entry := range files {
		if err := ctx.Err(); err != nil {
			return err
		}
		source, err := openSource(filepath.Join(documents, entry.source))
		if err != nil {
			return fmt.Errorf("stage: required third-party notice unavailable: %s", entry.target)
		}
		info, err := source.Stat()
		if err != nil || info.Size() <= 0 || info.Size() > 1<<20 {
			source.Close()
			return fmt.Errorf("stage: invalid notice size")
		}
		total += info.Size()
		if total > 4<<20 {
			source.Close()
			return fmt.Errorf("stage: aggregate notice size limit")
		}
		relative := "share/doc/grillo/third-party/" + entry.target
		if err := copyFile(ctx, source, filepath.Join(stage, relative), 0444, "", relative, inventory); err != nil {
			return err
		}
	}
	inventory.NoticeCoverage = "partial: frontend/font text included; Go/Helm/guest complete notices and corresponding-source review pending"
	return nil
}
