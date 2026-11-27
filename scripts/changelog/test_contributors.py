import unittest

from contributors import author_logins, contributor_section, external_logins, linked_logins, previous_release, thanks_section


class ContributorsTest(unittest.TestCase):
    def test_previous_release_skips_the_current_draft(self):
        releases = [
            {"tag_name": "v1.2.0", "draft": True},
            {"tag_name": "v1.1.0", "draft": False},
        ]
        self.assertEqual(previous_release(releases, "v1.2.0"), "v1.1.0")

    def test_author_logins_filters_bots_and_deduplicates_case_insensitively(self):
        compare = {
            "commits": [
                {"author": {"login": "QuentinHsu", "type": "User"}},
                {"author": {"login": "quentinhsu", "type": "User"}},
                {"author": {"login": "devin-ai-integration[bot]", "type": "Bot"}},
                {"author": {"login": "actions-user", "type": "Bot"}},
                {"author": None},
            ]
        }
        self.assertEqual(author_logins(compare), ["QuentinHsu"])

    def test_external_logins_filters_owner_and_collaborators(self):
        grants = {
            "wibus-wee": {"admin": True},
            "triage-user": {"triage": True},
            "push-user": {"push": True},
            "QuentinHsu": {"pull": True},
        }

        def api(path):
            login = path.split("/")[-2]
            return {"user": {"permissions": grants[login]}}

        logins = external_logins(
            "owner/repo",
            ["owner", "wibus-wee", "triage-user", "push-user", "QuentinHsu"],
            api,
        )
        self.assertEqual(logins, ["QuentinHsu"])

    def test_thanks_section_links_one_or_more_contributors(self):
        self.assertEqual(
            linked_logins(["one", "two", "three"]),
            "[@one](https://github.com/one), [@two](https://github.com/two) and [@three](https://github.com/three)",
        )
        self.assertEqual(
            thanks_section(["QuentinHsu"]),
            "### External contributors\n\nThanks to [@QuentinHsu](https://github.com/QuentinHsu) for contributing to this release.\n",
        )
        self.assertEqual(thanks_section([]), "")

    def test_contributor_section_uses_the_previous_published_release_range(self):
        calls = []

        def api(path):
            calls.append(path)
            if path == "repos/owner/repo/releases?per_page=100":
                return [{"tag_name": "v1.2.0", "draft": True}, {"tag_name": "v1.1.0", "draft": False}]
            if path == "repos/owner/repo/compare/v1.1.0...v1.2.0":
                return {"commits": [
                    {"author": {"login": "QuentinHsu", "type": "User"}},
                    {"author": {"login": "owner", "type": "User"}},
                ]}
            if path == "repos/owner/repo/collaborators/QuentinHsu/permission":
                return {"user": {"permissions": {"pull": True}}}
            raise AssertionError(path)

        section = contributor_section("owner/repo", "v1.2.0", api)
        self.assertIn("[@QuentinHsu](https://github.com/QuentinHsu)", section)
        self.assertNotIn("owner](https://github.com/owner)", section)
        self.assertEqual(calls[1], "repos/owner/repo/compare/v1.1.0...v1.2.0")


if __name__ == "__main__":
    unittest.main()
