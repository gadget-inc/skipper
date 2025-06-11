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
          # dependencies
          git = pkgs.git;
          go = pkgs.go_1_24;
          gofumpt = pkgs.gofumpt;
          golangci-lint = pkgs.golangci-lint;
          graphviz = pkgs.graphviz;
          krane = pkgs.krane;
          kubectl = pkgs.kubectl;
          nodejs = pkgs.nodejs_22;
          pnpm = pkgs.pnpm.override { nodejs = flake.packages.nodejs; };
          stern = pkgs.stern;

          # scripts
          build = pkgs.writeShellScriptBin "build" '' "$WORKSPACE_DIR"/scripts/build.ts "$@" '';
          clean = pkgs.writeShellScriptBin "clean" '' "$WORKSPACE_DIR"/scripts/clean.ts "$@" '';
          deploy = pkgs.writeShellScriptBin "deploy" '' "$WORKSPACE_DIR"/scripts/deploy.ts "$@" '';
          echo-load-test = pkgs.writeShellScriptBin "echo-load-test" '' "$WORKSPACE_DIR"/scripts/echo-load-test.ts "$@" '';
          echo-request = pkgs.writeShellScriptBin "echo-request" '' "$WORKSPACE_DIR"/scripts/echo-request.ts "$@" '';
          echo-websocket = pkgs.writeShellScriptBin "echo-websocket" '' "$WORKSPACE_DIR"/scripts/echo-websocket.ts "$@" '';
          fmt = pkgs.writeShellScriptBin "fmt" '' "$WORKSPACE_DIR"/scripts/fmt.ts "$@" '';
          lint = pkgs.writeShellScriptBin "lint" '' "$WORKSPACE_DIR"/scripts/lint.ts "$@" '';
          logs = pkgs.writeShellScriptBin "logs" '' "$WORKSPACE_DIR"/scripts/logs.ts "$@" '';
          tests = pkgs.writeShellScriptBin "tests" '' "$WORKSPACE_DIR"/scripts/tests.ts "$@" '';
        };

        devShell = pkgs.mkShell {
          packages = builtins.attrValues flake.packages;
        };
      }
    )));
}
