{
  description = "skipper development environment";

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
          graphviz = pkgs.graphviz;
          krane = pkgs.krane;
          kubectl = pkgs.kubectl;
          stern = pkgs.stern;
          task = pkgs.go-task;

          build = pkgs.writeShellScriptBin "build" ''
            task build "$@"
          '';

          build-fixtures = pkgs.writeShellScriptBin "build-fixtures" ''
            task build-fixtures "$@"
          '';

          clean = pkgs.writeShellScriptBin "clean" ''
            task clean "$@"
          '';

          deploy = pkgs.writeShellScriptBin "deploy" ''
            task deploy "$@"
          '';

          deploy-fixtures = pkgs.writeShellScriptBin "deploy-fixtures" ''
            task deploy-fixtures "$@"
          '';

          deploy-all = pkgs.writeShellScriptBin "deploy-all" ''
            task deploy-all "$@"
          '';

          echo-request = pkgs.writeShellScriptBin "echo-request" ''
            task echo-request "$@"
          '';

          echo-websocket = pkgs.writeShellScriptBin "echo-websocket" ''
            task echo-websocket "$@"
          '';

          echo-load-test = pkgs.writeShellScriptBin "echo-load-test" ''
            task echo-load-test "$@"
          '';

          skipper = pkgs.writeShellScriptBin "skipper" ''
            go run main.go "$@"
          '';

          tst = pkgs.writeShellScriptBin "tst" ''
            task test "$@"
          '';
        };

        devShell = pkgs.mkShell {
          packages = builtins.attrValues flake.packages;
        };
      }
    )));
}
