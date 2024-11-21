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
          buf = pkgs.buf;
          deno = pkgs.deno;
          direnv = pkgs.direnv;
          git = pkgs.git;
          go = pkgs.go;
          golangci-lint = pkgs.golangci-lint;
          goreleaser = pkgs.goreleaser;
          krane = pkgs.krane;
          nix-direnv = pkgs.nix-direnv;
          nixpkgs-fmt = pkgs.nixpkgs-fmt;
          node = pkgs.nodejs;
          task = pkgs.go-task;
        };

        devShell = pkgs.mkShell {
          packages = builtins.attrValues flake.packages;
        };
      }
    )));
}
