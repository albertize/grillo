#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Build the QEMU guest kernel with CONFIG_VIRTIO_FS enabled (the Firecracker CI
# config omits it). Out-of-tree so the Firecracker kernel layout is untouched.
set -euo pipefail

repo=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../../.." && pwd -P)
artifacts=${ARTIFACTS_DIR:-"$repo/experiments/artifacts/dependencies-v1"}
src=$artifacts/linux-6.1.188
cfg=$artifacts/downloads/guest-kernel.config
out=${OUT_DIR:-"$repo/experiments/artifacts/qemu"}
build=$out/kernel-build
kernel=$out/bzImage

for tool in make cp sha256sum nproc; do
    command -v "$tool" >/dev/null || { echo "build-kernel: missing tool: $tool" >&2; exit 1; }
done
[[ -d $src ]] || { echo "build-kernel: kernel source missing; run scripts/bootstrap.sh --download" >&2; exit 1; }
[[ -f $cfg ]] || { echo "build-kernel: kernel config missing: $cfg" >&2; exit 1; }

if [[ -f $kernel ]]; then
    echo "build-kernel: already built $kernel" >&2
    exit 0
fi

mkdir -p -- "$out" "$build"
echo "build-kernel: cleaning in-tree kernel build (artifact snapshots are unaffected)" >&2
make -C "$src" mrproper >/dev/null
cp -- "$cfg" "$build/.config"
"$src/scripts/config" --file "$build/.config" --enable VIRTIO_FS
echo "build-kernel: building bzImage with CONFIG_VIRTIO_FS" >&2
make -C "$src" O="$build" olddefconfig
make -C "$src" O="$build" -j"$(nproc)" bzImage
cp -- "$build/arch/x86/boot/bzImage" "$kernel"
sha256sum "$kernel"
