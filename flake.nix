{
  description = "Lightweight zenity prompt UI for the OpenSnitch daemon (unofficial)";

  inputs.nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";

  outputs = { self, nixpkgs }:
    let
      systems = [ "x86_64-linux" "aarch64-linux" ];
      forAll = f: nixpkgs.lib.genAttrs systems (s: f nixpkgs.legacyPackages.${s});
    in {
      packages = forAll (pkgs: rec {
        opensnitch-zenity = pkgs.buildGoModule {
          pname = "opensnitch-zenity";
          version = "1.0.0";
          src = self;
          vendorHash = "sha256-0UX4f5GZK2Re53ooF9k/gyLxmr6vHfuXAAADmAlYRcc=";
          subPackages = [ "cmd/opensnitch-zenity" ];
          env.CGO_ENABLED = 0;
          nativeBuildInputs = [ pkgs.makeWrapper ];
          postInstall = ''
            wrapProgram $out/bin/opensnitch-zenity \
              --prefix PATH : ${pkgs.lib.makeBinPath [ pkgs.zenity pkgs.libnotify pkgs.wl-clipboard pkgs.xclip ]}
            install -Dm644 contrib/systemd/opensnitch-zenity.service \
              $out/lib/systemd/user/opensnitch-zenity.service
            substituteInPlace $out/lib/systemd/user/opensnitch-zenity.service \
              --replace-fail /usr/bin/opensnitch-zenity $out/bin/opensnitch-zenity
          '';
          meta = {
            description = "Lightweight zenity prompt UI for the OpenSnitch daemon (unofficial)";
            homepage = "https://github.com/vmartinv/opensnitch-zenity";
            license = pkgs.lib.licenses.gpl3Plus;
            platforms = pkgs.lib.platforms.linux;
            mainProgram = "opensnitch-zenity";
          };
        };
        default = opensnitch-zenity;
      });
    };
}
