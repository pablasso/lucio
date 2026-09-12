#!/usr/bin/env python3
"""Inspect local archives without extracting or executing their contents."""
from pathlib import Path, PurePosixPath
import sys
import tarfile

if len(sys.argv) < 2:
    sys.exit("Usage: check-release.py dist/*.tar.gz")
for arg in sys.argv[1:]:
    path = Path(arg)
    prefix = path.name.removesuffix(".tar.gz")
    with tarfile.open(path, "r:gz") as archive:
        members = archive.getmembers()
        assert len(members) < 1000, "unexpected archive size"
        for member in members:
            parts = PurePosixPath(member.name).parts
            assert parts and parts[0] == prefix and ".." not in parts
            assert not any(part.startswith("._") for part in parts), "unexpected macOS metadata"
            assert member.isfile() or member.isdir(), "links/special files are not allowed"
            assert member.size < 64 << 20
            assert member.uid == member.gid == 0 and not member.uname and not member.gname
            assert not member.pax_headers, "unexpected extended metadata"
        for relative in ["lucio", "paperboy", "LICENSE", "NOTICE", "README.md",
                         "THIRD_PARTY_NOTICES.md", "BUILDINFO.txt", "docs/preview.png",
                         "third_party/licenses/go-bsd.txt", "third_party/licenses/serial.txt",
                         "internal/renderer/fonts/OFL.txt", "examples/hello.json",
                         "examples/image.json", "examples/picture.png", "examples/page.pdf",
                         "deploy/lucio.service", "schemas/lucio.scene.v1.json",
                         "tools/usb-setup.py"]:
            member = archive.getmember(prefix + "/" + relative)
            assert member.isfile() and member.size > 0, relative
            if relative in {"lucio", "paperboy"}:
                assert member.mode & 0o111, "binary not executable"
        info = archive.extractfile(prefix + "/BUILDINFO.txt").read().decode()
        assert "source_commit=" in info and "github.com/pablasso/lucio" in info
        assert all(p not in info for p in ("/Users/", "/home/", "/private/", "/tmp/")), info
    print(f"Archive contents and build metadata: {path.name}: passed")
