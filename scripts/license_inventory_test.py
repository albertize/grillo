# SPDX-License-Identifier: Apache-2.0
import importlib.util
import pathlib
import tempfile
import unittest

spec = importlib.util.spec_from_file_location("license_inventory", pathlib.Path(__file__).with_name("license-inventory.py"))
audit = importlib.util.module_from_spec(spec)
spec.loader.exec_module(audit)


class NoticeTests(unittest.TestCase):
    def test_hashes_notices_without_following_links(self):
        with tempfile.TemporaryDirectory() as directory:
            root = pathlib.Path(directory)
            (root / "LICENSE").write_text("synthetic notice")
            (root / "NOTICE").symlink_to(root / "LICENSE")
            records = audit.notices(root)
            self.assertEqual([record["name"] for record in records], ["LICENSE"])
            self.assertEqual(len(records[0]["sha256"]), 64)

    def test_rejects_large_notice(self):
        with tempfile.TemporaryDirectory() as directory:
            root = pathlib.Path(directory)
            with (root / "COPYING").open("wb") as stream:
                stream.truncate(4 * 1024 * 1024 + 1)
            with self.assertRaises(ValueError):
                audit.notices(root)


if __name__ == "__main__":
    unittest.main()
