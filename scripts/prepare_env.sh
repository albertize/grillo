#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Source this file in Bash; a child process cannot configure its parent shell.
if [[ ${BASH_SOURCE[0]} == "$0" ]]; then
    printf '%s\n' 'Use: source scripts/prepare_env.sh (in Bash)' >&2
    exit 1
fi

_grillo_prepare_env() {
    local root helm source_dir
    source_dir=${BASH_SOURCE[0]%/*}
    if [[ $source_dir == "${BASH_SOURCE[0]}" ]]; then source_dir=.; fi
    root=$(cd -- "$source_dir/.." && pwd -P) || return 1

    export GRILLO_DAEMON_BINARY="${GRILLO_DAEMON_BINARY:-$root/bin/grillod}"
    export GRILLO_NETNS_BINARY="${GRILLO_NETNS_BINARY:-$root/bin/grillo-netns}"
    export GRILLO_GUEST_KERNEL="${GRILLO_GUEST_KERNEL:-$root/experiments/artifacts/qemu/bzImage}"
    export GRILLO_GUEST_INITRAMFS="${GRILLO_GUEST_INITRAMFS:-$root/experiments/artifacts/t07/initramfs-agent.cpio.gz}"
    export GRILLO_GUEST_MANIFEST="${GRILLO_GUEST_MANIFEST:-$root/experiments/artifacts/t07/manifest.json}"
    if [[ -n ${GRILLO_HELM_BINARY:-} ]]; then
        export GRILLO_HELM_BINARY
    elif helm=$(type -P helm); then
        # Resolve relative PATH entries before the caller changes directory.
        if [[ $helm != /* ]]; then helm="$PWD/$helm"; fi
        export GRILLO_HELM_BINARY="$helm"
    else
        printf '%s\n' 'Helm not found; provision v4.2.2 and set GRILLO_HELM_BINARY for charts.' >&2
    fi

    case ":${PATH:-}:" in
        *":$root/bin:"*) ;;
        *) export PATH="$root/bin${PATH:+:$PATH}" ;;
    esac
    export GOTOOLCHAIN=local
}

if _grillo_prepare_env; then
    unset -f _grillo_prepare_env
else
    unset -f _grillo_prepare_env
    return 1
fi
