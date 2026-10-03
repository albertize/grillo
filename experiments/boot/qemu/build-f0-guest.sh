#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Trusted T02/T03 probe payloads only; no downloads or host mounts.
set -euo pipefail
repo=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../../.." && pwd -P)
out="$repo/experiments/artifacts/qemu"
mkdir -p -- "$out"
stage=$(mktemp -d "$out/.f0-guest.XXXXXXXX")
trap 'rm -rf -- "$stage"' EXIT
mkdir -p "$stage/root/proc" "$stage/root/sys" "$stage/root/dev" "$stage/root/run" "$stage/root/tmp"
cd -- "$repo"
CGO_ENABLED=0 go build -trimpath -ldflags=-s -o "$stage/root/init" ./experiments/boot/guestinit
CGO_ENABLED=0 go build -trimpath -ldflags=-s -o "$stage/root/probe" ./experiments/boot/qemu/f0guest
(cd "$stage/root" && find . -print0 | cpio --null -o --format=newc --quiet) | gzip -9 > "$stage/initramfs-f0.cpio.gz"
mv -- "$stage/initramfs-f0.cpio.gz" "$out/initramfs-f0.cpio.gz"
sha256sum "$out/initramfs-f0.cpio.gz"
