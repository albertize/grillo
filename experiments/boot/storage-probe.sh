#!/bin/sh
# SPDX-License-Identifier: Apache-2.0
# T02 storage/live-bind feasibility probe.
#
# The plan requires "live read/write/read-only binds" without privileged host
# mounts or copy-based substitutes (scenario G). Live host-directory sharing needs
# a shared-filesystem device (virtio-fs or 9p). This probe records, reproducibly,
# whether the candidate VMM and guest kernel provide one. It changes nothing on
# the host and starts no VM.
#
# Exit codes: 0 a shared-filesystem device exists; 3 BLOCKED (none exists).
set -eu

repo=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd -P)
artifacts=${ARTIFACTS_DIR:-"$repo/experiments/artifacts/dependencies-v1"}
spec=$artifacts/firecracker/release-v1.17.0-x86_64/firecracker_spec-v1.17.0.yaml
config=$artifacts/downloads/guest-kernel.config

if [ ! -f "$spec" ]; then
    echo "BLOCKED: Firecracker API spec not found; run 'bash scripts/bootstrap.sh --download'" >&2
    exit 3
fi
if [ ! -f "$config" ]; then
    echo "BLOCKED: guest kernel config not found; run 'bash scripts/bootstrap.sh --download'" >&2
    exit 3
fi

echo '== Firecracker API device endpoints =='
grep -oE '^  /[a-z0-9/_{}-]+:' "$spec" | sort -u
echo
echo '== shared-filesystem device search (virtio-fs / 9p / filesystem) =='
if grep -qiE 'virtio[-_]?fs|virtiofs|virtio[-_]?9p|[^a-z0-9]9p[^a-z0-9]|filesystem' "$spec"; then
    echo 'FOUND: shared-filesystem device mentioned in the API'
    found=1
else
    echo 'none: Firecracker exposes no virtio-fs or 9p device'
    found=0
fi
echo
echo '== guest kernel shared-filesystem options =='
grep -E 'CONFIG_(NET_9P|9P_FS|VIRTIO_FS|FUSE_FS|NFS_FS|VIRTIO_PMEM|VIRTIO_BLK)=' "$config" | sort || true
echo

if [ "$found" -eq 1 ]; then
    echo 'RESULT: a shared-filesystem device may exist; verify live bind semantics manually'
    exit 0
fi
cat <<'EOF'
RESULT: BLOCKED

Firecracker attaches storage only as virtio-block (a whole block device) and
virtio-pmem (a memory-mapped file region). Neither provides live, bidirectional
host-directory sharing with rename/watch semantics, and no virtio-fs/9p device
exists. Under the T02 gate this fails scenario G without a custom FUSE/9p bridge
or a different backend (for example QEMU microvm or Cloud Hypervisor with
virtiofsd). The guest kernel also lacks CONFIG_9P_FS.
EOF
exit 3
