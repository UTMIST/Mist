#!/usr/bin/env python3
"""Validate local Markdown links/anchors and current Bash examples without executing them."""
from pathlib import Path
import re
import subprocess
import sys
from urllib.parse import unquote

ROOT = Path(__file__).resolve().parents[1]

def slugs(path):
    result = set()
    duplicates = {}
    fenced = False
    for line in path.read_text().splitlines():
        if line.startswith("```"):
            fenced = not fenced
        if fenced:
            continue
        match = re.match(r"^#{1,6}\s+(.+?)\s*#*\s*$", line)
        if not match:
            continue
        text = match.group(1).lower().replace("`", "")
        text = re.sub(r"[^\w\- ]", "", text).replace(" ", "-")
        count = duplicates.get(text, 0)
        duplicates[text] = count + 1
        result.add(text + (f"-{count}" if count else ""))
    return result

errors = []
links = 0
shells = 0
names = subprocess.check_output(
    ["git", "ls-files", "--cached", "--others", "--exclude-standard", "-z"], cwd=ROOT
).decode().split("\0")
for name in sorted(set(names)):
    path = ROOT / name
    if not name.endswith(".md") or not path.is_file():
        continue
    lines = path.read_text().splitlines()
    fence = None
    block = []
    for number, line in enumerate(lines, 1):
        if line.startswith("```"):
            if fence is None:
                fence = line[3:].strip()
                block = []
            else:
                if fence in ("bash", "sh") and "docs/archive/" not in name:
                    check = subprocess.run(["bash", "-n"], input="\n".join(block), text=True, capture_output=True)
                    shells += 1
                    if check.returncode:
                        errors.append(f"{name}:{number}: Bash syntax: {check.stderr.strip()}")
                fence = None
            continue
        if fence is not None:
            block.append(line)
            continue
        for match in re.finditer(r"\[[^\]]*\]\(([^\s)]+)\)", line):
            target = match.group(1)
            if re.match(r"^[a-zA-Z][a-zA-Z0-9+.-]*:", target):
                continue
            filename, _, anchor = target.partition("#")
            destination = (path.parent / unquote(filename)).resolve() if filename else path
            links += 1
            if not destination.exists():
                errors.append(f"{name}:{number}: missing {target}")
            elif anchor and destination.suffix == ".md" and unquote(anchor) not in slugs(destination):
                errors.append(f"{name}:{number}: missing anchor {target}")
    if fence is not None:
        errors.append(f"{name}: unclosed code fence")
if errors:
    print("\n".join(errors), file=sys.stderr)
    sys.exit(1)
print(f"Documentation passed: {links} local links/anchors, {shells} current shell examples.")
