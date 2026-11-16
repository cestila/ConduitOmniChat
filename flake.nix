{
  description = "ConduitOmniChat - Local-first multi-network chat client";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    flake-utils.url = "github:numtide/flake-utils";
  };

  outputs = { self, nixpkgs, flake-utils }:
    flake-utils.lib.eachDefaultSystem (system:
      let
        pkgs = import nixpkgs { inherit system; };

        conduitd = pkgs.buildGoModule {
          pname = "conduitd";
          version = "0.1.1-unreleased";

          src = pkgs.lib.cleanSourceWith {
            src = ./.;
            filter = path: type:
              let
                rel = pkgs.lib.removePrefix (toString ./.) (toString path);
              in
                type != "socket" && (
                  pkgs.lib.hasPrefix "/core" rel ||
                  pkgs.lib.hasPrefix "/sdk" rel ||
                  rel == ""
                );
          };

          modRoot = "./core";
          vendorHash = "sha256-/jpIrA0s2God7+lG63vsnk4WOkm0w0rx88Tp3i4M6kY=";

          subPackages = [ "cmd/conduitd" ];

          meta = with pkgs.lib; {
            description = "Background daemon for ConduitOmniChat";
            homepage = "https://github.com/cestila/ConduitOmniChat";
            license = licenses.asl20;
            mainProgram = "conduitd";
            platforms = platforms.unix;
          };
        };

        conduit-gui = pkgs.buildNpmPackage {
          pname = "conduit-gui";
          version = "1.0.0";

          src = pkgs.lib.cleanSourceWith {
            src = ./gui;
            filter = path: type:
              let
                base = baseNameOf path;
              in
                type != "socket" &&
                base != "node_modules" &&
                base != "dist" &&
                base != ".git";
          };

          npmDepsHash = "sha256-TnbeThxfjtyvzu0yMzS5WCmwfB3SrBPezT9Mj2ywARM=";
          dontNpmBuild = true;

          env.ELECTRON_SKIP_BINARY_DOWNLOAD = "1";

          nativeBuildInputs = [ pkgs.makeWrapper ];

          installPhase = ''
            runHook preInstall

            mkdir -p $out/share/conduit-gui
            cp -r . $out/share/conduit-gui/

            mkdir -p $out/bin
            makeWrapper ${pkgs.lib.getExe pkgs.electron} $out/bin/conduit-gui \
              --add-flags "$out/share/conduit-gui/main.js" \
              --set-default CONDUITD_PATH "${conduitd}/bin/conduitd"

            mkdir -p $out/share/applications
            cat << 'DESKTOP' > $out/share/applications/conduit-gui.desktop
[Desktop Entry]
Name=Conduit OmniChat
Comment=Local-first multi-network chat client
Exec=conduit-gui %U
Terminal=false
Type=Application
Categories=Network;Chat;InstantMessaging;
StartupWMClass=conduit-gui
DESKTOP

            runHook postInstall
          '';

          meta = with pkgs.lib; {
            description = "GUI desktop client for ConduitOmniChat";
            homepage = "https://github.com/cestila/ConduitOmniChat";
            license = licenses.asl20;
            mainProgram = "conduit-gui";
            platforms = platforms.linux;
          };
        };
      in
      {
        packages = {
          default = conduit-gui;
          inherit conduitd conduit-gui;
        };

        apps = {
          default = flake-utils.lib.mkApp { drv = conduit-gui; };
          conduitd = flake-utils.lib.mkApp { drv = conduitd; };
          conduit-gui = flake-utils.lib.mkApp { drv = conduit-gui; };
        };
      }
    );
}
