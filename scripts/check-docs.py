#!/usr/bin/env python3
"""Check local Markdown/HTML image link destinations, without fetching URLs."""
from pathlib import Path
import re
import sys
from urllib.parse import unquote, urlsplit

root = Path(__file__).resolve().parent.parent
errors = []
for page in sorted(root.rglob("*.md")):
    relative = page.relative_to(root)
    if any(part in {".git", "bin", "dist", ".venv"} for part in relative.parts):
        continue
    text = page.read_text()
    links = re.findall(r"\[[^\]]*\]\(([^)\s]+)\)", text)
    links += re.findall(r'<img\b[^>]*\bsrc="([^"]+)"', text)
    for link in links:
        parsed = urlsplit(link)
        if parsed.scheme or parsed.netloc or not parsed.path:
            continue
        path = (page.parent / unquote(parsed.path)).resolve()
        if not path.is_relative_to(root) or not path.exists():
            errors.append(f"{relative}: missing/outside local destination {link}")
if errors:
    sys.exit("\n".join(errors))
print("Local documentation links: passed (external URLs/anchors not fetched)")
