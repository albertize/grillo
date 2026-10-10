#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
set -euo pipefail
work=$(mktemp -d)
trap 'rm -rf -- "$work"' EXIT
mkdir -p "$work/tools" "$work/output with spaces"
pin=$(printf '1%.0s' {1..64})
printf '%s\n' '#!/usr/bin/env bash' 'set -euo pipefail' \
    'out=""; while [[ $# -gt 0 ]]; do if [[ $1 == -out ]]; then out=$2; shift; fi; shift; done' \
    '[[ -n $out && ! -e $out ]]' \
    'mkdir -p "$out/bin"' \
    'printf "synthetic-prefix\\n" > "$out/bin/grillo"' \
    'if [[ ${PAYLOAD_TEST_FAIL:-} == stage ]]; then exit 19; fi' \
    > "$work/tools/go"
chmod +x "$work/tools/go"
export GO="$work/tools/go"
root="$work/output with spaces"
printf 'preserve\n' > "$root/unrelated"
run() { bash scripts/payload.sh "$root" 'selection with spaces' 'helm with spaces' "$pin"; }
run > "$work/first.log"
run > "$work/second.log"
mapfile -t outputs < <(find "$root" -mindepth 1 -maxdepth 1 -type d -name 'grillo-test.*')
[[ ${#outputs[@]} == 2 ]]
for output in "${outputs[@]}"; do
    [[ ! -e "$output/grillo" ]]
    (cd "$output" && sha256sum --check grillo-test.tar.gz.sha256)
    mkdir "$output/extracted"
    tar --no-same-owner -xzf "$output/grillo-test.tar.gz" -C "$output/extracted"
    [[ $(<"$output/extracted/grillo/bin/grillo") == synthetic-prefix ]]
done
if PAYLOAD_TEST_FAIL=stage run > "$work/failure.log" 2>&1; then
    echo 'payload test: staging failure ignored' >&2; exit 1
fi
# Failure removes only its unique stage; previous pairs and sentinel survive.
[[ $(find "$root" -mindepth 1 -maxdepth 1 -type d -name 'grillo-test.*' | wc -l) == 2 ]]
[[ $(<"$root/unrelated") == preserve ]]
# Archive failure after successful staging also refuses publication/cleans stage.
printf '%s\n' '#!/usr/bin/env bash' 'exit 23' > "$work/tools/tar"
chmod +x "$work/tools/tar"
if PATH="$work/tools:$PATH" run > "$work/tar-failure.log" 2>&1; then
    echo 'payload test: archive failure ignored' >&2; exit 1
fi
[[ $(find "$root" -mindepth 1 -maxdepth 1 -type d -name 'grillo-test.*' | wc -l) == 2 ]]
ln -s "$root" "$work/output-link"
if bash scripts/payload.sh "$work/output-link" selection helm "$pin" > /dev/null 2>&1; then
    echo 'payload test: output symlink accepted' >&2; exit 1
fi
if bash scripts/payload.sh "$work/invalid" selection helm bad-pin > /dev/null 2>&1; then
    echo 'payload test: invalid pin accepted' >&2; exit 1
fi
[[ ! -e "$work/invalid" ]]
echo 'payload tests PASS (offline fixture contract; not real staging or KVM)'
