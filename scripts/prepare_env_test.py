#!/usr/bin/env python3
# SPDX-License-Identifier: Apache-2.0
"""Offline activation tests: no tools installed, assets built, or daemon started."""

import json
from pathlib import Path
import shlex
import shutil
import subprocess
import sys
import tempfile
import unittest

SCRIPT = Path(__file__).with_name("prepare_env.sh").resolve()
ROOT = SCRIPT.parent.parent
BASH = shutil.which("bash")


class PrepareEnvTest(unittest.TestCase):
    def activate(self, script=SCRIPT, repeat=False, **overrides):
        source = "source " + shlex.quote(str(script)) + " || exit 1; "
        command = "set -eu; " + source
        if repeat:
            command += source
        command += "exec " + shlex.quote(sys.executable)
        command += " -c 'import json, os; print(json.dumps(dict(os.environ)))'"
        result = subprocess.run(
            [BASH, "--noprofile", "--norc", "-c", command],
            env={"PATH": "/nonexistent", **overrides}, cwd="/",
            text=True, capture_output=True, check=True,
        )
        return json.loads(result.stdout), result.stderr

    def test_defaults_missing_helm_and_repeated_activation(self):
        env, stderr = self.activate(repeat=True)
        defaults = {
            "GRILLO_DAEMON_BINARY": "bin/grillod",
            "GRILLO_NETNS_BINARY": "bin/grillo-netns",
            "GRILLO_GUEST_KERNEL": "experiments/artifacts/qemu/bzImage",
            "GRILLO_GUEST_INITRAMFS": "experiments/artifacts/t07/initramfs-agent.cpio.gz",
            "GRILLO_GUEST_MANIFEST": "experiments/artifacts/t07/manifest.json",
        }
        for name, path in defaults.items():
            self.assertEqual(env[name], str(ROOT / path))
        self.assertEqual(env["PATH"], str(ROOT / "bin") + ":/nonexistent")
        self.assertEqual(env["GOTOOLCHAIN"], "local")
        self.assertNotIn("GRILLO_HELM_BINARY", env)
        self.assertIn("Helm not found", stderr)

    def test_preserves_overrides_without_executing_them(self):
        with tempfile.TemporaryDirectory() as temp:
            marker = Path(temp) / "must-not-exist"
            value = f"/a path/'quote'; $(touch {marker})\nnext line"
            overrides = {name: value for name in (
                "GRILLO_DAEMON_BINARY", "GRILLO_NETNS_BINARY", "GRILLO_GUEST_KERNEL",
                "GRILLO_GUEST_INITRAMFS", "GRILLO_GUEST_MANIFEST", "GRILLO_HELM_BINARY",
            )}
            env, stderr = self.activate(**overrides)
            for name in overrides:
                self.assertEqual(env[name], value)
            self.assertEqual(stderr, "")
            self.assertFalse(marker.exists())

    def test_discovers_helm_without_running_it(self):
        with tempfile.TemporaryDirectory() as temp:
            helm = Path(temp) / "helm"
            helm.write_text("#!/bin/sh\nexit 99\n")
            helm.chmod(0o755)
            env, stderr = self.activate(PATH=temp)
            self.assertEqual(env["GRILLO_HELM_BINARY"], str(helm))
            self.assertEqual(stderr, "")

    def test_checkout_path_with_shell_metacharacters(self):
        with tempfile.TemporaryDirectory(prefix="grillo ' $;") as temp:
            script = Path(temp) / "scripts/prepare_env.sh"
            script.parent.mkdir()
            shutil.copyfile(SCRIPT, script)
            env, _ = self.activate(script)
            self.assertEqual(env["GRILLO_DAEMON_BINARY"], str(Path(temp) / "bin/grillod"))

    def test_direct_execution_rejected(self):
        result = subprocess.run([BASH, str(SCRIPT)], text=True, capture_output=True)
        self.assertEqual(result.returncode, 1)
        self.assertIn("source scripts/prepare_env.sh", result.stderr)
        self.assertEqual(result.stdout, "")

    def test_missing_source_directory_fails(self):
        with tempfile.TemporaryDirectory() as temp:
            script = Path(temp) / "missing/prepare_env.sh"
            result = subprocess.run(
                [BASH, "-c", "source " + shlex.quote(str(script))],
                text=True, capture_output=True,
            )
            self.assertNotEqual(result.returncode, 0)


if __name__ == "__main__":
    unittest.main()
