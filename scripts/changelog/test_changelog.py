import unittest
import os
import subprocess
import tempfile
from pathlib import Path

from changelog import notes


class ReleaseNotesTest(unittest.TestCase):
    def test_extracts_one_version_without_neighboring_sections(self):
        text = "# Changelog\n## Unreleased\n- Next\n## [1.2.0] - 2026-01-01\n### Fixed\n- One\n## 1.1.0\n- Old\n"
        self.assertEqual(notes(text, "v1.2.0"), "### Fixed\n- One")

    def test_rejects_missing_empty_and_duplicate_sections(self):
        for text in ("## 1.1.0\n- Old", "## 1.2.0\n### Fixed\n", "## 1.2.0\n- A\n## 1.2.0\n- B"):
            with self.subTest(text=text), self.assertRaises(ValueError):
                notes(text, "1.2.0")

    def test_release_body_uses_reviewed_notes_on_each_run(self):
        root = Path(__file__).resolve().parents[2]
        with tempfile.TemporaryDirectory() as tmp:
            path = Path(tmp)
            gh = path / "gh"
            gh.write_text("#!/bin/sh\nprintf '%s\\n' 'See-arm64.dmg' 'See_amd64.deb'\n")
            gh.chmod(0o755)
            source = path / "notes.md"
            source.write_text("### Fixed\n\n- Reviewed fix.\n")
            env = dict(os.environ, PATH=f"{tmp}{os.pathsep}{os.environ['PATH']}")
            cmd = [str(root / ".github/release-body.sh"), "v1.2.0", "owner/repo", str(source)]
            first = subprocess.check_output(cmd, text=True, env=env)
            self.assertEqual(first, subprocess.check_output(cmd, text=True, env=env))
            self.assertEqual(first.count("### Download"), 1)
            self.assertIn("[Apple silicon](https://github.com/owner/repo/releases/download/v1.2.0/See-arm64.dmg)", first)
            self.assertTrue(first.endswith(source.read_text()))


if __name__ == "__main__":
    unittest.main()
