# Releasing

A `vX.Y.Z` tag on `main` releases the CLI to every channel
(`.github/workflows/release.yml`):

| Channel | What is published |
|---|---|
| GitHub Release (this repo) | Archives for macOS, Linux and Windows, `checksums.txt`, build provenance |
| Homebrew (`sekondbrainailabs/s9n`) | The same archives as `cli-vX.Y.Z` on the tap, plus the regenerated formula |
| npm (`kemory`) | The launcher, which downloads and verifies the archive for the user's platform |
| PyPI (`kemory`) | One wheel per platform carrying the binary |

A tag with a `-` (`v0.7.0-rc.1`) is a pre-release and stops after the GitHub Release.

## Cutting a release

1. Make sure `main` is green.
2. Tag and push:

   ```bash
   git tag -a v0.7.0 -m "kemory 0.7.0"
   git push origin v0.7.0
   ```

3. Check each channel:

   ```bash
   brew upgrade kemory && kemory --version
   npx -y kemory@0.7.0 --version
   uvx kemory@0.7.0 --version
   ```

Versions continue the numbering of the previous Python CLI (last: 0.6.14), so
existing installs see each release as an upgrade.

## One-time setup

- Repository variable `HOMEBREW_RELEASE_APP_ID` and secret
  `HOMEBREW_RELEASE_APP_PRIVATE_KEY`: a GitHub App with Contents:write on
  `SeKondBrainAILabs/homebrew-s9n` only.
- Environments `npm` and `pypi` in this repository.
- npm package `kemory` → Settings → Trusted publisher: GitHub Actions,
  `SeKondBrainAILabs` / `kemory-cli` / `release.yml` / environment `npm`, with
  "Allow npm publish" ticked. npm allows one trusted publisher per package.
- PyPI project `kemory` → Publishing → add a trusted publisher:
  `SeKondBrainAILabs` / `kemory-cli` / `release.yml` / environment `pypi`.
