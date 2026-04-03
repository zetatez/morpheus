# Homebrew Tap for Morpheus

## Installation

```bash
brew tap zetatez/morpheus
brew install morpheus
```

## For Maintainers

When releasing a new version:

1. Build macOS binaries: `python release.py --version X.Y.Z`
2. Calculate sha256 for both tarballs:
   ```bash
   shasum -a 256 morph-darwin-amd64.tar.gz morph-darwin-arm64.tar.gz
   ```
3. Update `version` and `sha256` values in `Formula/morpheus.rb`
4. Commit and tag:
   ```bash
   git add Formula/morpheus.rb
   git commit -m "morpheus vX.Y.Z"
   git tag vX.Y.Z
   git push --tags
   ```
