# SDK package release

The npm package is `@getopenpost/sdk`. Its SemVer is independent from the SocialFlow.ai app version.

## Routine releases

Every publishable change under `packages/sdk/` must increase its stable package version. Pull-request and main CI compare the package with the previous commit; tag CI compares a new tag with the previous published release. All of them fail when package sources changed without a version increase.

The app's `v*` release workflow publishes the SDK independently, beside the Hosted deployment:

1. An absent version publishes once through npm trusted publishing.
2. An existing version with the same tarball integrity is safe to reuse.
3. An existing version with different integrity stops the package job and requires a version increase. It does not block the app deployment.
4. An unclear publish result triggers registry reconciliation, not a blind retry.

The package job then verifies provenance, integrity and a clean registry install.

## Package-only release or recovery

Increase `packages/sdk/package.json`, run `bun scripts/tasks.mjs check npm-packages`, commit and push to `main`, then run:

```bash
gh workflow run release.yml --ref main -f package=sdk
```

This dispatch publishes and verifies only the SDK, retaining trusted-publisher provenance without rebuilding or redeploying the app. Finish when that job succeeds and the registry serves the intended version.

Never reuse a published version with different contents. Bump the patch version instead.
