#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Offline fixture tests; never contact Podman or a registry.
set -euo pipefail
repo=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd -P)
work=$(mktemp -d)
trap 'rm -rf -- "$work"' EXIT
mkdir -p "$work/bin" "$work/fixture/bin"
printf '#!/bin/sh\nexit 0\n' > "$work/fixture/bin/busybox"
chmod +x "$work/fixture/bin/busybox"
tar -cf "$work/fixture.tar" -C "$work/fixture" .
export FIXTURE_TAR="$work/fixture.tar" PODMAN_LOG="$work/podman.log"
cat > "$work/bin/podman" <<'SH'
#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >> "$PODMAN_LOG"
case $1 in
 create)
  [[ $2 = --cidfile ]]
  printf 'owned-%s\n' "${3%/container.cid}" > "$3"
  ;;
 export)
  [[ $2 = owned-* ]]
  cat "$FIXTURE_TAR"
  [[ ${FAIL_EXPORT:-0} = 0 ]]
  ;;
 rm)
  shift
  [[ ${1:-} != -f ]] || shift
  [[ $# = 1 && $1 = owned-* ]]
  [[ ${FAIL_REMOVE:-0} = 0 ]]
  ;;
 *) exit 99 ;;
esac
SH
chmod +x "$work/bin/podman"
export PATH="$work/bin:$PATH"
source "$repo/experiments/boot/fetch-oci.sh"

# Invoke in a fresh shell: testing a function directly in 'if' disables Bash's
# errexit inside that function and would mask real failure behavior.
run_fetch() {
 bash -c 'source "$1"; fetch_rootfs "$2" pinned-image' _ "$repo/experiments/boot/fetch-oci.sh" "$1"
}

FAIL_EXPORT=1 run_fetch "$work/failure" && { echo 'partial export accepted' >&2; exit 1; }
[[ ! -e $work/failure/rootfs ]]
[[ -z $(find "$work/failure" -maxdepth 1 -name '.rootfs-stage.*' -print) ]]
grep -q '^rm -f owned-' "$PODMAN_LOG"
run_fetch "$work/failure"
[[ -x $work/failure/rootfs/bin/busybox ]]
[[ $(<"$work/failure/rootfs/.grillo-fixture-complete") = pinned-image ]]
cp "$PODMAN_LOG" "$work/before.log"
run_fetch "$work/failure"
cmp "$PODMAN_LOG" "$work/before.log"

mkdir -p "$work/incomplete/rootfs"
if run_fetch "$work/incomplete"; then echo 'unmarked cache accepted' >&2; exit 1; fi
[[ -d $work/incomplete/rootfs ]]

# Two builders must reuse one completed cache, without deleting each other's
# containers. Names from the caller's Podman namespace are never removed.
run_fetch "$work/concurrent" & p1=$!
run_fetch "$work/concurrent" & p2=$!
wait "$p1"; wait "$p2"
[[ $(grep -c '^create ' "$PODMAN_LOG") = 3 ]]
if grep -q 'grillo-t02-fixture\|--name' "$PODMAN_LOG"; then exit 1; fi
# Failed cleanup cannot publish a successful cache; retain its precise CID for
# manual recovery rather than retrying a name-based destructive sweep.
if FAIL_REMOVE=1 run_fetch "$work/cleanup-failure"; then
    echo 'cleanup failure ignored' >&2; exit 1
fi
[[ ! -e $work/cleanup-failure/rootfs ]]
[[ $(find "$work/cleanup-failure" -name container.cid | wc -l) = 1 ]]

mkdir -p "$work/symlink"
ln -s "$work/fixture" "$work/symlink/rootfs"
if run_fetch "$work/symlink"; then echo 'symlink cache accepted' >&2; exit 1; fi
[[ -L $work/symlink/rootfs ]]
printf 'PASS: OCI fixture ownership, partial export, retry, cache, concurrency, cleanup failure, symlink refusal\n'
