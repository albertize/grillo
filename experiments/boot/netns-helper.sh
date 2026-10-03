#!/bin/sh
# SPDX-License-Identifier: Apache-2.0
# T01 rootless networking-helper probe.
#
# Proves that a rootless networking helper can give a new user+network namespace
# an address, a default route, DNS, and external connectivity, without sudo and
# without touching the host network configuration. This is a prerequisite for
# T02/T11; it is not the Grillo application network.
#
# Environment:
#   GRILLO_NET_HELPER      force a helper (pasta); default: auto-detect pasta
#   GRILLO_EGRESS_URL      HTTP URL used to test egress; default http://example.com/
#   GRILLO_EGRESS_TIMEOUT  per-request timeout in seconds; default 10
#
# Exit codes: 0 PASS, 1 FAIL/BLOCKED, 2 INCONCLUSIVE (configured but no egress).
set -eu

if [ "$(id -u)" = 0 ]; then
    echo 'BLOCKED: run as an unprivileged host user, not root' >&2
    exit 1
fi
for tool in timeout id cat mktemp; do
    command -v "$tool" >/dev/null || { echo "BLOCKED: missing tool: $tool" >&2; exit 1; }
done

helper=${GRILLO_NET_HELPER:-}
if [ -z "$helper" ]; then
    if command -v pasta >/dev/null; then
        helper=pasta
    elif command -v slirp4netns >/dev/null; then
        helper=slirp4netns
    else
        echo 'BLOCKED: no rootless networking helper (pasta or slirp4netns) in PATH' >&2
        echo 'Install one (for example: sudo dnf install passt) before running this probe.' >&2
        exit 1
    fi
fi
if [ "$helper" != pasta ]; then
    echo "BLOCKED: helper '$helper' is not yet exercised by this probe; use pasta" >&2
    exit 1
fi

egress_url=${GRILLO_EGRESS_URL:-http://example.com/}
egress_timeout=${GRILLO_EGRESS_TIMEOUT:-10}
export EGRESS_URL=$egress_url EGRESS_TIMEOUT=$egress_timeout

echo "helper: $(pasta --version 2>&1 | head -1)"
echo "egress: $egress_url"
echo '--- distribution restrictions ---'
printf 'host_uid=%s\n' "$(id -u)"
printf 'max_user_namespaces=%s\n' "$(cat /proc/sys/user/max_user_namespaces 2>/dev/null || echo n/a)"
if [ -e /proc/sys/kernel/unprivileged_userns_clone ]; then
    printf 'unprivileged_userns_clone=%s\n' "$(cat /proc/sys/kernel/unprivileged_userns_clone)"
else
    echo 'unprivileged_userns_clone=absent'
fi
if command -v getenforce >/dev/null; then
    printf 'selinux=%s\n' "$(getenforce 2>/dev/null || echo unknown)"
fi
if [ -e /dev/net/tun ]; then
    ls -l /dev/net/tun
else
    echo '/dev/net/tun=absent'
fi

work=$(mktemp -d)
trap 'rm -rf -- "$work"' EXIT

cat > "$work/inner.sh" <<'INNER'
#!/bin/sh
set -eu
echo 'INNER interfaces:'
ip -4 -brief addr show
if ! ip -4 -o addr show | awk '$4 ~ /\./ && $4 !~ /^127\./ {found=1} END {exit !found}'; then
    echo 'INNER: no non-loopback IPv4 address' >&2
    exit 10
fi
if ! ip -4 route show default | grep -q default; then
    echo 'INNER: no default route' >&2
    exit 11
fi
if ! ip link show lo | grep -q 'lo.*UP\|LOOPBACK.*UP'; then
    ip link show lo
fi
echo 'INNER: namespace configured'
command -v curl >/dev/null || { echo 'INNER: curl unavailable for egress test' >&2; exit 21; }
code=$(curl -sS -m "$EGRESS_TIMEOUT" -o /dev/null -w '%{http_code}' "$EGRESS_URL" 2>/dev/null) || {
    echo 'INNER: egress unreachable' >&2
    exit 20
}
echo "INNER: egress http=$code"
INNER
chmod 0700 "$work/inner.sh"

set +e
timeout --kill-after=2s "${GRILLO_HELPER_TIMEOUT:-60}" pasta -f --config-net -- sh "$work/inner.sh"
rc=$?
set -e

echo '--- cleanup ---'
if pgrep -x pasta >/dev/null 2>&1; then
    echo 'FAIL: a pasta process remains after the probe' >&2
    pgrep -a pasta >&2 || true
    exit 1
fi
echo 'no pasta process remains'

case $rc in
0)
    echo 'PASS: rootless helper provided address, default route, and egress'
    echo 'NOT CHECKED: port forwarding, DNS interception specifics, multi-VM routing'
    ;;
10 | 11)
    echo 'FAIL: helper started but did not configure the namespace' >&2
    exit 1
    ;;
20)
    echo 'INCONCLUSIVE: namespace configured but no external egress (offline host?)' >&2
    exit 2
    ;;
21)
    echo 'INCONCLUSIVE: curl not available inside the namespace for the egress test' >&2
    exit 2
    ;;
*)
    echo "FAIL: helper exited with status $rc" >&2
    exit 1
    ;;
esac
