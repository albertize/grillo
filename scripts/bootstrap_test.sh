#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Offline tests: downloaded programs are never executed; fixtures are not KVM evidence.
set -euo pipefail
bootstrap=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)/bootstrap.sh
work=$(mktemp -d)
trap 'rm -rf -- "$work"' EXIT

bash -n "$bootstrap"
bash "$bootstrap" > "$work/plan"
grep -q 'Go 1.26.8' "$work/plan"
bash "$bootstrap" --help > /dev/null
if bash "$bootstrap" --invalid > /dev/null 2>&1; then
    echo 'FAIL: invalid flag accepted' >&2; exit 1
else
    test "$?" -eq 2
fi
if bash "$bootstrap" --download --system-packages > /dev/null 2>&1; then
    echo 'FAIL: combined modes accepted' >&2; exit 1
else
    test "$?" -eq 2
fi

# Test real checksum verification independently of network access.
bash -s -- "$bootstrap" "$work" <<'CASE'
set -euo pipefail
source "$1"
work=$2
curl() {
    while [[ "$1" != --output ]]; do shift; done
    printf 'verified fixture\n' > "$2"
}
digest=$(printf 'verified fixture\n' | sha256sum)
digest=${digest%% *}
fetch_verified https://invalid.example/fixture "$digest" "$work/verified"
test "$(< "$work/verified")" = 'verified fixture'
if fetch_verified https://invalid.example/fixture "${digest//?/0}" "$work/rejected"; then
    echo 'FAIL: checksum mismatch accepted' >&2; exit 1
fi
test ! -e "$work/rejected"
test ! -e "$work/rejected.part"
curl() { return 22; }
if fetch_verified https://invalid.example/fixture "$digest" "$work/missing"; then
    echo 'FAIL: download failure accepted' >&2; exit 1
fi
test ! -e "$work/missing"
CASE

if (( EUID == 0 )); then
    if bash "$bootstrap" --download > /dev/null 2>&1; then
        echo 'FAIL: root downloads allowed' >&2; exit 1
    fi
    echo 'SKIP: installation fixtures require an unprivileged user'
else
    if bash "$bootstrap" --system-packages > /dev/null 2>&1; then
        echo 'FAIL: unprivileged package installation accepted' >&2; exit 1
    fi
    # Exercise staging, extraction, paths with shell metacharacters, and activation.
    # These archives are deliberately tiny offline stand-ins, not upstream binaries.
    fixture="$work/fixtures"
    mkdir -p "$fixture/go/bin" "$fixture/release-v1.17.0-x86_64" "$fixture/linux-6.1.188"
    printf 'do not execute\n' > "$fixture/go/bin/go"
    printf 'do not execute\n' > "$fixture/release-v1.17.0-x86_64/firecracker-v1.17.0-x86_64"
    printf 'fixture\n' > "$fixture/linux-6.1.188/COPYING"
    tar -czf "$fixture/go.tgz" -C "$fixture" go
    tar -czf "$fixture/firecracker.tgz" -C "$fixture" release-v1.17.0-x86_64
    tar -cJf "$fixture/linux.tar.xz" -C "$fixture" linux-6.1.188
    target="$work/project with spaces ' \$literal"
    mkdir "$target"
    bash -s -- "$bootstrap" "$target" "$fixture" <<'CASE'
set -euo pipefail
source "$1"
root=$2
fixtures=$3
fetch_verified() {
    case "$1" in
        *go.dev*) cp "$fixtures/go.tgz" "$3" ;;
        *releases/download*) cp "$fixtures/firecracker.tgz" "$3" ;;
        *cdn.kernel.org*) cp "$fixtures/linux.tar.xz" "$3" ;;
        *raw.githubusercontent.com*) printf 'fixture config\n' > "$3" ;;
        *) return 1 ;;
    esac
}
install_downloads "$root"
test -x "$root/experiments/artifacts/dependencies-v1/bin/firecracker"
test "$(< "$root/experiments/artifacts/dependencies-v1/go.mod")" = 'module grillo.local/bootstrap-artifacts'
test -f "$root/experiments/artifacts/dependencies-v1/linux-6.1.188/COPYING"
source "$root/experiments/artifacts/dependencies-v1/env.sh"
test "$(command -v firecracker)" = "$root/experiments/artifacts/dependencies-v1/bin/firecracker"
test "$GOTOOLCHAIN" = local
CASE
    # Refuse existing installations, and never touch symlink destinations.
    for mode in existing symlink failure; do
        root="$work/$mode"
        mkdir -p "$root/experiments/artifacts"
        case "$mode" in
            existing) mkdir "$root/experiments/artifacts/dependencies-v1" ;;
            symlink) ln -s "$fixture" "$root/experiments/artifacts/dependencies-v1" ;;
        esac
        if bash -s -- "$bootstrap" "$root" <<'CASE'
set -euo pipefail
source "$1"
# Simulated network failure; must abort before extraction and clean staging.
fetch_verified() { return 42; }
install_downloads "$2"
CASE
        then
            echo "FAIL: $mode accepted" >&2; exit 1
        fi
        test -z "$(find "$root/experiments/artifacts" -maxdepth 1 -name '.dependencies.*' -print)"
    done
    test -f "$fixture/linux-6.1.188/COPYING"
fi

echo 'PASS: bootstrap offline tests'
