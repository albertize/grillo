#!/usr/bin/env python3
# SPDX-License-Identifier: Apache-2.0
import importlib.util
import os
import pathlib
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch

spec = importlib.util.spec_from_file_location('policy', pathlib.Path(__file__).with_name('go-toolchain.py'))
policy = importlib.util.module_from_spec(spec)
spec.loader.exec_module(policy)


class PolicyTest(unittest.TestCase):
    def test_exact_family_and_minimum(self):
        policy.validate('go1.27.2', '1.27.2')
        for actual in ['go1.26.9', 'go1.27.2-X:nodwarf5', 'go1.27.1', 'go1.27.3', 'go1.28.0', 'devel go1.27', '', 'go1.27.2\nextra']:
            with self.subTest(actual=actual), self.assertRaises(ValueError):
                policy.validate(actual, '1.27.2')
        for expected in ['1.27', '1.26.9', '1.27.0', '1.27.1', '1.28.0', '1.27.2rc1']:
            with self.subTest(expected=expected), self.assertRaises(ValueError):
                policy.validate('go'+expected, expected)
        with self.assertRaises(ValueError):
            policy.validate('go1.27.2', '1.27.2', 'go1.27.2')

    def test_selected_go_path_and_root_identity(self):
        with tempfile.TemporaryDirectory(prefix='go policy spaces ') as tmp:
            root = pathlib.Path(tmp); goroot = root/'goroot'; goroot.mkdir()
            (goroot/'VERSION').write_text('go1.27.2\ntime fixture\n')
            (root/'.go-version').write_text('1.27.2\n')
            go = root/'go'
            go.write_text('#!/usr/bin/env python3\nimport os\nassert os.environ["GOTOOLCHAIN"] == "local"\nassert "GOVERSION" not in os.environ\nprint("go1.27.2")\nprint(os.environ["TEST_GOROOT"])\n')
            go.chmod(0o700)
            env = {**os.environ, 'TEST_GOROOT':str(goroot)};env.pop('GOVERSION', None)
            with patch.object(policy, 'ROOT', root):
                selected, version, child = policy.prepare(str(go), env)
                self.assertEqual(selected, str(go));self.assertEqual(version, 'go1.27.2')
                self.assertEqual(child['PATH'].split(os.pathsep)[0], str(root))
                (goroot/'VERSION').write_text('go1.26.9\n')
                with self.assertRaisesRegex(ValueError, 'GOROOT'):
                    policy.prepare(str(go), env)
                with self.assertRaises(ValueError):
                    policy.prepare(str(go), {**env, 'GOVERSION':'go1.27.2'})
            with self.assertRaises(ValueError):policy.prepare(str(root/'missing'), env)

    def test_scanner_failure_and_timeout(self):
        self.assertEqual(policy.run_scanner([sys.executable, '-c', 'raise SystemExit(7)'], os.environ), 7)
        with self.assertRaises(subprocess.TimeoutExpired):
            policy.run_scanner([sys.executable, '-c', 'import time; time.sleep(10)'], os.environ, timeout=0.03)

    def test_repo_pin_consistency(self):
        root = pathlib.Path(__file__).resolve().parent.parent
        version = (root/'.go-version').read_text().strip()
        self.assertIn('\ngo '+version+'\n', (root/'go.mod').read_text())
        self.assertIn('GO_VERSION='+version+'\n', (root/'scripts/bootstrap.sh').read_text())
        self.assertIn("go-version-file: '.go-version'", (root/'.github/workflows/ci.yml').read_text())


if __name__ == '__main__':
    unittest.main()
