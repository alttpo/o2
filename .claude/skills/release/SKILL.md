---
name: release
description: Release o2. Tags the current commit with a user-supplied version (e.g. v0.5.0d), pushes the tag so GitHub Actions builds it and creates the GitHub release, then rewrites the release description as a bulleted list of player-facing changes since the previous tag. Use when the user asks to release, cut a release, or tag a version of o2.
argument-hint: <version, e.g. v0.5.1 or v0.5.0d>
---

# Release o2

The version comes from the user (the skill argument, or ask if missing).

## 1. Validate the version

It must match `^v[0-9]+\.[0-9]+\.[0-9]+[A-Za-z]*$` (a `v`, a 3-part dotted number, and an optional suffix such as `v0.5.0d`). If it does not, tell the user and stop. Also stop if the tag already exists locally or on origin (`git tag -l <v>`, `git ls-remote --tags origin <v>`).

## 2. Check state

- `git status --short` and `git log -1 --oneline`: the tag goes on the current commit (HEAD). Warn the user if there are uncommitted changes to tracked files, or if HEAD is not pushed to origin (`git branch -r --contains HEAD` is empty), since CI builds what is on GitHub. Untracked files are fine.
- Record the previous tag before creating the new one: `git describe --tags --abbrev=0 HEAD` (this finds the nearest earlier tag, so it also handles suffix tags like `v0.5.0c`).

## 3. Tag and push

Pushing a tag is outward-facing and triggers a public release. Make sure the user has asked for this release (invoking the skill with a version counts).

```sh
git tag -a <version> -m "<version>"
git push origin <version>
```

The tag message is exactly the version string.

## 4. Wait for CI

`.github/workflows/main.yml` runs on `v*` tags, builds Linux/Windows/macOS binaries, and uploads them to a GitHub release via `svenstaro/upload-release-action` (which creates the release).

- Find the run: `gh run list --branch <version> --limit 5` (tag pushes show the tag as the branch).
- Wait in the background rather than polling, e.g. `gh run watch <run-id> --exit-status` with `run_in_background`.
- If the build fails, report that with the failing job and stop. Do not edit the release notes of a failed build.
- Confirm the release exists: `gh release view <version>`.

## 5. Write the release notes

Gather changes since the previous tag: `git log --no-merges --format='%h %s%n%b' <prev>..<version>` (and look at diffs when a subject is unclear).

Write a bulleted list of **player-facing** changes only:

- One brief line per bullet, no sub-bullets, no commit hashes, no file/function names, no internal details (refactors, CI, tests, dependency bumps, docs, tooling are omitted entirely).
- Order: new features first, then changes to existing features, then bug fixes. No headings or blank lines between groups; just one continuous bulleted list in that order.
- Merge commits that describe the same change into one bullet. Write in plain language a player would understand (e.g. "Bombed and dashed walls opened by another player now open for you too").
- If nothing player-facing changed, say so in a single bullet.

Show the draft to the user, then apply:

```sh
gh release edit <version> --notes-file <scratchpad file>
```

Finish by giving the release URL (`gh release view <version> --json url -q .url`).
