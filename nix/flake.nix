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
          go = pkgs.go_1_26;
          gofumpt = pkgs.gofumpt;
          golangci-lint = pkgs.golangci-lint;
          gotestsum = pkgs.gotestsum;
          graphviz = pkgs.graphviz;
          # Override krane to add the csv gem, which Ruby 3.4 removed from
          # default gems. Krane's bindings_parser.rb requires csv but the
          # upstream gemset doesn't declare it. We must patch the Gemfile,
          # Gemfile.lock, and gemset so Bundler can resolve csv at runtime.
          # TODO: remove when upstream krane gemset includes csv
          krane = pkgs.krane.override {
            bundlerApp = args: pkgs.bundlerApp (args // {
              gemdir = null; # forces bundled-common to use explicit gemfile/lockfile/gemset
              gemfile = pkgs.writeText "Gemfile" ''
                source 'https://rubygems.org'
                gem 'krane'
                gem 'csv'
              '';
              lockfile = pkgs.writeText "Gemfile.lock" (
                builtins.replaceStrings
                  [ "  specs:\n" "\nDEPENDENCIES\n  krane\n" ]
                  [ "  specs:\n    csv (3.3.5)\n" "\nDEPENDENCIES\n  csv\n  krane\n" ]
                  (builtins.readFile "${args.gemdir}/Gemfile.lock")
              );
              gemset = (import "${args.gemdir}/gemset.nix") // {
                csv = {
                  groups = [ "default" ];
                  platforms = [ ];
                  source = {
                    remotes = [ "https://rubygems.org" ];
                    # nix-prefetch-url https://rubygems.org/gems/csv-3.3.5.gem
                    sha256 = "0gz7r2kazwwwyrwi95hbnhy54kwkfac5swh2gy5p5vw36fn38lbf";
                    type = "gem";
                  };
                  version = "3.3.5";
                };
              };
            });
          };
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
          tests = pkgs.writeShellScriptBin "tests" '' "$WORKSPACE_DIR"/scripts/tests.ts "$@" '';
        };

        devShell = pkgs.mkShell {
          packages = builtins.attrValues flake.packages;
        };
      }
    )));
}
