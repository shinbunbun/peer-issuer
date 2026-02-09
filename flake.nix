{
  description = "WireGuard peer issuer API for RouterOS";

  inputs = {
    nixpkgs.url = "github:nixos/nixpkgs/nixos-unstable";
    flake-utils.url = "github:numtide/flake-utils";
  };

  outputs =
    {
      self,
      nixpkgs,
      flake-utils,
    }:
    flake-utils.lib.eachDefaultSystem (
      system:
      let
        pkgs = import nixpkgs { inherit system; };
      in
      {
        packages.default = pkgs.buildGoModule {
          pname = "peer-issuer";
          version = "0.1.0";
          src = ./.;
          vendorHash = "sha256-zXKQgnphUXePjx/Kw0K90BQGErPUAGHzWS6NtW7+R/c=";
          subPackages = [ "cmd/peer-issuer" ];
          env.CGO_ENABLED = 0;
          ldflags = [
            "-s"
            "-w"
          ];
        };
      }
    );
}
