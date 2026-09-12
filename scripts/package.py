#!/usr/bin/env python3
"""Copy only tracked, allowlisted release material, never untracked runtime files."""
from pathlib import Path
import shutil
import subprocess
import sys

root = Path(__file__).resolve().parent.parent
dest = Path(sys.argv[1]).resolve()
allowed = [
    "LICENSE", "NOTICE", "README.md", "CONTRIBUTING.md", "SECURITY.md",
    "THIRD_PARTY_NOTICES.md", "docs/", "examples/", "deploy/", "third_party/",
    "scripts/", "schemas/*.json", "tools/usb-setup.py", "tools/requirements.txt",
    "internal/renderer/fonts/OFL.txt",
]
files = subprocess.check_output(["git", "ls-files", "-z", "--", *allowed], cwd=root)
for raw in files.split(b"\0"):
    if not raw:
        continue
    relative = Path(raw.decode())
    source = root / relative
    if source.resolve() != source or source.is_symlink() or not source.is_file():
        sys.exit(f"Refusing non-regular release source: {relative}")
    target = dest / relative
    target.parent.mkdir(parents=True, exist_ok=True)
    shutil.copy2(source, target)
for required in ("README.md", "THIRD_PARTY_NOTICES.md", "docs/preview.png"):
    if not (dest / required).is_file():
        sys.exit(f"Required release file is missing/untracked: {required}")
