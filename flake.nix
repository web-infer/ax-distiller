{
  inputs.nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
  outputs =
    { self, nixpkgs }:
    let
      system = "x86_64-linux";
      pkgs = import nixpkgs {
        inherit system;
      };
      codegenDeps = with pkgs; [
        buf
        sqlc
        protoc-gen-go
        protoc-gen-connect-go
        protoc-gen-es
      ];
    in
    {
      apps.${system}.codegen =
        let
          codegen = pkgs.writeShellApplication {
            name = "codegen";
            runtimeInputs = codegenDeps;
            text = ''
              ${pkgs.sqlc}/bin/sqlc generate
              ${pkgs.buf}/bin/buf generate
            '';
          };
        in
        {
          type = "app";
          program = "${codegen}/bin/codegen";
        };

      devShells.${system}.default =
        let
          libs = with pkgs; [
            ungoogled-chromium
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
            ++ codegenDeps
          );

          NIX_LD = builtins.readFile "${pkgs.stdenv.cc}/nix-support/dynamic-linker";
          LD_LIBRARY_PATH = "${pkgs.lib.makeLibraryPath libs}:$LD_LIBRARY_PATH";

          shellHook = ''
            echo "Devshell activated."
          '';
        };
    };
}
