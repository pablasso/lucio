import os
import hashlib
import tarfile
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
import unittest


class PackageTests(unittest.TestCase):
    def test_only_tracked_allowlisted_regular_files(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp) / "source"
            dest = Path(tmp) / "package"
            root.mkdir()
            dest.mkdir()
            (root / "scripts").mkdir()
            script = root / "scripts/package.py"
            shutil.copyfile(Path(__file__).with_name("package.py"), script)
            subprocess.run(["git", "init", "-q", str(root)], check=True)
            for name in ("README.md", "THIRD_PARTY_NOTICES.md", "docs/preview.png",
                         "docs/tracked.md", "docs/untracked.txt", "unrelated.txt"):
                path = root / name
                path.parent.mkdir(parents=True, exist_ok=True)
                path.write_text("synthetic packaging test\n")
            subprocess.run(["git", "add", "README.md", "THIRD_PARTY_NOTICES.md",
                            "docs/preview.png", "docs/tracked.md", "unrelated.txt"],
                           cwd=root, check=True)
            subprocess.run([sys.executable, str(script), str(dest)], check=True)
            self.assertTrue((dest / "docs/tracked.md").is_file())
            self.assertFalse((dest / "docs/untracked.txt").exists())
            self.assertFalse((dest / "unrelated.txt").exists())
            os.symlink("tracked.md", root / "docs/link.md")
            subprocess.run(["git", "add", "docs/link.md"], cwd=root, check=True)
            result = subprocess.run([sys.executable, str(script), str(dest)],
                                    capture_output=True, text=True)
            self.assertNotEqual(result.returncode, 0)
            self.assertIn("Refusing non-regular", result.stderr)

    def test_archive_strips_host_metadata_and_writes_checksum(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp) / "demo"
            root.mkdir()
            source = root / "readme.txt"
            source.write_text("original synthetic data\n")
            dest = Path(tmp) / "demo.tar.gz"
            script = Path(__file__).with_name("archive.py")
            subprocess.run([sys.executable, str(script), str(root), str(dest)], check=True)
            original = dest.read_bytes()
            self.assertEqual(original[4:8], b"\0\0\0\0")  # no gzip timestamp
            with tarfile.open(dest) as archive:
                for member in archive.getmembers():
                    self.assertEqual((member.uid, member.gid, member.uname, member.gname, member.mtime), (0, 0, "", "", 0))
                    self.assertEqual(member.pax_headers, {})
            checksum = dest.with_name("demo.sha256").read_text().split()[0]
            self.assertEqual(checksum, hashlib.sha256(original).hexdigest())
            os.utime(source, (100000, 100000))
            subprocess.run([sys.executable, str(script), str(root), str(dest)], check=True)
            self.assertEqual(original, dest.read_bytes())


if __name__ == "__main__":
    unittest.main()
