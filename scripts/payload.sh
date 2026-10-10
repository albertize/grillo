#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Local experimental transfer payload, not signed/compliance-cleared release.
set -euo pipefail
umask 077

if [[ $# != 4 || -z $1 || -z $2 || -z $3 || ! $4 =~ ^[[:xdigit:]]{64}$ ]]; then
    echo 'Usage: payload.sh OUTPUT_ROOT GUEST_SELECTION HELM_BINARY HELM_SHA256' >&2
    exit 2
fi
root=$1
selection=$2
helm=$3
pin=$4
for tool in "${GO:-go}" tar sha256sum mktemp; do
    command -v "$tool" >/dev/null || { echo "payload: missing tool: $tool" >&2; exit 1; }
done
if [[ -L $root ]]; then
    echo 'payload: symlink output root refused' >&2
    exit 1
fi
mkdir -p -- "$root"
root=$(cd -- "$root" && pwd -P)
work=$(mktemp -d "$root/grillo-test.XXXXXXXX")
success=false
cleanup() {
    if [[ $success != true ]]; then
        # This exact private directory was allocated by this invocation only.
        rm -rf -- "$work"
    fi
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

"${GO:-go}" run ./scripts/stage \
    -out "$work/grillo" -guest-selection "$selection" \
    -helm "$helm" -helm-sha256 "$pin"
tar -C "$work" -czf "$work/grillo-test.tar.gz" grillo
(
    cd -- "$work"
    sha256sum grillo-test.tar.gz > grillo-test.tar.gz.sha256
    sha256sum --check grillo-test.tar.gz.sha256
)
# Remove only our temporary stage; retain the archive/checksum pair on success.
rm -rf -- "$work/grillo"
success=true
printf 'Experimental payload (redistribution review pending):\n%s\n%s\n' \
    "$work/grillo-test.tar.gz" "$work/grillo-test.tar.gz.sha256"
