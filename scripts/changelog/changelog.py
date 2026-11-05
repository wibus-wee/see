#!/usr/bin/env python3
"""Collect commit evidence and extract reviewed release notes."""

import argparse
import re
import subprocess
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]


def git(*args):
    return subprocess.run(["git", *args], cwd=ROOT, check=True, text=True, stdout=subprocess.PIPE).stdout.strip()


def notes(text, version):
    version = version.removeprefix("v")
    matches = []
    current = None
    for line in text.splitlines():
        if line.startswith("## "):
            if current is not None:
                matches.append("\n".join(current).strip())
                current = None
            token = line[3:].split(maxsplit=1)[0] if line[3:].strip() else ""
            match = re.match(r"^\[([^]]+)\](?:\([^)]*\))?$", token)
            heading = (match.group(1) if match else token).removeprefix("v")
            if heading == version:
                current = []
        elif current is not None:
            current.append(line)
    if current is not None:
        matches.append("\n".join(current).strip())
    if len(matches) != 1 or not matches[0] or not re.search(r"(?m)^\s*[-*] \S", matches[0]):
        raise ValueError(f"CHANGELOG.md needs exactly one nonempty section with a bullet for {version}")
    return matches[0]


def context(base, end):
    if not base:
        try:
            base = git("describe", "--tags", "--abbrev=0", "--match", "v[0-9]*", end)
        except subprocess.CalledProcessError as exc:
            raise ValueError("No reachable version tag; supply --from with a base commit or tag") from exc
    base_id = git("rev-parse", "--verify", base + "^{commit}")
    end_id = git("rev-parse", "--verify", end + "^{commit}")
    if subprocess.run(["git", "merge-base", "--is-ancestor", base_id, end_id], cwd=ROOT).returncode:
        raise ValueError(f"{base} is not an ancestor of {end}")
    history = git("log", "--reverse", "--format=commit %H%n%s%n%b", "--stat", f"{base_id}..{end_id}")
    return f"Base: {base} ({base_id})\nEnd: {end} ({end_id})\n\n{history or '(No commits in this range.)'}"


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    commands = parser.add_subparsers(dest="command", required=True)
    evidence = commands.add_parser("context", help="collect commits since a version tag")
    evidence.add_argument("--from", dest="base", help="base commit or tag (excluded)")
    evidence.add_argument("--to", default="HEAD", help="end commit or tag (included)")
    extract = commands.add_parser("notes", help="extract a reviewed version section")
    extract.add_argument("version")
    args = parser.parse_args()
    try:
        if args.command == "context":
            print(context(args.base, args.to))
        else:
            print(notes((ROOT / "CHANGELOG.md").read_text(encoding="utf-8"), args.version))
    except (ValueError, OSError, subprocess.CalledProcessError) as exc:
        print(f"changelog: {exc}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
