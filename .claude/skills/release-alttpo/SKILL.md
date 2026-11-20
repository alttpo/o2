---
name: release-alttpo
description: Release alttpo (the ../alttpo repository, not o2). Tags the current alttpo commit with a user-supplied tag name, pushes it so GitHub Actions builds it and creates the GitHub release, then rewrites the release description as a bulleted list of player-facing changes since the previous tag. Use when the user asks to release, cut a release, or tag a build of alttpo.
argument-hint: <tag name, e.g. esync-v20261120-o2-compat>
---

# Release alttpo

All git and gh commands run in the alttpo repository at `/Users/jsd1982/Developer/me/alttpo/alttpo` (`git -C` or `cd`), origin `alttpo/alttpo`. The tag name comes from the user (the skill argument, or ask if missing); use it verbatim. Recent examples: `esync-v20261119-o2-compat`, `esync-v20260324`.

## 1. Check state

- Stop if the tag already exists locally or on origin (`git tag -l <tag>`, `git ls-remote --tags origin <tag>`).
- `git status --short` and `git log -1 --oneline`: the tag goes on the current commit (HEAD). Warn the user if tracked files have uncommitted changes, or if HEAD isn't pushed to origin (`git branch -r --contains HEAD` is empty), since CI builds what's on GitHub. Untracked files (screen recordings etc.) are normal; ignore them.
- Record the previous tag before tagging: `git describe --tags --abbrev=0 HEAD`.
- Heads up: a tag starting with `stable-` posts to the Discord #stable-builds channel; any other tag posts to #developer-builds. Mention which one applies when confirming with the user.

## 2. Tag and push

Pushing a tag is outward-facing and triggers a public release plus a Discord post. Invoking the skill with a tag name counts as the user's go-ahead.

```sh
git tag -a <tag> -m "<tag>"
git push origin <tag>
```

The tag message is exactly the tag name.

## 3. Wait for CI

`.github/workflows/ci.yml` runs on every push. For tag pushes, its package job builds and uploads the artifacts to a GitHub release via `softprops/action-gh-release`, which creates the release.

- Find the run: `gh run list --branch <tag> --limit 5` (tag pushes show the tag as the branch).
- Wait in the background rather than polling, e.g. `gh run watch <run-id> --exit-status` with `run_in_background`.
- If the build fails, report the failing job and stop. Don't edit the notes of a failed build.
- Confirm the release exists: `gh release view <tag>`.

## 4. Write the release notes

Gather changes since the previous tag: `git log --no-merges --format='%h %s%n%b' <prev>..<tag>` (look at diffs when a subject is unclear).

Write a bulleted list of **player-facing** changes only:

- One brief line per bullet, no sub-bullets, no commit hashes, no file/function/script names, no internal details. Omit refactors, CI, tests, tooling, docs, and anything a player wouldn't notice.
- Order: new features first, then changes to existing features, then bug fixes. No headings or blank lines; one continuous bulleted list in that order.
- Merge commits describing the same change into one bullet. Plain language a player would understand.
- If nothing player-facing changed, use a single bullet saying so.

Show the draft to the user, then apply:

```sh
gh release edit <tag> --notes-file <scratchpad file>
```

Finish by giving the release URL (`gh release view <tag> --json url -q .url`).
