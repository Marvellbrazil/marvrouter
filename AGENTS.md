# AGENTS.md — Agent & Workflow Operating Guidelines

## GitHub Actions Quota Protection

- **Limited Actions Minutes**: GitHub Actions compute minutes are strictly limited. Every push to the `master` branch automatically triggers `.github/workflows/build-release.yml` (Next.js bundle + ARM64 gateway compilation + GitHub release).
- **Never Push Incrementally**: Do NOT push intermediate commits, speculative fixes, or work-in-progress turns to `origin/master`.
- **Verify Before Push**: Test and verify changes locally or via server inspection before pushing.
- **Explicit Push Confirmation**: Push to `origin/master` only when a milestone is completed and verified, or when the user explicitly instructs to push.
- **Use `[skip ci]` for Non-Build Changes**: Append `[skip ci]` to commit messages for documentation, comments, markdown, or non-code edits that do not require artifact rebuilding.
