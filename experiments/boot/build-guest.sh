#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Build the T01 feasibility guest artifacts: an uncompressed guest kernel and a
# gzip-compressed cpio initramfs containing the experiment PID 1 and test command.
# Requires the pinned downloads from scripts/bootstrap.sh; never uses sudo.
set -euo pipefail

repo=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd -P)
artifacts=${ARTIFACTS_DIR:-"$repo/experiments/artifacts/dependencies-v1"}
out=${OUT_DIR:-"$repo/experiments/artifacts/t01"}
kernel_src=$artifacts/linux-6.1.188
kernel_config=$artifacts/downloads/guest-kernel.config
kernel=$out/vmlinux
initramfs=$out/initramfs.cpio.gz

for tool in go cpio gzip sha256sum; do
    command -v "$tool" >/dev/null || { echo "build-guest: missing tool: $tool" >&2; exit 1; }
done

if [[ ! -d $kernel_src ]]; then
    echo "build-guest: kernel source not found: $kernel_src" >&2
    echo "run 'bash scripts/bootstrap.sh --download' first" >&2
    exit 1
fi
[[ -f $kernel_config ]] || { echo "build-guest: missing kernel config: $kernel_config" >&2; exit 1; }

mkdir -p -- "$out"
umask 022

if [[ ! -f $kernel ]]; then
    if [[ ! -f $kernel_src/vmlinux ]]; then
        echo "build-guest: building guest kernel (this can take several minutes)" >&2
        cp -- "$kernel_config" "$kernel_src/.config"
        make -C "$kernel_src" olddefconfig
        make -C "$kernel_src" -j"$(nproc)" vmlinux
    fi
    cp -- "$kernel_src/vmlinux" "$kernel"
fi

echo "build-guest: building guest binaries" >&2
stage=$(mktemp -d "$out/.initramfs.XXXXXXXX")
trap 'rm -rf -- "$stage"' EXIT
root=$stage/root
mkdir -p -- "$root/bin" "$root/proc" "$root/sys" "$root/dev" "$root/tmp"

(
    cd -- "$repo"
    CGO_ENABLED=0 go build -trimpath -ldflags=-s -o "$root/init" ./experiments/boot/guestinit
    CGO_ENABLED=0 go build -trimpath -ldflags=-s -o "$root/bin/grillo-testcmd" ./experiments/boot/guestcmd
)

echo "build-guest: packing initramfs" >&2
# Preserve ownership/symlinks deterministically; the initramfs is trusted input
# built locally from this repository, not something extracted from a download.
( cd -- "$root" && find . -print0 | cpio --null -o --format=newc --quiet ) | gzip -9 > "$initramfs"

echo "build-guest: artifacts"
printf '  kernel    %s (%s bytes)\n' "$kernel" "$(stat -c %s "$kernel")"
printf '  initramfs %s (%s bytes)\n' "$initramfs" "$(stat -c %s "$initramfs")"
( cd -- "$out" && sha256sum vmlinux initramfs.cpio.gz )
