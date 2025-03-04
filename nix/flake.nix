{
  description = "fusion development environment";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixpkgs-unstable";
    flake-utils.url = "github:numtide/flake-utils";
  };

  outputs = { self, nixpkgs, flake-utils }:
    (flake-utils.lib.eachDefaultSystem (system: nixpkgs.lib.fix (flake:
      let
        pkgs = nixpkgs.legacyPackages.${system};
      in
      {
        packages = {
          deno = pkgs.deno;
          git = pkgs.git;
          go = pkgs.go_1_24;
          golangci-lint = pkgs.golangci-lint;
          krane = pkgs.krane;
          kubectl = pkgs.kubectl;
          stern = pkgs.stern;
          task = pkgs.go-task;

          fusion = pkgs.writeShellScriptBin "fusion" ''
            go run main.go "$@"
          '';
        };

        devShell = pkgs.mkShell {
          packages = builtins.attrValues flake.packages;
        };
      }
    )));
}
