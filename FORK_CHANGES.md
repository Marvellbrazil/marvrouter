# Fork changes

Notes for changes that exist only in this fork. `CHANGELOG.md` is owned by upstream and is taken from upstream on every sync, so fork notes live here to avoid merge conflicts.

## Unreleased

- **Updater**: `scripts/update-marvrouter.sh` falls back to the GitHub releases feed when the Releases API is unavailable, and reports a clean error if both sources fail.
- **Upstream sync**: `scripts/sync-upstream.sh` resolves upstream `CHANGELOG.md` conflicts, keeps fork lines on conflicting hunks in the fork-owned UI files, aborts on any other conflict, and the workflow opens an issue when a sync fails.
