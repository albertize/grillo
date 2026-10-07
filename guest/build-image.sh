#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Build the T07 guest initramfs: the real grillo-agent PID 1, a pinned static
# runc, busybox container root filesystems, and a per-boot handshake key.
#
# Explicit opt-in; never uses sudo. Requires the T02 OCI fixture (runc + busybox
# rootfs) from `make oci-guest`.
set -euo pipefail

repo=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd -P)
t02=${T02_DIR:-"$repo/experiments/artifacts/t02"}
out=${OUT_DIR:-"$repo/experiments/artifacts/t07"}
kernel=${KERNEL:-"$repo/experiments/artifacts/qemu/bzImage"}
agent_version=${AGENT_VERSION:-0.1.0-t07}

for tool in go cpio gzip sha256sum base64 head install; do
    command -v "$tool" >/dev/null || { echo "build-image: missing tool: $tool" >&2; exit 1; }
done
[[ -x $t02/runc ]] || { echo "build-image: run 'make oci-guest' first (missing runc)" >&2; exit 1; }
[[ -d $t02/rootfs ]] || { echo "build-image: run 'make oci-guest' first (missing rootfs)" >&2; exit 1; }
[[ -f $kernel ]] || { echo "build-image: missing guest kernel $kernel" >&2; exit 1; }

umask 077
mkdir -p -- "$out/bin"

# A legacy fixture key for direct component gates. Production runtime boots
# override it with a fresh key in an operation-private verified initramfs overlay;
# neither key is placed on the command line or in ordinary diagnostics.
if [[ ! -s $out/key ]]; then
    head -c 32 /dev/urandom | base64 > "$out/key"
fi
chmod 0600 -- "$out/key"

# Build the agent once and reuse it in the image and the manifest.
(
    cd -- "$repo"
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags=-s -o "$out/bin/grillo-agent" ./cmd/grillo-agent
)

stage=$(mktemp -d "$out/.image.XXXXXXXX")
trap 'rm -rf -- "$stage"' EXIT
root=$stage/root
mkdir -p -- "$root"/{proc,sys,dev,tmp,run/grillo,sys/fs/cgroup,shared,etc/grillo}

mkdir -p "$root/bin"
install -m 0755 -- "$out/bin/grillo-agent" "$root/init"
install -m 0755 -- "$t02/rootfs/bin/busybox" "$root/bin/busybox"
# busybox is dynamically linked; the agent uses it to configure the interface.
for libdir in lib lib64 usr/lib usr/lib64; do
    if [[ -d $t02/rootfs/$libdir ]]; then
        cp -a -- "$t02/rootfs/$libdir" "$root/"
    fi
done
install -m 0755 -- "$t02/runc" "$root/runc"
cp -- "$out/key" "$root/etc/grillo/key"

# One busybox rootfs per container so roots stay separate; hardlinks keep the
# initramfs small. New files created in one root are not visible in another.
for name in setup app sidecar; do
    cp -al -- "$t02/rootfs" "$root/rootfs-$name"
done
mkdir -p -- "$root/rootfs-app/www"
rm -f -- "$root/rootfs-app/www/index.html"
printf 'grillo-agent-localhost-ok\n' > "$root/rootfs-app/www/index.html"

echo "build-image: packing $out/initramfs-agent.cpio.gz" >&2
( cd -- "$root" && find . -print0 | cpio --null -o --format=newc --quiet ) | gzip -9 > "$stage/initramfs-agent.cpio.gz"
chmod 0600 -- "$stage/initramfs-agent.cpio.gz"
mv -- "$stage/initramfs-agent.cpio.gz" "$out/initramfs-agent.cpio.gz"

(
    cd -- "$repo"
    go run ./guest/manifest -out "$out/manifest.json" \
        "grillo-agent=$agent_version=$out/bin/grillo-agent" \
        "runc=1.5.2=$t02/runc" \
        "kernel=$(basename "$kernel")=$kernel" \
        "initramfs=$agent_version=$out/initramfs-agent.cpio.gz"
)

printf 'build-image: done\n'
( cd -- "$out" && sha256sum initramfs-agent.cpio.gz )
