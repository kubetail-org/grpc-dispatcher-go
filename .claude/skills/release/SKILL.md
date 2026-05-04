---
name: release
description: Create GitHub releases for the grpc-dispatcher-go library by tagging the repo with the appropriate semver version.
disable-model-invocation: true
---

# Release Skill

Tag and push a new semver release for the `grpc-dispatcher-go` library.

## Steps

1. **Detect upstream remote** — find the remote that points to `kubetail-org/grpc-dispatcher-go`:

   ```sh
   git remote -v | grep 'kubetail-org/grpc-dispatcher-go' | head -1 | awk '{print $1}'
   ```

   Store the result as `{upstream}`. If no remote matches, stop and ask the user which remote to use.

2. **Verify branch and state**:
   - Run `git fetch {upstream}` to update remote refs
   - Run `git status -uno` to confirm working tree is clean
   - Run `git rev-parse HEAD` and `git rev-parse {upstream}/main` to confirm HEAD is at `{upstream}/main`

   If working tree is dirty or HEAD is not at `{upstream}/main`, stop and ask the user to resolve before continuing.

3. **Find latest tag**:

   ```sh
   git tag --list "v*" | sort -V | tail -1
   ```

4. **Check for changes since latest tag** — run:

   ```sh
   git log {latest-tag}..HEAD --oneline
   ```

   - If no commits exist since the latest tag, stop (no release needed) and tell the user.

5. **Determine new version using semver** — analyze commits:
   - **Patch** (`Z+1`): bug fixes, tests, docs, refactors, chores, ci changes
   - **Minor** (`Y+1`, reset `Z=0`): new backwards-compatible features
   - **Major** (`X+1`, reset `Y=0`, `Z=0`): breaking changes

   Use conventional commit prefixes as hints (`fix:` → patch, `feat:` → minor, `BREAKING CHANGE` → major), but read the actual messages to make a judgment call.

6. **Confirm with user before tagging** — present a summary:

   ```
   Current tag  | New tag    | Reason
   -------------|------------|-------
   v0.1.5       | v0.2.0     | new feature: ...
   ```

   Wait for explicit user confirmation before proceeding.

7. **Tag and push**:

   ```sh
   git tag -s v{X.Y.Z} -m "release: v{X.Y.Z}"
   git push {upstream} v{X.Y.Z}
   ```

   Confirm the push succeeded.

## Rules

- NEVER skip the user confirmation step — always show the summary and wait for approval.
- NEVER tag or push without user confirmation.
- NEVER use `--no-verify` or `--force` when pushing tags.
- If `git status` shows uncommitted changes, stop immediately and tell the user to commit or stash first.
- If HEAD is not at `{upstream}/main`, stop immediately and tell the user to check out `{upstream}/main` first.
