#!/usr/bin/env python3
# SPDX-License-Identifier: Apache-2.0
"""Read-only development inventory; not a license/compliance certification."""
import argparse
import hashlib
import json
import pathlib
import subprocess


def run(*args):
    result = subprocess.run(args, cwd=pathlib.Path(__file__).resolve().parent.parent,
                            capture_output=True, timeout=30, check=True)
    if len(result.stdout) > 4 * 1024 * 1024:
        raise ValueError("inventory command output limit")
    return result.stdout.decode()


def notices(root):
    files = []
    if not root.is_dir():
        return files
    for item in sorted(root.iterdir()):
        if item.name.upper().startswith(("LICENSE", "COPYING", "NOTICE", "COPYRIGHT")):
            if item.is_symlink() or not item.is_file():
                continue
            if item.stat().st_size > 4 * 1024 * 1024:
                raise ValueError("notice size limit")
            files.append({"name": item.name, "sha256": hashlib.sha256(item.read_bytes()).hexdigest()})
    return files


def inventory(root):
    data = run("go", "list", "-m", "-json", "all")
    decoder = json.JSONDecoder()
    module_cache = pathlib.Path(run("go", "env", "GOMODCACHE").strip())
    modules = []
    while data.strip():
        obj, end = decoder.raw_decode(data.lstrip())
        data = data.lstrip()[end:]
        source = pathlib.Path(obj.get("Dir", "/nonexistent"))
        if not source.is_dir() and obj.get("Version"):
            escaped = "".join("!" + ch.lower() if ch.isupper() else ch for ch in obj["Path"])
            source = module_cache / (escaped + "@" + obj["Version"])
        modules.append({"module": obj["Path"], "version": obj.get("Version", "working-tree"),
                        "sum": obj.get("Sum"), "notices": notices(source),
                        "source_materialized": source.is_dir()})
    lockpath = root / "web/package-lock.json"
    if lockpath.stat().st_size > 4 * 1024 * 1024:
        raise ValueError("package lock size limit")
    lock = json.loads(lockpath.read_text())
    npm = []
    for name, obj in sorted(lock["packages"].items()):
        if not name:
            continue
        relative = pathlib.PurePosixPath(name)
        if relative.is_absolute() or ".." in relative.parts:
            raise ValueError("unsafe package path")
        npm.append({"package": name.removeprefix("node_modules/"), "version": obj.get("version"),
                    "declared_license": obj.get("license", "unspecified"), "resolved": obj.get("resolved"),
                    "integrity": obj.get("integrity"), "notices": notices(root / "web" / name),
                    "installed": (root / "web" / name).is_dir()})
    return {"schema_version": 1, "redistribution_review": "incomplete; not a release attestation",
            "go": modules, "npm": npm,
            "go_toolchain": {"version": run("go", "env", "GOVERSION").strip(),
                             "notices": notices(pathlib.Path(run("go", "env", "GOROOT").strip()))},
            "limitations": ["Uninstalled optional packages require separate source/notice review.",
                            "Metadata labels do not determine obligations of embedded fonts or bundled code.",
                            "Guest kernel/runc/BusyBox/glibc and helper provenance are reviewed separately.",
                            "Checksums identify bytes; no publisher signatures are verified here."]}


if __name__ == "__main__":
    parser = argparse.ArgumentParser()
    parser.add_argument("--output", type=pathlib.Path, required=True)
    args = parser.parse_args()
    root = pathlib.Path(__file__).resolve().parent.parent
    result = inventory(root)
    # Explicit caller-selected output, never automatic source/tool download.
    with args.output.open("x") as stream:
        json.dump(result, stream, indent=2, sort_keys=True)
        stream.write("\n")
