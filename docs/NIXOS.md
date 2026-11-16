# Installing ConduitOmniChat on NixOS / Nix

ConduitOmniChat provides a Nix Flake exposing both the official precompiled binary releases (default, fast, no compilation required) and development builds compiled directly from source.

## Available Packages & Targets

| Target | Description | Source |
| --- | --- | --- |
| **`default` / `conduit-gui`** | Official desktop release (`0.1.1`) | GitHub Release AppImage (FHS wrapped) |
| **`conduitd`** | Official headless daemon (`0.1.1`) | GitHub Release precompiled tarball |
| **`conduit-gui-source`** | Desktop Electron client | Compiled from git repository / branch |
| **`conduitd-source`** | Headless daemon | Compiled from Go source |

---

## 1. Quick Run (without permanent install)

### Using Official Release Binaries (Default)

```sh
# Run desktop GUI directly
nix run github:cestila/ConduitOmniChat

# Run headless daemon
nix run github:cestila/ConduitOmniChat#conduitd
```

### Pinning a Specific Release Tag

To run or pin a specific released tag:

```sh
nix run github:cestila/ConduitOmniChat/v0.1.1
```

### Compiling and Running from `main` (Source Target)

To test bleeding-edge changes from the `main` branch compiled on the fly:

```sh
# Run desktop GUI built from source
nix run github:cestila/ConduitOmniChat#conduit-gui-source

# Run daemon built from source
nix run github:cestila/ConduitOmniChat#conduitd-source
```

---

## 2. NixOS System Configuration (`configuration.nix`)

In your flake-based NixOS setup:

```nix
{
  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    
    # Track the release tag or main branch:
    conduit.url = "github:cestila/ConduitOmniChat";
    # Or pin to tag:
    # conduit.url = "github:cestila/ConduitOmniChat/v0.1.1";
  };

  outputs = { self, nixpkgs, conduit, ... }: {
    nixosConfigurations.yourhostname = nixpkgs.lib.nixosSystem {
      system = "x86_64-linux";
      modules = [
        ./configuration.nix
        {
          environment.systemPackages = [
            # Option A: Official Precompiled Binaries (Fast, default)
            conduit.packages.x86_64-linux.default # or conduit.packages.x86_64-linux.conduit-gui
            conduit.packages.x86_64-linux.conduitd

            # Option B: Built from Source (uncomment if you want to compile locally)
            # conduit.packages.x86_64-linux.conduit-gui-source
            # conduit.packages.x86_64-linux.conduitd-source
          ];
        }
      ];
    };
  };
}
```

Apply with:
```sh
sudo nixos-rebuild switch --flake /etc/nixos#yourhostname
```

---

## 3. Home Manager Configuration

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
            # Default precompiled release
            conduit.packages.x86_64-linux.conduit-gui
            conduit.packages.x86_64-linux.conduitd

            # Or from source:
            # conduit.packages.x86_64-linux.conduit-gui-source
          ];
        }
      ];
    };
  };
}
```

---

## 4. Managing Plugins in NixOS

Plugins (`conduit-plugin-*`) are dynamic executables downloaded on demand:

- Install official plugins from the GUI in the **Plugins** panel.
- Or use the terminal:
  ```sh
  conduitd plugin install telegram
  conduitd plugin install whatsapp
  ```

Plugins are installed to `~/.local/share/conduit/plugins`, ensuring plugin management remains mutable and isolated from the read-only Nix store.
