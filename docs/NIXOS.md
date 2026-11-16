# Installing ConduitOmniChat on NixOS / Nix

ConduitOmniChat provides a Nix Flake exposing both the background daemon (`conduitd`) and the desktop client (`conduit-gui`).

## Packages & Apps in the Flake

- **`default` / `conduit-gui`**: Desktop Electron client. It is wrapped to automatically execute against the compiled `conduitd` daemon binary. Includes a desktop entry.
- **`conduitd`**: The Go headless daemon binary. Useful if you run headless or manage the daemon separately.

## Quick Run (without permanent install)

Run directly from GitHub using `nix run`:

```sh
# Run the desktop GUI (launches conduitd in the background automatically)
nix run github:cestila/ConduitOmniChat

# Or run the daemon standalone
nix run github:cestila/ConduitOmniChat#conduitd
```

Or clone the repository and run locally:

```sh
nix run .#conduit-gui
```

## Adding to NixOS (`configuration.nix`)

In a flake-based NixOS setup:

1. Add the flake input to your `/etc/nixos/flake.nix`:

```nix
{
  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    conduit.url = "github:cestila/ConduitOmniChat";
  };

  outputs = { self, nixpkgs, conduit, ... }: {
    nixosConfigurations.yourhostname = nixpkgs.lib.nixosSystem {
      system = "x86_64-linux";
      modules = [
        ./configuration.nix
        {
          environment.systemPackages = [
            conduit.packages.x86_64-linux.default # or conduit-gui
            conduit.packages.x86_64-linux.conduitd
          ];
        }
      ];
    };
  };
}
```

2. Rebuild your system:

```sh
sudo nixos-rebuild switch --flake /etc/nixos#yourhostname
```

## Adding to Home Manager

```nix
{
  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    home-manager.url = "github:nix-community/home-manager";
    conduit.url = "github:cestila/ConduitOmniChat";
  };

  outputs = { self, nixpkgs, home-manager, conduit, ... }: {
    homeConfigurations."username" = home-manager.lib.homeManagerConfiguration {
      pkgs = nixpkgs.legacyPackages.x86_64-linux;
      modules = [
        {
          home.packages = [
            conduit.packages.x86_64-linux.conduit-gui
            conduit.packages.x86_64-linux.conduitd
          ];
        }
      ];
    };
  };
}
```

## Temporary Shell

To inspect or use the binaries in a temporary environment:

```sh
nix shell github:cestila/ConduitOmniChat#conduit-gui github:cestila/ConduitOmniChat#conduitd
```

## Managing Plugins in NixOS

Plugins for Conduit are dynamic executables (`conduit-plugin-*`).
Once `conduit-gui` or `conduitd` is installed:

- You can install official plugins right from the GUI's **Plugins** panel.
- Or use the terminal command:
  ```sh
  conduitd plugin install telegram
  conduitd plugin install whatsapp
  ```
Plugins are installed into `~/.local/share/conduit/plugins`, ensuring your user-level plugin installations remain mutable outside the read-only Nix store.
