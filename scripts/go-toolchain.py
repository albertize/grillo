#!/usr/bin/env python3
# SPDX-License-Identifier: Apache-2.0
"""Fail-closed build/advisory policy; never downloads or substitutes a version."""
import argparse
import os
import pathlib
import re
import shutil
import signal
import subprocess
import sys

ROOT = pathlib.Path(__file__).resolve().parent.parent


def validate(actual, expected, override=None):
    if not re.fullmatch(r"1\.27\.[0-9]+", expected):
        raise ValueError("invalid pin: Grillo is pinned to the Go 1.27 release family")
    if tuple(map(int, expected.split('.'))) < (1, 27, 2):
        raise ValueError("Go pin is below the selected patch minimum 1.27.2")
    if override is not None:
        raise ValueError("unset GOVERSION: overriding advisory identity is not allowed")
    if actual != "go" + expected:
        raise ValueError(f"need exact upstream go{expected}; got {actual!r}. Vendor/development strings cannot establish stdlib scan coverage")


def prepare(go, env):
    path = shutil.which(go)
    if not path:
        raise ValueError("selected Go executable unavailable")
    path = str(pathlib.Path(path).resolve())
    if pathlib.Path(path).name != 'go':
        raise ValueError("select the actual toolchain bin/go, not a renamed shim")
    child = dict(env)
    child['GOTOOLCHAIN'] = 'local'
    child['PATH'] = str(pathlib.Path(path).parent) + os.pathsep + child.get('PATH', '')
    # Never let a scanner override pretend the compiler/stdlib were upgraded.
    child.pop('GOVERSION', None)
    result = subprocess.run([path, 'env', 'GOVERSION', 'GOROOT'], env=child,
                            capture_output=True, timeout=15, check=True, cwd=ROOT)
    if len(result.stdout) > 4096:
        raise ValueError("oversized toolchain identity")
    lines = result.stdout.decode().splitlines()
    if len(lines) != 2:
        raise ValueError("unrecognized Go identity response")
    actual, goroot = lines
    expected = (ROOT / '.go-version').read_text().strip()
    validate(actual, expected, env.get('GOVERSION'))
    # A conflicting GOROOT must not pair a patched compiler identity with an old
    # stdlib tree. This is a consistency check, not publisher authentication.
    with (pathlib.Path(goroot) / 'VERSION').open() as stream:
        version = stream.readline(4097).strip()
    if version != actual:
        raise ValueError("GOROOT VERSION differs from actual compiler identity")
    return path, actual, child


def run_scanner(command, env, timeout=240):
    # Only the new operation's process group is killed on timeout/cancellation;
    # terminating go run alone could leave its scanner/compiler child running.
    with subprocess.Popen(command, cwd=ROOT, env=env, start_new_session=True) as process:
        try:
            return process.wait(timeout=timeout)
        except (subprocess.TimeoutExpired, KeyboardInterrupt):
            try:
                os.killpg(process.pid, signal.SIGKILL)
            except ProcessLookupError:
                pass
            process.wait()
            raise


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--go', default='go')
    parser.add_argument('--scanner', choices=['v1.8.0'])
    args = parser.parse_args()
    try:
        go, version, env = prepare(args.go, os.environ)
        print(f"Verified Go policy: {version}; GOTOOLCHAIN=local; no advisory version override", flush=True)
        if args.scanner:
            return run_scanner([go, 'run', 'golang.org/x/vuln/cmd/govulncheck@' + args.scanner, './...'], env)
        return 0
    except (ValueError, OSError, UnicodeError, subprocess.SubprocessError) as error:
        print(f"Go toolchain policy refused: {error}", file=sys.stderr)
        return 2


if __name__ == '__main__':
    sys.exit(main())
