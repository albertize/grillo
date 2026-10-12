# SPDX-License-Identifier: Apache-2.0
import hashlib
import importlib.util
import os
from pathlib import Path
import tempfile
import unittest

SPEC = importlib.util.spec_from_file_location("source_inventory", Path(__file__).with_name("source-inventory.py"))
MODULE = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(MODULE)


class SourceInventoryTests(unittest.TestCase):
    def prepared(self, root):
        for name in MODULE.INPUTS:
            path = root / name
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_bytes(b"fixture source bytes")
        (root / "libpathrs-Cargo.lock").write_text("version = 3\npackage = []\n")

    def test_inventory_does_not_fetch_or_clear_distribution(self):
        with tempfile.TemporaryDirectory() as work:
            root = Path(work)
            self.prepared(root)
            result = MODULE.inventory(root)
            self.assertEqual(len(result["inputs"]), len(MODULE.INPUTS))
            self.assertEqual(result["archive_execution"], "none")
            self.assertIn("pending", result["status"])
            self.assertNotIn(work, str(result))

    def test_no_follow_special_and_byte_bounds(self):
        with tempfile.TemporaryDirectory() as work:
            root = Path(work)
            path = root / "unsafe"
            path.symlink_to("/dev/null")
            with self.assertRaises(OSError):
                MODULE.bounded_bytes(path)
            path.unlink()
            os.mkfifo(path)
            with self.assertRaises(ValueError):
                MODULE.bounded_bytes(path)
            path.unlink()
            path.write_bytes(b"1234")
            with self.assertRaises(ValueError):
                MODULE.bounded_bytes(path, 3)
            self.assertEqual(MODULE.bounded_bytes(path, 4), b"1234")

    def test_crate_checksum_and_parent_symlink(self):
        with tempfile.TemporaryDirectory() as work:
            root = Path(work)
            self.prepared(root)
            directory = root / "libpathrs-crates"
            directory.mkdir()
            blob = directory / "test-1.0.0.crate"
            blob.write_bytes(b"crate source")
            checksum = hashlib.sha256(blob.read_bytes()).hexdigest()
            (root / "libpathrs-Cargo.lock").write_text(
                'version = 3\n[[package]]\nname = "test"\nversion = "1.0.0"\n'
                'source = "registry+https://github.com/rust-lang/crates.io-index"\n'
                f'checksum = "{checksum}"\n')
            self.assertEqual(len(MODULE.inventory(root)["rust_crates"]), 1)
            blob.write_bytes(b"changed source")
            with self.assertRaises(ValueError):
                MODULE.inventory(root)
            blob.unlink()
            directory.rmdir()
            directory.symlink_to(root)
            with self.assertRaises(ValueError):
                MODULE.inventory(root)


if __name__ == "__main__":
    unittest.main()
