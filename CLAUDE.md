# opensnitch-zenity

## Releasing vX.Y.Z

Ask the user before pushing, tagging or pushing to the AUR.

1. `make vet test`; clean tree. If the dialogs changed, run
   `contrib/screenshots/shoot.sh` (headless; mirrors `internal/prompt`).
2. Bump `version` in `flake.nix` and `pkgver` (`pkgrel=1`) in
   `contrib/PKGBUILD`. If `go.sum` changed, update the flake's `vendorHash`
   (below). Commit, push, tag, push tag; the Release workflow builds binaries.
3. Set `sha256sums` from the GitHub tag tarball, regenerate `.SRCINFO`
   (`makepkg --printsrcinfo`), test `makepkg -f` in a scratch copy, commit, push.
4. AUR (account `possum`, SSH key set up): clone
   `ssh://aur@aur.archlinux.org/opensnitch-zenity.git` to a scratch dir, copy
   in PKGBUILD and .SRCINFO, commit, push `master`.

`vendorHash`: no local Nix. Set `pkgs.lib.fakeHash`, copy tracked files +
`flake.nix` to a scratch dir (flakes ignore untracked files in a git
checkout), build with `docker run nixos/nix ... nix build .#` (enable
`nix-command flakes`), and use the `got:` hash.
