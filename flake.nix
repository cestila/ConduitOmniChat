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

        version = "0.2.0";

        # -------------------------------------------------------------
        # 1. Official Precompiled Binary Release (Default)
        # -------------------------------------------------------------
        conduit-gui-bin = pkgs.appimageTools.wrapType2 {
          pname = "conduit-gui";
          inherit version;

          src = pkgs.fetchurl {
            url = "https://github.com/cestila/ConduitOmniChat/releases/download/v${version}/Conduit-${version}.AppImage";
            hash = "sha256-2LJ0mvuLPqN7XCRYC3DTmIu5nwC+0ab7j5NcuoCwh8I=";
          };

          extraPkgs = p: [ ];

          extraInstallCommands = ''
            mkdir -p $out/share/applications
            cat << 'DESKTOP' > $out/share/applications/conduit-gui.desktop
[Desktop Entry]
Name=Conduit OmniChat
Comment=ConduitOmniChat Portable Multi-Platform GUI Client
Exec=conduit-gui %U
Terminal=false
Type=Application
Icon=conduit-gui
Categories=Network;Chat;InstantMessaging;
StartupWMClass=Conduit
DESKTOP

            mkdir -p $out/share/icons/hicolor/256x256/apps
            cp ${pkgs.appimageTools.extract {
              pname = "conduit-gui";
              inherit version;
              src = pkgs.fetchurl {
                url = "https://github.com/cestila/ConduitOmniChat/releases/download/v${version}/Conduit-${version}.AppImage";
                hash = "sha256-2LJ0mvuLPqN7XCRYC3DTmIu5nwC+0ab7j5NcuoCwh8I=";
              };
            }}/usr/share/icons/hicolor/256x256/apps/conduit-gui.png $out/share/icons/hicolor/256x256/apps/
          '';

          meta = with pkgs.lib; {
            description = "GUI desktop client for ConduitOmniChat (Official precompiled binary)";
            homepage = "https://github.com/cestila/ConduitOmniChat";
            license = licenses.asl20;
            mainProgram = "conduit-gui";
            platforms = [ "x86_64-linux" ];
          };
        };

        conduitd-bin = pkgs.stdenv.mkDerivation {
          pname = "conduitd";
          inherit version;

          src = pkgs.fetchurl {
            url = "https://github.com/cestila/ConduitOmniChat/releases/download/v${version}/conduitd_${version}_linux_amd64.tar.gz";
            hash = "sha256-8BzNqQUgxw27/ZADGkiVIwODkZMlb7tKl5eeCO7SciA=";
          };

          sourceRoot = ".";

          installPhase = ''
            mkdir -p $out/bin
            cp conduitd $out/bin/conduitd
            chmod +x $out/bin/conduitd
          '';

          meta = with pkgs.lib; {
            description = "Background daemon for ConduitOmniChat (Official precompiled binary)";
            homepage = "https://github.com/cestila/ConduitOmniChat";
            license = licenses.asl20;
            mainProgram = "conduitd";
            platforms = [ "x86_64-linux" ];
          };
        };

        # -------------------------------------------------------------
        # 2. Source-Built Targets (Development / Main branch)
        # -------------------------------------------------------------
        conduitd-source = pkgs.buildGoModule {
          pname = "conduitd-source";
          version = "${version}-dev";

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

          ldflags = [
            "-s"
            "-w"
            "-X github.com/cestila/ConduitOmniChat/core/pkg/buildinfo.Version=v${version}-dev"
            "-X github.com/cestila/ConduitOmniChat/core/pkg/buildinfo.Commit=${self.shortRev or self.dirtyShortRev or "nix"}"
            "-X github.com/cestila/ConduitOmniChat/core/pkg/buildinfo.Date=${self.lastModifiedDate or "unknown"}"
          ];

          meta = with pkgs.lib; {
            description = "Background daemon for ConduitOmniChat (Built from source)";
            homepage = "https://github.com/cestila/ConduitOmniChat";
            license = licenses.asl20;
            mainProgram = "conduitd";
            platforms = platforms.unix;
          };
        };

        conduit-gui-source = pkgs.buildNpmPackage {
          pname = "conduit-gui-source";
          version = "${version}-dev";

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

          npmDepsHash = "sha256-BSlQJJ2drJExrv3eyZr+X0SPzlqZ0tMUpI15wg0Q6kQ=";
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
              --set-default CONDUITD_PATH "${conduitd-source}/bin/conduitd"

            mkdir -p $out/share/applications
            cat << 'DESKTOP' > $out/share/applications/conduit-gui.desktop
[Desktop Entry]
Name=Conduit OmniChat (Dev)
Comment=Local-first multi-network chat client (Built from source)
Exec=conduit-gui %U
Terminal=false
Type=Application
Categories=Network;Chat;InstantMessaging;
StartupWMClass=conduit-gui
DESKTOP

            runHook postInstall
          '';

          meta = with pkgs.lib; {
            description = "GUI desktop client for ConduitOmniChat (Built from source)";
            homepage = "https://github.com/cestila/ConduitOmniChat";
            license = licenses.asl20;
            mainProgram = "conduit-gui";
            platforms = platforms.linux;
          };
        };
      in
      {
        packages = {
          # Default: official precompiled binaries
          default = conduit-gui-bin;
          conduit-gui = conduit-gui-bin;
          conduitd = conduitd-bin;

          # Optional: built from source (main branch / local modifications)
          conduit-gui-source = conduit-gui-source;
          conduitd-source = conduitd-source;
        };

        apps = {
          default = flake-utils.lib.mkApp { drv = conduit-gui-bin; };
          conduit-gui = flake-utils.lib.mkApp { drv = conduit-gui-bin; };
          conduitd = flake-utils.lib.mkApp { drv = conduitd-bin; };

          conduit-gui-source = flake-utils.lib.mkApp { drv = conduit-gui-source; };
          conduitd-source = flake-utils.lib.mkApp { drv = conduitd-source; };
        };
      }
    );
}
