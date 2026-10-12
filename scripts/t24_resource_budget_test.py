# SPDX-License-Identifier: Apache-2.0
import importlib.util
import json
from pathlib import Path
import unittest

SPEC = importlib.util.spec_from_file_location("resource_budget", Path(__file__).with_name("t24-resource-budget.py"))
MODULE = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(MODULE)


def receipt(ready=1000000):
    lines = []
    for phase in MODULE.PHASES:
        row = dict(phase=phase, ready_us=ready, sampled_peak_rss_bytes=128 << 20,
                   logical_bytes=500 << 20, allocated_bytes=500 << 20,
                   idle_cpu_user_hz_ticks=3, idle_interval_us=1000000,
                   rss_samples=10, idle_rss_bytes=128 << 20)
        lines.append("    fixture.go:1: RESOURCE_SAMPLE " + json.dumps(row))
    lines.extend(("--- PASS: TestKVMInstalledRuntime (1.00s)", "PASS",
                  "ok\tgithub.com/albertize/grillo/internal/distribution\t1.00s"))
    return "\n".join(lines)


class ResourceBudgetTests(unittest.TestCase):
    def test_successful_fixture_contract(self):
        self.assertEqual(MODULE.check(receipt(), 1)["status"], "PASS")
        self.assertEqual(MODULE.check(receipt().replace("ok\t", "ok  \t"), 1)["status"], "PASS")

    def test_failure_skip_and_missing_samples(self):
        for tail in ("\nFAIL", "\n--- FAIL: cleanup", "\n--- SKIP: prerequisite"):
            with self.assertRaises(ValueError):
                MODULE.check(receipt() + tail, 1)
        with self.assertRaises(ValueError):
            MODULE.check(receipt())
        with self.assertRaises(ValueError):
            MODULE.check(receipt().replace("--- PASS:", "--- INCOMPLETE:"), 1)

    def test_budget_and_profile_failure(self):
        with self.assertRaises(ValueError):
            MODULE.check(receipt(ready=2100000), 1)
        with self.assertRaises(ValueError):
            MODULE.check(receipt().replace("seeded-cas-", "fresh-registry-"), 1)


if __name__ == "__main__":
    unittest.main()
