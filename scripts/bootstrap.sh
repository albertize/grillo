#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Explicit setup only; never called by build, test, doctor, or runtime commands.
set -euo pipefail

GO_VERSION=1.26.8
FIRECRACKER_VERSION=1.17.0
KERNEL_VERSION=6.1.188
PACKAGES=(gcc make flex bison bc elfutils-libelf-devel openssl-devel perl
          cpio curl tar gzip xz diffutils findutils coreutils util-linux iproute)

usage() {
    cat <<'EOF'
Usage: bash scripts/bootstrap.sh [--dry-run|--download|--system-packages|--help]

Default/--dry-run: print the plan; do not download/install software or change files.
--download: download/check/extract pinned Linux/amd64 development and T01 tools
            into experiments/artifacts/dependencies-v1, as the normal user.
--system-packages: install Fedora build packages using dnf (requires an explicitly
                   root-invoked script). This script never invokes sudo.

These are the CURRENT dependencies, not a full runtime installation. No guest
build/boot, networking helper, Helm, OCI runtime, or host configuration is applied.
Downloads are verified against pinned SHA-256 values before extraction. They are
not executed. Package versions follow configured Fedora repositories, not pins.
EOF
}

plan() {
    usage
    printf '\nPinned downloads:\n  Go %s (BSD-3-Clause)\n  Firecracker %s (Apache-2.0; bundled notices apply)\n  Linux %s source (GPL-2.0 with per-file terms)\n  Firecracker guest kernel config %s\n' \
        "$GO_VERSION" "$FIRECRACKER_VERSION" "$KERNEL_VERSION" "$FIRECRACKER_VERSION"
    printf '\nFedora packages (explicit, separate operation):\n  dnf install'
    printf ' %s' "${PACKAGES[@]}"
    printf '\n\nDownload destination: experiments/artifacts/dependencies-v1\n'
}

fail() { printf 'ERROR: %s\n' "$*" >&2; return 1; }

# Destination must be inside the private staging directory owned by the caller.
# Failure never replaces a previously verified destination.
fetch_verified() {
    local url=$1 digest=$2 destination=$3
    curl --fail --location --show-error --silent \
        --proto '=https' --proto-redir '=https' \
        --connect-timeout 15 --max-time 1800 --retry 3 --max-filesize 268435456 \
        --output "$destination.part" "$url" || return 1
    if ! printf '%s  %s\n' "$digest" "$destination.part" | sha256sum --check --status; then
        rm -f -- "$destination.part"
        fail "checksum mismatch; refusing artifact: ${destination##*/}"
        return 1
    fi
    mv -- "$destination.part" "$destination"
}

system_packages() {
    # os-release is trusted, administrator-controlled host configuration.
    local ID=''
    if [[ ! -r /etc/os-release ]]; then
        fail 'cannot identify host distribution'; return 1
    fi
    . /etc/os-release
    if [[ "$ID" != fedora ]]; then
        fail '--system-packages currently supports Fedora only'; return 1
    fi
    if (( EUID != 0 )); then
        fail '--system-packages needs explicit administrator invocation; no automatic sudo'; return 1
    fi
    # Keep dnf confirmation enabled. Do not upgrade the host or install a daemon.
    dnf install "${PACKAGES[@]}"
}

# Subshell confines the cleanup trap and umask to this setup operation.
install_downloads() (
    local root=$1 stage='' tool path
    if [[ $(uname -s) != Linux || $(uname -m) != x86_64 ]]; then
        fail 'downloads currently support Linux/x86_64 only'; exit 1
    fi
    if (( EUID == 0 )); then
        fail 'run --download as your normal user, not root'; exit 1
    fi
    for tool in curl sha256sum tar gzip xz mktemp install mv rm; do
        command -v "$tool" >/dev/null || { fail "missing prerequisite: $tool"; exit 1; }
    done
    for path in "$root/experiments" "$root/experiments/artifacts"; do
        [[ ! -L "$path" ]] || { fail "refusing symlink directory: $path"; exit 1; }
    done
    local parent="$root/experiments/artifacts"
    local destination="$parent/dependencies-v1"
    if [[ -e "$destination" || -L "$destination" ]]; then
        fail "$destination already exists; no overwrite performed. Use its env.sh if trusted, or review and move it aside before retrying."
        exit 1
    fi
    umask 077
    mkdir -p -- "$parent"
    stage=$(mktemp -d "$parent/.dependencies.XXXXXXXX")
    # Only remove the exact private directory created above, never the destination.
    trap '[[ -z "$stage" ]] || rm -rf -- "$stage"' EXIT
    trap 'exit 130' INT
    trap 'exit 143' TERM
    mkdir -- "$stage/downloads" "$stage/bin" "$stage/firecracker"
    # Keep Go's ./... and mod tidy traversal out of third-party source fixtures.
    printf 'module grillo.local/bootstrap-artifacts\n' > "$stage/go.mod"

    fetch_verified "https://go.dev/dl/go${GO_VERSION}.linux-amd64.tar.gz" \
        d0f743b33e8d8945e6b1f432edd15785c70507121d6e2a723b21285eddf8b57b \
        "$stage/downloads/go${GO_VERSION}.linux-amd64.tar.gz"
    fetch_verified "https://github.com/firecracker-microvm/firecracker/releases/download/v${FIRECRACKER_VERSION}/firecracker-v${FIRECRACKER_VERSION}-x86_64.tgz" \
        06094a1108ae9e82aa4c23a775aa92758f53f1175d422270d9d6162cb9ade558 \
        "$stage/downloads/firecracker-v${FIRECRACKER_VERSION}-x86_64.tgz"
    fetch_verified "https://cdn.kernel.org/pub/linux/kernel/v6.x/linux-${KERNEL_VERSION}.tar.xz" \
        ed4d0acb1307c235230c89efc094e210e6290593f94a7e617f28b1001101a33a \
        "$stage/downloads/linux-${KERNEL_VERSION}.tar.xz"
    fetch_verified "https://raw.githubusercontent.com/firecracker-microvm/firecracker/v${FIRECRACKER_VERSION}/resources/guest_configs/microvm-kernel-ci-x86_64-6.1.config" \
        153ca1b40f3312bfb40b7587b471ea548a915f98f480f0d0892a1537957a2147 \
        "$stage/downloads/guest-kernel.config"

    # Extract only after every download verifies. Keep upstream licenses/notices.
    tar --extract --gzip --file "$stage/downloads/go${GO_VERSION}.linux-amd64.tar.gz" \
        --directory "$stage" --no-same-owner --no-same-permissions
    tar --extract --gzip --file "$stage/downloads/firecracker-v${FIRECRACKER_VERSION}-x86_64.tgz" \
        --directory "$stage/firecracker" --no-same-owner --no-same-permissions
    tar --extract --xz --file "$stage/downloads/linux-${KERNEL_VERSION}.tar.xz" \
        --directory "$stage" --no-same-owner --no-same-permissions
    install -m 0755 \
        "$stage/firecracker/release-v${FIRECRACKER_VERSION}-x86_64/firecracker-v${FIRECRACKER_VERSION}-x86_64" \
        "$stage/bin/firecracker"
    # Bash-escaped absolute path handles spaces and shell metacharacters safely.
    {
        printf '# Generated by Grillo bootstrap; source with Bash.\n'
        printf 'export PATH=%q:"$PATH"\n' "$destination/go/bin:$destination/bin"
        printf 'export GOTOOLCHAIN=local\n'
    } > "$stage/env.sh"
    # GNU mv -T prevents concurrent installs from nesting one tree in another.
    mv -T -- "$stage" "$destination"
    stage=''
    printf 'Installed verified artifacts; no downloaded code executed.\nActivate in Bash:\n  source %q\n' "$destination/env.sh"
)

main() {
    if (( $# > 1 )); then usage >&2; return 2; fi
    case "${1:---dry-run}" in
        --help|-h) usage ;;
        --dry-run) plan ;;
        --system-packages) system_packages ;;
        --download)
            local root
            root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd -P)
            install_downloads "$root"
            ;;
        *) usage >&2; return 2 ;;
    esac
}

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
    main "$@"
fi
