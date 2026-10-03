#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# T02 OCI fixture: fetch a pinned static runc and build a busybox OCI rootfs plus
# three bundles (basic exec test, localhost server, localhost client). Explicit
# opt-in; never uses sudo. Separate from T01 artifacts so T01 evidence stays fixed.
set -euo pipefail

# Publish only complete fixtures. The lock serializes builders sharing OUT_DIR;
# the unnamed container is owned by this invocation via its unique CID file.
fetch_rootfs() (
    set -euo pipefail
    local out=$1 image=$2 stage=''
    mkdir -p -- "$out"
    exec 9>"$out/.rootfs.lock"
    flock -x 9
    if [[ -e $out/rootfs || -L $out/rootfs ]]; then
        if [[ ! -L $out/rootfs && -f $out/rootfs/.grillo-fixture-complete &&
              $(<"$out/rootfs/.grillo-fixture-complete") = "$image" &&
              -x $out/rootfs/bin/busybox ]]; then
            return 0
        fi
        echo 'fetch-oci: unverified/incomplete rootfs; review and move it aside before retrying' >&2
        return 1
    fi
    stage=$(mktemp -d "$out/.rootfs-stage.XXXXXXXX")
    cleanup() {
        local status=$?
        trap - EXIT
        if [[ -s $stage/container.cid ]]; then
            if ! podman rm -f "$(<"$stage/container.cid")" >/dev/null; then
                echo "fetch-oci: container cleanup failed; CID retained in $stage/container.cid" >&2
                exit 1
            fi
        fi
        rm -rf -- "$stage"
        exit "$status"
    }
    trap cleanup EXIT
    trap 'exit 130' INT
    trap 'exit 143' TERM
    podman create --cidfile "$stage/container.cid" "$image" >/dev/null
    mkdir -- "$stage/rootfs"
    podman export "$(<"$stage/container.cid")" |
        tar -x -C "$stage/rootfs" --no-same-owner --no-same-permissions
    [[ -x $stage/rootfs/bin/busybox ]] || {
        echo 'fetch-oci: exported fixture has no executable busybox' >&2
        exit 1
    }
    printf '%s\n' "$image" > "$stage/rootfs/.grillo-fixture-complete"
    # Cleanup must succeed before the cache becomes reusable.
    podman rm "$(<"$stage/container.cid")" >/dev/null
    rm -- "$stage/container.cid"
    mv -T -- "$stage/rootfs" "$out/rootfs"
)

main() {
repo=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd -P)
out=${OUT_DIR:-"$repo/experiments/artifacts/t02"}

RUNC_VERSION=1.5.2
RUNC_SHA=599f6f94ff8c5057241eff0d54c3c74f95c34935b6457b33fe545defc61e9488
# Content-addressed OCI image (busybox 1.37, linux/amd64 manifest).
BUSYBOX_IMAGE='docker.io/library/busybox:1.37@sha256:bdf57e528e45e4433820e045b29b4597825a1c9e38353532d90a01445013f82e'

for tool in curl sha256sum tar flock; do
    command -v "$tool" >/dev/null || { echo "fetch-oci: missing tool: $tool" >&2; exit 1; }
done
[[ $(uname -m) = x86_64 ]] || { echo 'fetch-oci: Linux/x86_64 only' >&2; exit 1; }

mkdir -p -- "$out"
# Serialize the entire fixture build, including runc and bundle replacement.
exec 8>"$out/.fixture.lock"
flock -x 8
umask 022

# Static runc, checksum-verified before use.
if [[ ! -x $out/runc ]]; then
    echo "fetch-oci: downloading runc v$RUNC_VERSION" >&2
    curl --fail --location --show-error --silent \
        --proto '=https' --proto-redir '=https' \
        --connect-timeout 15 --max-time 600 --retry 3 --max-filesize 67108864 \
        --output "$out/runc.part" \
        "https://github.com/opencontainers/runc/releases/download/v${RUNC_VERSION}/runc.amd64"
    if ! printf '%s  %s\n' "$RUNC_SHA" "$out/runc.part" | sha256sum --check --status; then
        rm -f -- "$out/runc.part"
        echo 'fetch-oci: runc checksum mismatch; refusing artifact' >&2
        exit 1
    fi
    chmod 0755 "$out/runc.part"
    mv -- "$out/runc.part" "$out/runc"
fi
printf '%s  %s\n' "$RUNC_SHA" "$out/runc" | sha256sum --check --status || {
    echo 'fetch-oci: cached runc checksum mismatch; refusing execution' >&2
    exit 1
}
"$out/runc" --version >&2

# Busybox rootfs from a content-addressed image (no Grillo registry client).
fetch_rootfs "$out" "$BUSYBOX_IMAGE"
mkdir -p -- "$out/rootfs/www"
printf 'grillo-oci-localhost-ok\n' > "$out/rootfs/www/index.html"

echo 'fetch-oci: building bundles' >&2
rm -rf -- "$out/bundles"
for bundle in basic serve client; do
    mkdir -p -- "$out/bundles/$bundle"
    # Hardlink the shared rootfs so the initramfs stores its content once.
    cp -al -- "$out/rootfs" "$out/bundles/$bundle/rootfs"
done

cat > "$out/bundles/basic/config.json" <<'JSON'
{
  "ociVersion": "1.0.2",
  "process": {
    "terminal": false,
    "args": ["/bin/sleep", "300"],
    "env": ["PATH=/bin:/usr/bin:/sbin:/usr/sbin"],
    "cwd": "/"
  },
  "root": {"path": "rootfs", "readonly": false},
  "hostname": "grillo-basic",
  "mounts": [
    {"destination": "/proc", "type": "proc", "source": "proc"},
    {"destination": "/dev", "type": "tmpfs", "source": "tmpfs", "options": ["nosuid", "strictatime", "mode=755", "size=65536k"]},
    {"destination": "/dev/pts", "type": "devpts", "source": "devpts", "options": ["nosuid", "noexec", "newinstance", "ptmxmode=0666", "mode=0620"]},
    {"destination": "/dev/shm", "type": "tmpfs", "source": "shm", "options": ["nosuid", "noexec", "nodev", "mode=1777", "size=65536k"]},
    {"destination": "/sys", "type": "sysfs", "source": "sysfs", "options": ["nosuid", "noexec", "nodev", "ro"]}
  ],
  "linux": {
    "resources": {"devices": [{"allow": false, "access": "rwm"}]},
    "namespaces": [
      {"type": "pid"},
      {"type": "ipc"},
      {"type": "uts"},
      {"type": "mount"}
    ]
  }
}
JSON

cat > "$out/bundles/serve/config.json" <<'JSON'
{
  "ociVersion": "1.0.2",
  "process": {
    "terminal": false,
    "args": ["/bin/httpd", "-f", "-p", "8080", "-h", "/www"],
    "env": ["PATH=/bin:/usr/bin:/sbin:/usr/sbin"],
    "cwd": "/"
  },
  "root": {"path": "rootfs", "readonly": false},
  "hostname": "grillo-serve",
  "mounts": [
    {"destination": "/proc", "type": "proc", "source": "proc"},
    {"destination": "/dev", "type": "tmpfs", "source": "tmpfs", "options": ["nosuid", "strictatime", "mode=755", "size=65536k"]},
    {"destination": "/dev/pts", "type": "devpts", "source": "devpts", "options": ["nosuid", "noexec", "newinstance", "ptmxmode=0666", "mode=0620"]},
    {"destination": "/dev/shm", "type": "tmpfs", "source": "shm", "options": ["nosuid", "noexec", "nodev", "mode=1777", "size=65536k"]},
    {"destination": "/sys", "type": "sysfs", "source": "sysfs", "options": ["nosuid", "noexec", "nodev", "ro"]}
  ],
  "linux": {
    "resources": {"devices": [{"allow": false, "access": "rwm"}]},
    "namespaces": [
      {"type": "pid"},
      {"type": "ipc"},
      {"type": "uts"},
      {"type": "mount"}
    ]
  }
}
JSON

cat > "$out/bundles/client/config.json" <<'JSON'
{
  "ociVersion": "1.0.2",
  "process": {
    "terminal": false,
    "args": ["/bin/wget", "-qO-", "http://127.0.0.1:8080/"],
    "env": ["PATH=/bin:/usr/bin:/sbin:/usr/sbin"],
    "cwd": "/"
  },
  "root": {"path": "rootfs", "readonly": false},
  "hostname": "grillo-client",
  "mounts": [
    {"destination": "/proc", "type": "proc", "source": "proc"},
    {"destination": "/dev", "type": "tmpfs", "source": "tmpfs", "options": ["nosuid", "strictatime", "mode=755", "size=65536k"]},
    {"destination": "/dev/pts", "type": "devpts", "source": "devpts", "options": ["nosuid", "noexec", "newinstance", "ptmxmode=0666", "mode=0620"]},
    {"destination": "/dev/shm", "type": "tmpfs", "source": "shm", "options": ["nosuid", "noexec", "nodev", "mode=1777", "size=65536k"]},
    {"destination": "/sys", "type": "sysfs", "source": "sysfs", "options": ["nosuid", "noexec", "nodev", "ro"]}
  ],
  "linux": {
    "resources": {"devices": [{"allow": false, "access": "rwm"}]},
    "namespaces": [
      {"type": "pid"},
      {"type": "ipc"},
      {"type": "uts"},
      {"type": "mount"}
    ]
  }
}
JSON

echo "fetch-oci: done"
printf '  runc    %s\n' "$(sha256sum "$out/runc" | cut -d' ' -f1)"
printf '  rootfs  %s\n' "$(du -sh "$out/rootfs" | cut -f1)"
printf '  image   %s\n' "$BUSYBOX_IMAGE"
}

if [[ ${BASH_SOURCE[0]} = "$0" ]]; then
    main "$@"
fi
