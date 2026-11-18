package main

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/cestila/ConduitOmniChat/core/pkg/buildinfo"
	"github.com/cestila/ConduitOmniChat/core/pkg/plugininstall"
	"github.com/cestila/ConduitOmniChat/core/pkg/pluginmgr"
	"github.com/cestila/ConduitOmniChat/sdk/pluginsdk"
)

// pluginInstaller is the part of plugininstall.Installer the CLI needs.
type pluginInstaller interface {
	Install(ctx context.Context, spec, version string) (plugininstall.Result, error)
	Remove(id string) (string, error)
	IsManagedPath(binaryPath string) bool
}

// cliDeps are the injectable dependencies of the command line.
type cliDeps struct {
	Installer  pluginInstaller
	PluginDirs []string // search directories in precedence order
	Registry   *pluginmgr.RegistryClient
	Manifest   *pluginmgr.ManifestStore
	TUIVersion string // optional override for testing; defaults to probeTUIVersion("")
}

const cliUsage = `Usage:
  conduitd                                   run the daemon
  conduitd plugin install <id|owner/repo> [--version vX.Y.Z]
  conduitd plugin list
  conduitd plugin remove <id>
  conduitd plugin check
  conduitd plugin update [<id>|--all]
  conduitd version
  conduitd help
`

func cliHelp(dirs []string) string {
	var b strings.Builder
	b.WriteString("conduitd - ConduitOmniChat daemon\n\n")
	b.WriteString(cliUsage)
	b.WriteString("\nCommands:\n")
	b.WriteString("  plugin install   download a plugin from its GitHub release, verify its SHA-256 and install it\n")
	b.WriteString("  plugin list      list the installed plugin binaries\n")
	b.WriteString("  plugin remove    delete an installed plugin binary\n")
	b.WriteString("  plugin check     check for plugin, Core, and TUI updates\n")
	b.WriteString("  plugin update    update one plugin or all managed plugins\n")
	b.WriteString("  version          print version and build information\n")
	b.WriteString("\nPlugin directories (highest precedence first):\n")
	for _, d := range dirs {
		fmt.Fprintf(&b, "  %s\n", d)
	}
	b.WriteString("\nEnvironment:\n")
	b.WriteString("  CONDUIT_PLUGIN_DIR   extra plugin directories searched first\n")
	b.WriteString("  CONDUIT_GITHUB_API   GitHub API base URL (default https://api.github.com)\n")
	return b.String()
}

// runCLI handles the command line. handled is false when args is empty and the
// daemon should run; otherwise exitCode is 0 (ok), 1 (operation error) or 2 (usage).
func runCLI(args []string, stdout, stderr io.Writer, deps cliDeps) (handled bool, exitCode int) {
	if len(args) == 0 {
		return false, 0
	}
	usageErr := func(format string, a ...any) (bool, int) {
		fmt.Fprintf(stderr, "conduitd: "+format+"\n\n%s", append(a, cliUsage)...)
		return true, 2
	}
	opErr := func(err error) (bool, int) {
		fmt.Fprintf(stderr, "conduitd: %v\n", err)
		return true, 1
	}

	switch args[0] {
	case "help", "-h", "--help":
		fmt.Fprint(stdout, cliHelp(deps.PluginDirs))
		return true, 0
	case "version", "--version", "-v":
		if len(args) > 1 {
			return usageErr("unexpected argument %q", args[1])
		}
		fmt.Fprintf(stdout, "conduitd %s\n", buildinfo.Summary())
		return true, 0
	case "plugin":
	default:
		return usageErr("unknown command %q", args[0])
	}

	if len(args) < 2 {
		return usageErr("missing plugin subcommand")
	}
	rest := args[2:]
	switch args[1] {
	case "install":
		spec, version := "", ""
		for i := 0; i < len(rest); i++ {
			a := rest[i]
			switch {
			case a == "--version":
				if i+1 >= len(rest) {
					return usageErr("flag --version needs a value")
				}
				i++
				version = rest[i]
			case strings.HasPrefix(a, "--version="):
				version = strings.TrimPrefix(a, "--version=")
			case strings.HasPrefix(a, "-"):
				return usageErr("unknown flag %q", a)
			case spec == "":
				spec = a
			default:
				return usageErr("unexpected argument %q", a)
			}
		}
		if spec == "" {
			return usageErr("plugin install needs a plugin id or owner/repo")
		}
		res, err := deps.Installer.Install(context.Background(), spec, version)
		if err != nil {
			return opErr(err)
		}
		fmt.Fprintf(stdout, "installed %s %s -> %s\n", res.ID, res.Version, res.Path)
		return true, 0

	case "list":
		if len(rest) > 0 {
			return usageErr("unexpected argument %q", rest[0])
		}
		bins := pluginmgr.FindBinaries(deps.PluginDirs)
		if len(bins) == 0 {
			fmt.Fprintln(stdout, "no plugins installed")
			return true, 0
		}
		for _, p := range bins {
			id := strings.TrimPrefix(filepath.Base(p), pluginmgr.BinaryPrefix)
			if n := len(id); n > 4 && strings.EqualFold(id[n-4:], ".exe") {
				id = id[:n-4]
			}
			fmt.Fprintf(stdout, "%s\t%s\n", id, p)
		}
		return true, 0

	case "remove":
		if len(rest) == 0 {
			return usageErr("plugin remove needs a plugin id")
		}
		if strings.HasPrefix(rest[0], "-") {
			return usageErr("unknown flag %q", rest[0])
		}
		if len(rest) > 1 {
			return usageErr("unexpected argument %q", rest[1])
		}
		path, err := deps.Installer.Remove(rest[0])
		if err != nil {
			return opErr(err)
		}
		fmt.Fprintf(stdout, "removed %s (%s)\n", rest[0], path)
		return true, 0

	case "check":
		if len(rest) > 0 {
			return usageErr("unexpected argument %q", rest[0])
		}
		if err := runPluginCheck(context.Background(), stdout, deps); err != nil {
			return opErr(err)
		}
		return true, 0

	case "update":
		targetID, all := "", false
		for _, a := range rest {
			switch {
			case a == "--all":
				all = true
			case strings.HasPrefix(a, "-"):
				return usageErr("unknown flag %q", a)
			case targetID == "":
				targetID = a
			default:
				return usageErr("unexpected argument %q", a)
			}
		}
		if all && targetID != "" {
			return usageErr("specify either a plugin id or --all, not both")
		}
		if !all && targetID == "" {
			return usageErr("plugin update needs a plugin id or --all")
		}
		if err := runPluginUpdate(context.Background(), stdout, deps, targetID, all); err != nil {
			return opErr(err)
		}
		return true, 0
	}
	return usageErr("unknown plugin subcommand %q", args[1])
}

func binaryPluginID(p string) string {
	id := strings.TrimPrefix(filepath.Base(p), pluginmgr.BinaryPrefix)
	if n := len(id); n > 4 && strings.EqualFold(id[n-4:], ".exe") {
		id = id[:n-4]
	}
	return id
}

func runPluginCheck(ctx context.Context, stdout io.Writer, deps cliDeps) error {
	reg := pluginmgr.CatalogRegistry()
	reachable := false
	if deps.Registry != nil {
		fetched, ok, _ := deps.Registry.Fetch(ctx, true)
		reachable = ok
		if fetched != nil {
			reg = fetched
		}
		if reachable && deps.Manifest != nil {
			_ = deps.Manifest.MergeRemote(reg)
		}
	}
	var local *pluginmgr.LocalManifest
	if deps.Manifest != nil {
		local, _ = deps.Manifest.Load()
	}
	if !reachable && deps.Registry != nil {
		fmt.Fprintln(stdout, "note: remote registry unreachable; showing cached/catalog state")
	}

	regByID := map[string]pluginmgr.RegistryPlugin{}
	for _, rp := range reg.Plugins {
		regByID[rp.ID] = rp
	}

	coreLatest := reg.Core.Version
	if coreLatest == "" && local != nil {
		coreLatest = local.Core.Version
	}
	coreLatestCol := coreLatest
	if coreLatestCol == "" {
		coreLatestCol = "-"
	}
	coreStatus := "ok"
	if coreLatest != "" && pluginmgr.CompareSemver(coreLatest, buildinfo.Version) > 0 {
		coreStatus = "update-available"
	}
	fmt.Fprintf(stdout, "core\t%s\t%s\t%s\n", buildinfo.Version, coreLatestCol, coreStatus)

	tuiVer := strings.TrimSpace(deps.TUIVersion)
	if tuiVer == "" {
		tuiVer = probeTUIVersion("")
	}
	if tuiVer != "" {
		fmt.Fprintf(stdout, "tui\t%s\t-\tok\n", tuiVer)
	}

	bins := pluginmgr.FindBinaries(deps.PluginDirs)
	if len(bins) == 0 {
		fmt.Fprintln(stdout, "no plugins installed")
		return nil
	}
	for _, p := range bins {
		id := binaryPluginID(p)
		installed := "unknown"
		latest := "-"
		status := "ok"
		if local != nil {
			if rec, ok := local.Plugins[id]; ok {
				if rec.InstalledVersion != "" {
					installed = rec.InstalledVersion
				}
				if rec.LatestVersion != "" {
					latest = rec.LatestVersion
				}
			}
		}
		if rp, ok := regByID[id]; ok && rp.LatestVersion != "" {
			latest = rp.LatestVersion
		}
		if installed != "unknown" && latest != "-" && pluginmgr.CompareSemver(latest, installed) > 0 {
			status = "update-available"
		}
		if deps.Installer != nil && !deps.Installer.IsManagedPath(p) {
			status = "managed-externally"
		}
		fmt.Fprintf(stdout, "%s\t%s\t%s\t%s\n", id, installed, latest, status)
	}
	return nil
}

func runPluginUpdate(ctx context.Context, stdout io.Writer, deps cliDeps, targetID string, all bool) error {
	if deps.Installer == nil {
		return fmt.Errorf("plugin installer is not available")
	}
	reg := pluginmgr.CatalogRegistry()
	if deps.Registry != nil {
		if fetched, _, _ := deps.Registry.Fetch(ctx, false); fetched != nil {
			reg = fetched
		}
	}
	var local *pluginmgr.LocalManifest
	if deps.Manifest != nil {
		local, _ = deps.Manifest.Load()
	}

	regByID := map[string]pluginmgr.RegistryPlugin{}
	for _, rp := range reg.Plugins {
		regByID[rp.ID] = rp
	}
	installedVersions := map[string]string{}
	if local != nil {
		for id, rec := range local.Plugins {
			if rec.InstalledVersion != "" {
				installedVersions[id] = rec.InstalledVersion
			}
		}
	}
	resolveEntry := func(id string) pluginmgr.RegistryPlugin {
		if rp, ok := regByID[id]; ok {
			if local != nil {
				if rec, ok := local.Plugins[id]; ok {
					if rp.LatestVersion == "" && rec.LatestVersion != "" {
						rp.LatestVersion = rec.LatestVersion
					}
					if rp.MinCoreVersion == "" && rec.MinCoreVersion != "" {
						rp.MinCoreVersion = rec.MinCoreVersion
					}
					if rp.MinProtocolVersion == 0 && rec.MinProtocolVersion > 0 {
						rp.MinProtocolVersion = rec.MinProtocolVersion
					}
					if rp.MaxProtocolVersion == 0 && rec.MaxProtocolVersion > 0 {
						rp.MaxProtocolVersion = rec.MaxProtocolVersion
					}
				}
			}
			return rp
		}
		if local != nil {
			if rec, ok := local.Plugins[id]; ok {
				return pluginmgr.RegistryPlugin{
					ID:                 id,
					Repository:         rec.Repository,
					LatestVersion:      rec.LatestVersion,
					MinCoreVersion:     rec.MinCoreVersion,
					MinProtocolVersion: rec.MinProtocolVersion,
					MaxProtocolVersion: rec.MaxProtocolVersion,
					Dependencies:       rec.Dependencies,
				}
			}
		}
		return pluginmgr.RegistryPlugin{ID: id}
	}

	bins := pluginmgr.FindBinaries(deps.PluginDirs)
	binByID := map[string]string{}
	for _, p := range bins {
		binByID[binaryPluginID(p)] = p
	}

	var targets []string
	if all {
		if len(bins) == 0 {
			fmt.Fprintln(stdout, "no plugins installed")
			return nil
		}
		for _, p := range bins {
			id := binaryPluginID(p)
			if !deps.Installer.IsManagedPath(p) {
				fmt.Fprintf(stdout, "skipped %s (managed externally at %s)\n", id, p)
				continue
			}
			installedVer := installedVersions[id]
			latestVer := resolveEntry(id).LatestVersion
			if installedVer != "" && latestVer != "" && pluginmgr.CompareSemver(latestVer, installedVer) <= 0 {
				fmt.Fprintf(stdout, "up-to-date %s (%s)\n", id, installedVer)
				continue
			}
			targets = append(targets, id)
		}
	} else {
		if p, ok := binByID[targetID]; ok && !deps.Installer.IsManagedPath(p) {
			return fmt.Errorf("plugin %q is managed externally (%s) and cannot be updated", targetID, p)
		}
		targets = []string{targetID}
	}

	for _, id := range targets {
		entry := resolveEntry(id)
		if compat, reason := pluginmgr.CheckCompatibility(entry, buildinfo.Version, pluginsdk.ProtocolVersion, installedVersions); !compat {
			if all {
				fmt.Fprintf(stdout, "skipped %s (incompatible: %s)\n", id, reason)
				continue
			}
			return fmt.Errorf("plugin %q is incompatible: %s", id, reason)
		}

		spec := id
		if _, ok := pluginmgr.CatalogEntryFor(id); !ok && local != nil {
			if rec, ok := local.Plugins[id]; ok && rec.Repository != "" {
				spec = rec.Repository
			}
		}
		res, err := deps.Installer.Install(ctx, spec, "")
		if err != nil {
			return err
		}
		if deps.Manifest != nil {
			if local == nil {
				local = &pluginmgr.LocalManifest{SchemaVersion: 1, Plugins: map[string]pluginmgr.LocalPluginRecord{}}
			}
			if local.Plugins == nil {
				local.Plugins = map[string]pluginmgr.LocalPluginRecord{}
			}
			rec := local.Plugins[res.ID]
			rec.ID = res.ID
			rec.InstalledVersion = res.Version
			if rec.Repository == "" {
				rec.Repository = entry.Repository
			}
			if rec.Source == "" {
				if strings.Contains(spec, "/") {
					rec.Source = "third-party"
				} else {
					rec.Source = "official"
				}
			}
			local.Plugins[res.ID] = rec
			_ = deps.Manifest.Save(local)
		}
		fmt.Fprintf(stdout, "updated %s %s -> %s\n", res.ID, res.Version, res.Path)
	}
	return nil
}
