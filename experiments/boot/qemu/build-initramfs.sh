#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Build the QEMU storage-probe initramfs (PID 1 that mounts virtiofs). Explicit
# opt-in; never uses sudo.
set -euo pipefail

repo=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../../.." && pwd -P)
out=${OUT_DIR:-"$repo/experiments/artifacts/qemu"}
probe_pkg=${PROBE_PKG:-./experiments/boot/qemu/storageprobe}
probe_name=${PROBE_NAME:-initramfs-probe.cpio.gz}
initramfs=$out/$probe_name

for tool in go cpio gzip sha256sum; do
    command -v "$tool" >/dev/null || { echo "build-initramfs: missing tool: $tool" >&2; exit 1; }
done

mkdir -p -- "$out"
umask 022
stage=$(mktemp -d "$out/.probe.XXXXXXXX")
trap 'rm -rf -- "$stage"' EXIT
root=$stage/root
mkdir -p -- "$root/proc" "$root/sys" "$root/dev" "$root/mnt/host" "$root/mnt/ro"

(
    cd -- "$repo"
    CGO_ENABLED=0 go build -trimpath -ldflags=-s -o "$root/init" "$probe_pkg"
)

echo "build-initramfs: packing $initramfs" >&2
( cd -- "$root" && find . -print0 | cpio --null -o --format=newc --quiet ) | gzip -9 > "$initramfs"
printf 'build-initramfs: %s (%s bytes)\n' "$initramfs" "$(stat -c %s "$initramfs")"
( cd -- "$out" && sha256sum "$probe_name" )
