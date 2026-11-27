#!/usr/bin/env python3
"""Write a release thank-you section for contributors outside the maintainer team."""

import argparse
import json
import subprocess
import sys
from urllib.parse import quote

MAINTAINER_GRANTS = {"triage", "push", "maintain", "admin"}


def gh_api(path):
    output = subprocess.run(["gh", "api", path], check=True, text=True, stdout=subprocess.PIPE).stdout
    return json.loads(output)


def previous_release(releases, current):
    for release in releases:
        tag = release.get("tag_name")
        if tag and tag != current and not release.get("draft"):
            return tag
    return None


def author_logins(compare):
    logins = []
    seen = set()
    for commit in compare.get("commits", []):
        author = commit.get("author") or {}
        login = author.get("login")
        if not login or author.get("type") != "User":
            continue
        key = login.lower()
        if key.endswith("[bot]") or key in seen:
            continue
        seen.add(key)
        logins.append(login)
    return logins


def external_logins(repo, logins, api):
    external = []
    owner = repo.split("/", 1)[0].lower()
    for login in logins:
        if login.lower() == owner:
            continue
        path = f"repos/{repo}/collaborators/{quote(login, safe='')}/permission"
        response = api(path)
        grants = response.get("user", {}).get("permissions")
        if not isinstance(grants, dict):
            raise ValueError(f"GitHub returned no repository grants for {login}")
        if not any(grants.get(grant) is True for grant in MAINTAINER_GRANTS):
            external.append(login)
    return external


def linked_logins(logins):
    links = [f"[@{login}](https://github.com/{login})" for login in logins]
    if len(links) == 1:
        return links[0]
    return ", ".join(links[:-1]) + f" and {links[-1]}"


def thanks_section(logins):
    if not logins:
        return ""
    return (
        "### External contributors\n\n"
        f"Thanks to {linked_logins(logins)} for contributing to this release.\n"
    )


def contributor_section(repo, current, api=gh_api):
    releases = api(f"repos/{repo}/releases?per_page=100")
    base = previous_release(releases, current)
    if base is None:
        return ""
    basehead = f"{quote(base, safe='')}...{quote(current, safe='')}"
    compare = api(f"repos/{repo}/compare/{basehead}")
    return thanks_section(external_logins(repo, author_logins(compare), api))


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("repo", help="owner/repository")
    parser.add_argument("tag", help="release tag being published")
    args = parser.parse_args()
    try:
        print(contributor_section(args.repo, args.tag), end="")
    except (ValueError, OSError, subprocess.CalledProcessError) as exc:
        print(f"contributors: {exc}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
