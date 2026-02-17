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
          buf = pkgs.buf;
          git = pkgs.git;
          go = pkgs.go_1_25;
          gofumpt = pkgs.gofumpt;
          golangci-lint = pkgs.golangci-lint;
          gotestsum = pkgs.gotestsum;
          graphviz = pkgs.graphviz;
          krane = pkgs.krane;
          kube-linter = pkgs.kube-linter;
          kubectl = pkgs.kubectl;
          nodejs = pkgs.nodejs_24;
          pnpm = pkgs.pnpm.override { nodejs = flake.packages.nodejs; };
          stern = pkgs.stern;
          yamllint = pkgs.yamllint;
          benchstat = pkgs.buildGoModule {
            pname = "benchstat";
            version = "0.0.0-unstable-2025-02-16";
            src = pkgs.fetchFromGitHub {
              owner = "golang";
              repo = "perf";
              rev = "8161c38c6cdca9a67a8635da2ae5c19990171269";
              hash = "sha256-zDoQjHBB5yKF1h+qOh4CKbB/lzilfaLT8fHp48FnFj8=";
            };
            vendorHash = "sha256-kGF184E+rOWncQsvjk1iCpF26/3Ll/IY9CPEh6vhRBQ=";
            subPackages = [ "cmd/benchstat" ];
          };

          # scripts
          build = pkgs.writeShellScriptBin "build" '' "$WORKSPACE_DIR"/scripts/build.ts "$@" '';
          clean = pkgs.writeShellScriptBin "clean" '' "$WORKSPACE_DIR"/scripts/clean.ts "$@" '';
          deploy = pkgs.writeShellScriptBin "deploy" '' "$WORKSPACE_DIR"/scripts/deploy.ts "$@" '';
          echo-load-test = pkgs.writeShellScriptBin "echo-load-test" '' "$WORKSPACE_DIR"/scripts/echo-load-test.ts "$@" '';
          echo-request = pkgs.writeShellScriptBin "echo-request" '' "$WORKSPACE_DIR"/scripts/echo-request.ts "$@" '';
          echo-websocket = pkgs.writeShellScriptBin "echo-websocket" '' "$WORKSPACE_DIR"/scripts/echo-websocket.ts "$@" '';
          fmt = pkgs.writeShellScriptBin "fmt" '' "$WORKSPACE_DIR"/scripts/fmt.ts "$@" '';
          generate = pkgs.writeShellScriptBin "generate" '' "$WORKSPACE_DIR"/scripts/generate.ts "$@" '';
          kube-lint = pkgs.writeShellScriptBin "kube-lint" '' "$WORKSPACE_DIR"/scripts/kube-lint.ts "$@" '';
          lint = pkgs.writeShellScriptBin "lint" '' "$WORKSPACE_DIR"/scripts/lint.ts "$@" '';
          logs = pkgs.writeShellScriptBin "logs" '' "$WORKSPACE_DIR"/scripts/logs.ts "$@" '';
          profile = pkgs.writeShellScriptBin "profile" '' "$WORKSPACE_DIR"/scripts/profile.ts "$@" '';
          script-tests = pkgs.writeShellScriptBin "script-tests" '' pnpm --filter scripts test "$@" '';
          web-tests = pkgs.writeShellScriptBin "web-tests" '' pnpm --filter web test "$@" '';
          tests = pkgs.writeShellScriptBin "tests" '' "$WORKSPACE_DIR"/scripts/tests.ts "$@" '';
        };

        devShell = pkgs.mkShell {
          packages = builtins.attrValues flake.packages;
        };
      }
    )));
}
