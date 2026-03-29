{
  inputs.nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
  outputs =
    { self, nixpkgs }:
    let
      system = "x86_64-linux";
      pkgs = import nixpkgs {
        inherit system;
      };
    in
    {
      devShells.${system}.default =
        let
          libs = with pkgs; [
            ungoogled-chromium
            # fuse
            # stdenv.cc.cc
            # glib
            # nss
            # nspr
            # dbus
            # at-spi2-atk
            # cups
            # libdrm
            # expat
            # xorg.libX11
            # xorg.libXcomposite
            # xorg.libXdamage
            # xorg.libXext
            # xorg.libXfixes
            # xorg.libXrandr
            # libgbm
            # xorg.libxcb
            # libxkbcommon
            # pango
            # cairo
            # alsa-lib
            # fuse
            # libz
            # libudev-zero
          ];
        in
        pkgs.mkShell {
          name = "devenv";
          buildInputs = libs;
          nativeBuildInputs = (
            with pkgs;
            [
              pkg-config
            ]
          );

          NIX_LD = builtins.readFile "${pkgs.stdenv.cc}/nix-support/dynamic-linker";
          LD_LIBRARY_PATH = "${pkgs.lib.makeLibraryPath libs}:$LD_LIBRARY_PATH";

          shellHook = ''
            echo "Devshell activated."
          '';
        };
    };
}
