#!/usr/bin/env python3
"""Create an archive without host ownership, xattrs, or macOS sidecar metadata."""
import gzip
import hashlib
from pathlib import Path
import os
import sys
import tarfile
import tempfile

source, dest = (Path(p).resolve() for p in sys.argv[1:])


def normalize(member):
    if not (member.isfile() or member.isdir()):
        raise ValueError(f"refusing special archive entry: {member.name}")
    member.uid = member.gid = 0
    member.uname = member.gname = ""
    member.mtime = 0
    member.pax_headers = {}
    member.mode = 0o755 if member.isdir() or member.mode & 0o111 else 0o644
    return member


with tempfile.NamedTemporaryFile(dir=dest.parent, delete=False) as output:
    temporary = Path(output.name)
    try:
        with gzip.GzipFile(filename="", mode="wb", fileobj=output, mtime=0) as compressed:
            with tarfile.open(fileobj=compressed, mode="w", format=tarfile.PAX_FORMAT) as archive:
                archive.add(source, arcname=source.name, filter=normalize)
    except BaseException:
        temporary.unlink(missing_ok=True)
        raise
try:
    os.replace(temporary, dest)
finally:
    temporary.unlink(missing_ok=True)
digest = hashlib.sha256()
with dest.open("rb") as stream:
    for chunk in iter(lambda: stream.read(1 << 20), b""):
        digest.update(chunk)
checksum = dest.with_name(dest.name.removesuffix(".tar.gz") + ".sha256")
checksum.write_text(f"{digest.hexdigest()}  {dest.name}\n")
