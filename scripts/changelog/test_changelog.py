import unittest
import os
import subprocess
import tempfile
from pathlib import Path

from changelog import notes


FAKE_GH = r'''#!/usr/bin/env python3
import json
import sys

args = sys.argv[1:]
if args[0] == "release":
    print("See-arm64.dmg\nSee_amd64.deb")
elif args[0] == "api":
    endpoint = args[1]
    if endpoint == "repos/owner/repo/releases?per_page=100":
        print(json.dumps([
            {"tag_name": "v1.2.0", "draft": True},
            {"tag_name": "v1.1.0", "draft": False},
        ]))
    elif endpoint == "repos/owner/repo/compare/v1.1.0...v1.2.0":
        print(json.dumps({"commits": [
            {"author": {"login": "wibus-wee", "type": "User"}},
            {"author": {"login": "QuentinHsu", "type": "User"}},
            {"author": {"login": "QuentinHsu", "type": "User"}},
            {"author": {"login": "devin-ai-integration[bot]", "type": "Bot"}},
        ]}))
    elif endpoint.startswith("repos/owner/repo/collaborators/"):
        login = endpoint.split("/")[-2]
        grants = {"wibus-wee": {"admin": True}, "QuentinHsu": {"pull": True}}
        print(json.dumps({"user": {"permissions": grants[login]}}))
    else:
        raise SystemExit(f"unexpected endpoint: {endpoint}")
else:
    raise SystemExit(f"unexpected command: {args}")
'''


class ReleaseNotesTest(unittest.TestCase):
    def test_extracts_one_version_without_neighboring_sections(self):
        text = "# Changelog\n## Unreleased\n- Next\n## [1.2.0] - 2026-01-01\n### Fixed\n- One\n## 1.1.0\n- Old\n"
        self.assertEqual(notes(text, "v1.2.0"), "### Fixed\n- One")

    def test_rejects_missing_empty_and_duplicate_sections(self):
        for text in ("## 1.1.0\n- Old", "## 1.2.0\n### Fixed\n", "## 1.2.0\n- A\n## 1.2.0\n- B"):
            with self.subTest(text=text), self.assertRaises(ValueError):
                notes(text, "1.2.0")

    def test_release_body_uses_reviewed_notes_and_external_contributors_on_each_run(self):
        root = Path(__file__).resolve().parents[2]
        with tempfile.TemporaryDirectory() as tmp:
            path = Path(tmp)
            gh = path / "gh"
            gh.write_text(FAKE_GH)
            gh.chmod(0o755)
            source = path / "notes.md"
            source.write_text("### Fixed\n\n- Reviewed fix.\n")
            env = dict(os.environ, PATH=f"{tmp}{os.pathsep}{os.environ['PATH']}")
            cmd = [str(root / ".github/release-body.sh"), "v1.2.0", "owner/repo", str(source)]
            first = subprocess.check_output(cmd, text=True, env=env)
            self.assertEqual(first, subprocess.check_output(cmd, text=True, env=env))
            self.assertEqual(first.count("### Download"), 1)
            self.assertIn("[Apple silicon](https://github.com/owner/repo/releases/download/v1.2.0/See-arm64.dmg)", first)
            self.assertIn("### Fixed\n\n- Reviewed fix.\n\n---\n\n### External contributors", first)
            self.assertIn("Thanks to [@QuentinHsu](https://github.com/QuentinHsu) for contributing to this release.", first)
            self.assertNotIn("wibus-wee](https://github.com/wibus-wee)", first)


if __name__ == "__main__":
    unittest.main()
