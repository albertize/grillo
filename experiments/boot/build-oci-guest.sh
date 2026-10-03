#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Build the T02 OCI guest initramfs: the T01 PID 1 plus static runc and the OCI
# bundles from fetch-oci.sh. Explicit opt-in; never uses sudo.
set -euo pipefail

repo=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd -P)
src=${ARTIFACTS_DIR:-"$repo/experiments/artifacts/dependencies-v1"}
out=${OUT_DIR:-"$repo/experiments/artifacts/t02"}
kernel=$out/vmlinux
initramfs=$out/initramfs-oci.cpio.gz

for tool in go cpio gzip sha256sum; do
    command -v "$tool" >/dev/null || { echo "build-oci-guest: missing tool: $tool" >&2; exit 1; }
done
[[ -x $out/runc ]] || { echo "build-oci-guest: run 'bash experiments/boot/fetch-oci.sh' first" >&2; exit 1; }
[[ -d $out/bundles ]] || { echo 'build-oci-guest: missing OCI bundles; run fetch-oci.sh' >&2; exit 1; }

# Reuse the T01 kernel (same guest kernel is fine for OCI).
if [[ ! -f $kernel ]]; then
    t01=$repo/experiments/artifacts/t01/vmlinux
    if [[ -f $t01 ]]; then
        cp -- "$t01" "$kernel"
    else
        echo "build-oci-guest: no guest kernel; run 'make guest' first" >&2
        exit 1
    fi
fi

mkdir -p -- "$out"
umask 022
stage=$(mktemp -d "$out/.oci-initramfs.XXXXXXXX")
trap 'rm -rf -- "$stage"' EXIT
root=$stage/root
mkdir -p -- "$root/bin" "$root/proc" "$root/sys" "$root/dev" "$root/tmp" "$root/run" "$root/sys/fs/cgroup" "$root/oci"

(
    cd -- "$repo"
    CGO_ENABLED=0 go build -trimpath -ldflags=-s -o "$root/init" ./experiments/boot/guestinit
)
install -m 0755 -- "$out/runc" "$root/runc"
# Copy the bundle tree; hardlinks in the source avoid duplicating rootfs content.
cp -al -- "$out/bundles" "$root/oci/bundles"

echo "build-oci-guest: packing $initramfs" >&2
( cd -- "$root" && find . -print0 | cpio --null -o --format=newc --quiet ) | gzip -9 > "$initramfs"

printf 'build-oci-guest: %s (%s bytes)\n' "$initramfs" "$(stat -c %s "$initramfs")"
( cd -- "$out" && sha256sum initramfs-oci.cpio.gz )
