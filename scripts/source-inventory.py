#!/usr/bin/env python3
# SPDX-License-Identifier: Apache-2.0
"""Read-only inventory of explicitly provisioned third-party source inputs.

No fetch, unpack, package execution, key import or redistribution approval.
"""
import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import stat
import tomllib

INPUTS = {
    "helm-4.2.2-1.fc44.src.rpm": ("Helm Fedora SRPM", "4.2.2-1.fc44"),
    "helm-srpm/helm-4.2.2.tar.gz": ("Helm upstream source", "4.2.2"),
    "helm-srpm/helm-4.2.2-vendor.tar.bz2": ("Helm vendored sources", "4.2.2-1.fc44"),
    "helm-srpm/helm.spec": ("Helm build recipe", "4.2.2-1.fc44"),
    "helm-srpm/go-vendor-tools.toml": ("Helm license recipe", "4.2.2-1.fc44"),
    "runc-29dd3dc2b13b4123162e5fe132504bb4b15569f1.tar.gz": ("runc source and Go vendor", "1.5.2 / 29dd3dc2b13b4123162e5fe132504bb4b15569f1"),
    "libseccomp-2.6.1.tar.gz": ("libseccomp source", "2.6.1"),
    "libpathrs-0.2.6.tar.gz": ("libpathrs source", "0.2.6"),
    "libpathrs-Cargo.lock": ("libpathrs crate locks", "0.2.6"),
    "busybox-recipe-066dbf90090ab2dbb905310fac2fa10ea39a7ba6.tar.gz": ("BusyBox image build recipes and patches", "066dbf90090ab2dbb905310fac2fa10ea39a7ba6"),
    "busybox-1.37.0.tar.bz2": ("BusyBox source", "1.37.0"),
    "glibc_2.41-12+deb13u4.dsc": ("glibc Debian source descriptor", "2.41-12+deb13u4"),
    "glibc_2.41.orig.tar.xz": ("glibc upstream source", "2.41"),
    "glibc_2.41-12+deb13u4.debian.tar.xz": ("glibc Debian patches/build recipes", "2.41-12+deb13u4"),
    "glibc-copyright.txt": ("glibc Debian copyright/terms", "2.41-12+deb13u4"),
    "libc6_2.41-12+deb13u4_amd64.deb": ("glibc binary comparison input (not source)", "2.41-12+deb13u4"),
    "debian-glibc-maintainer.asc": ("Debian glibc source signature public key", "primary 77462642A9EF94FD0F77196DBA9C78061DDD8C9B"),
    "busybox-maintainer.asc": ("BusyBox source signature public key", "C9E9416F76E610DBD09D040F47B70C55ACC9965B"),
    "busybox-1.37.0.tar.bz2.sig": ("BusyBox source signature", "1.37.0"),
}

SOURCES = {
    "helm-4.2.2-1.fc44.src.rpm": "Fedora fedora-source/updates-source repodata; resolved by explicit dnf5 source download",
    "runc-29dd3dc2b13b4123162e5fe132504bb4b15569f1.tar.gz": "https://codeload.github.com/opencontainers/runc/tar.gz/29dd3dc2b13b4123162e5fe132504bb4b15569f1",
    "libseccomp-2.6.1.tar.gz": "https://codeload.github.com/seccomp/libseccomp/tar.gz/refs/tags/v2.6.1",
    "libpathrs-0.2.6.tar.gz": "https://codeload.github.com/cyphar/libpathrs/tar.gz/refs/tags/v0.2.6",
    "busybox-recipe-066dbf90090ab2dbb905310fac2fa10ea39a7ba6.tar.gz": "https://codeload.github.com/docker-library/busybox/tar.gz/066dbf90090ab2dbb905310fac2fa10ea39a7ba6",
    "busybox-1.37.0.tar.bz2": "https://busybox.net/downloads/busybox-1.37.0.tar.bz2",
    "busybox-1.37.0.tar.bz2.sig": "https://busybox.net/downloads/busybox-1.37.0.tar.bz2.sig",
    "busybox-maintainer.asc": "https://keyserver.ubuntu.com/pks/lookup?op=get&search=0xC9E9416F76E610DBD09D040F47B70C55ACC9965B",
    "debian-glibc-maintainer.asc": "https://keyring.debian.org/pks/lookup?op=get&search=0x52BC8695BE34F90AD7D40CB81388C0F899E8336B",
}

for _name in ("glibc_2.41-12+deb13u4.dsc", "glibc_2.41.orig.tar.xz", "glibc_2.41-12+deb13u4.debian.tar.xz", "libc6_2.41-12+deb13u4_amd64.deb"):
    SOURCES[_name] = "https://deb.debian.org/debian/pool/main/g/glibc/" + _name


def bounded_bytes(path, limit=64 << 20):
    fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
    with os.fdopen(fd, "rb") as source:
        info = os.fstat(source.fileno())
        if not stat.S_ISREG(info.st_mode) or not 0 < info.st_size <= limit:
            raise ValueError("source input is not a bounded regular file")
        data = source.read(limit + 1)
        if len(data) > limit:
            raise ValueError("source input grew beyond limit")
        return data


def inventory(root):
    root = Path(root).absolute()
    if root.resolve() != root:
        raise ValueError("source root must not follow symlinks")
    records = []
    for name, (component, version) in INPUTS.items():
        path = root / name
        if path.parent.resolve() != path.parent:
            raise ValueError("source parent must not follow symlinks")
        data = bounded_bytes(path)
        records.append(dict(file=name, component=component, version=version,
                            bytes=len(data), sha256=hashlib.sha256(data).hexdigest(),
                            source=SOURCES.get(name, "contained in the inventoried source/package input; see review report")))
    lock = tomllib.loads(bounded_bytes(root / "libpathrs-Cargo.lock", 1 << 20).decode())
    crates = []
    for package in lock["package"]:
        if "source" not in package:
            continue
        if package["source"] != "registry+https://github.com/rust-lang/crates.io-index":
            raise ValueError("unsupported Rust source registry")
        name, version = package["name"], package["version"]
        if not re.fullmatch(r"[A-Za-z0-9_-]{1,128}", name) or not re.fullmatch(r"[0-9A-Za-z.+-]{1,128}", version):
            raise ValueError("unsafe crate identity")
        filename = name + "-" + version + ".crate"
        path = root / "libpathrs-crates" / filename
        if path.parent.resolve() != path.parent:
            raise ValueError("crate parent must not follow symlinks")
        data = bounded_bytes(path, 16 << 20)
        digest = hashlib.sha256(data).hexdigest()
        if digest != package["checksum"]:
            raise ValueError("crate checksum mismatch")
        crates.append(dict(file="libpathrs-crates/" + filename, version=version,
                           name=name, bytes=len(data), sha256=digest,
                           source="https://static.crates.io/crates/" + name + "/" + filename))
        if len(crates) > 512:
            raise ValueError("crate count bound exceeded")
    return dict(format_version=1, status="source inputs reviewed; redistribution clearance pending",
                verification="archive hashes observed; crate checksums verified against provided Cargo.lock; signatures are not verified by this script",
                archive_execution="none", inputs=records, rust_crates=crates)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--root", required=True)
    parser.add_argument("--output", required=True)
    args = parser.parse_args()
    data = (json.dumps(inventory(args.root), indent=2, sort_keys=True) + "\n").encode()
    fd = os.open(args.output, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
    with os.fdopen(fd, "wb") as output:
        output.write(data)
        output.flush()
        os.fsync(output.fileno())


if __name__ == "__main__":
    main()
