#!/bin/sh
# SPDX-License-Identifier: Apache-2.0
# All network changes are confined to a fresh, short-lived network namespace.
set -eu
if [ "$(id -u)" = 0 ]; then
    echo 'BLOCKED: run as an unprivileged host user, not root' >&2
    exit 1
fi
for tool in timeout unshare ip; do
    command -v "$tool" >/dev/null || {
        echo "BLOCKED: required tool missing: $tool" >&2
        exit 1
    }
done
printf 'Host UID: %s\n' "$(id -u)"
timeout --kill-after=2s 10s unshare --user --map-root-user --net sh -eu -c '
    echo "Namespace UID mapping:"
    read inside outside count < /proc/self/uid_map
    printf "%s %s %s\n" "$inside" "$outside" "$count"
    ip tuntap add dev grillo-tap0 mode tap
    ip link set grillo-tap0 up
    ip link show grillo-tap0
    ip link delete grillo-tap0
    echo "PASS: user/network namespace and TAP create/up/delete"
'
echo 'NOT CHECKED: helper forwarding, guest connectivity, or host publishing'
