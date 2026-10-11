# opensnitch-zenity

Go prompt UI for the OpenSnitch daemon; see README.md for behaviour and
development commands (`make test`, `make vet`, `make proto`).

## Releasing a new version

Distributed through GitHub Releases (built by `.github/workflows/release.yml`),
the AUR package `opensnitch-zenity` (maintainer account `possum`, SSH key
already set up), and the Nix flake. The AUR checksum only exists once the tag
is on GitHub, so the order matters. For version X.Y.Z:

1. Check `make vet test` pass and the working tree is clean.
2. If the dialogs changed, run `contrib/screenshots/shoot.sh` (headless; keep
   its zenity arguments in sync with `internal/prompt/prompt.go`), look at
   `docs/*.png`, and update the alt text in README.md.
3. Bump the version: `version = "X.Y.Z"` in `flake.nix`, `pkgver=X.Y.Z` and
   `pkgrel=1` in `contrib/PKGBUILD`. Update `optdepends` if new optional
   runtime tools are used. If `go.mod`/`go.sum` changed since the last
   release, update `vendorHash` in `flake.nix` (see below) so the tag's
   flake builds. Commit ("Release vX.Y.Z").
4. Push and tag: `git push origin main && git tag vX.Y.Z && git push origin vX.Y.Z`.
   Watch the Release workflow (`gh run watch`) and check the release has the
   amd64/arm64 tarballs and SHA256SUMS (`gh release view vX.Y.Z`).
5. Checksum: `curl -sL https://github.com/vmartinv/opensnitch-zenity/archive/vX.Y.Z.tar.gz | sha256sum`,
   put it in `sha256sums` in `contrib/PKGBUILD`, then from `contrib/`:
   `makepkg --printsrcinfo > .SRCINFO`. Test-build in a scratch copy
   (`makepkg -f` there, it runs `check()`). Commit ("PKGBUILD: checksum for
   vX.Y.Z") and push. The PKGBUILD inside the tagged tarball keeps the old
   checksum; that is expected.
6. AUR: clone `ssh://aur@aur.archlinux.org/opensnitch-zenity.git` into a
   scratch dir, copy `contrib/PKGBUILD` and `contrib/.SRCINFO` in, commit
   ("Update to X.Y.Z"), push to `master`. Verify at
   https://aur.archlinux.org/packages/opensnitch-zenity.
Updating `vendorHash`: Nix is not installed locally, so use Docker. Set
`vendorHash = pkgs.lib.fakeHash;`, copy the tracked files plus `flake.nix`
into a scratch dir (untracked files are invisible to flakes inside a git
checkout), and run
`docker run --rm -v <dir>:/src:ro nixos/nix sh -c 'cp -r /src /w && cd /w && nix --extra-experimental-features "nix-command flakes" build .#'`.
Put the `got:` hash in `vendorHash` and rebuild to confirm.

Pushing to GitHub, tagging and pushing to the AUR are public: confirm with the
user before doing them.
