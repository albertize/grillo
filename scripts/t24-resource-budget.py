#!/usr/bin/env python3
# SPDX-License-Identifier: Apache-2.0
"""Check a successful 30-repeat seeded-CAS local KVM resource receipt.

Candidate local regression budgets, not a portable product SLO or guest-network
benchmark. Does not excuse a failed process, skip, incomplete batch or cleanup.
"""
import argparse
import json
import math
import os
import re
import stat
import statistics

PHASES = ("seeded-cas-fresh-rootfs-compose", "seeded-cas-warm-rootfs-compose")
LIMITS = {
    PHASES[0]: {"ready_us": 2_000_000, "sampled_peak_rss_bytes": 256 << 20,
                "logical_bytes": 700 << 20, "allocated_bytes": 700 << 20,
                "idle_cpu_user_hz_ticks": 8},
    PHASES[1]: {"ready_us": 1_800_000, "sampled_peak_rss_bytes": 256 << 20,
                "logical_bytes": 700 << 20, "allocated_bytes": 700 << 20,
                "idle_cpu_user_hz_ticks": 8},
}


def check(text, count=30):
    lines = text.splitlines()
    if any("--- FAIL:" in x or "--- SKIP:" in x or x.startswith(("FAIL", "panic:", "fatal error:")) for x in lines):
        raise ValueError("failed/skipped receipt cannot satisfy a resource budget")
    if not any(x == "PASS" for x in lines) or not any(re.match(r"^ok\s+github\.com/albertize/grillo/internal/distribution(?:\s|$)", x) for x in lines):
        raise ValueError("successful Go test completion missing")
    if sum(x.startswith("--- PASS: TestKVMInstalledRuntime ") for x in lines) != count:
        raise ValueError("successful installed-runtime case count mismatch")
    records = []
    for line in lines:
        if len(line) > 65536:
            raise ValueError("receipt line limit exceeded")
        if "RESOURCE_SAMPLE " in line:
            record = json.loads(line.split("RESOURCE_SAMPLE ", 1)[1])
            if record.get("phase") not in PHASES:
                raise ValueError("not a seeded-CAS local profile")
            for key in (*LIMITS[PHASES[0]], "idle_interval_us", "rss_samples", "idle_rss_bytes"):
                if type(record.get(key)) is not int or record[key] < 0:
                    raise ValueError("invalid resource sample")
            if record["rss_samples"] == 0 or not 900_000 <= record["idle_interval_us"] <= 2_000_000:
                raise ValueError("incomplete RSS/idle sample window")
            records.append(record)
    if count < 1 or count > 1000 or len(records) != count * 2:
        raise ValueError("incomplete or duplicated resource batch")
    result = {"profile": "candidate local seeded-CAS regression budget", "status": "PASS", "phases": {}}
    for phase in PHASES:
        group = [x for x in records if x["phase"] == phase]
        if len(group) != count:
            raise ValueError("phase count mismatch")
        metrics = {}
        for key, limit in LIMITS[phase].items():
            values = sorted(x[key] for x in group)
            p95 = values[math.ceil(0.95 * count) - 1]
            # Readiness uses nearest-rank p95. Memory/disk/idle ticks are
            # finite caps on every observed sample, not just on the percentile.
            actual = p95 if key == "ready_us" else max(values)
            if actual > limit:
                raise ValueError("resource budget exceeded: " + phase + "/" + key)
            metrics[key] = dict(median=statistics.median(values), p95=p95,
                                maximum=max(values), budget=limit)
        result["phases"][phase] = dict(samples=count, metrics=metrics)
    return result


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--log", required=True)
    args = parser.parse_args()
    fd = os.open(args.log, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
    with os.fdopen(fd, "rb") as source:
        info = os.fstat(source.fileno())
        if not stat.S_ISREG(info.st_mode) or info.st_size > 16 << 20:
            raise ValueError("bounded regular receipt required")
        data = source.read((16 << 20) + 1)
        if len(data) > 16 << 20:
            raise ValueError("receipt grew beyond limit")
    print(json.dumps(check(data.decode()), indent=2, sort_keys=True))


if __name__ == "__main__":
    main()
