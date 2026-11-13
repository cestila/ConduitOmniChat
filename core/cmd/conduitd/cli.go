package main

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/cestila/ConduitOmniChat/core/pkg/plugininstall"
	"github.com/cestila/ConduitOmniChat/core/pkg/pluginmgr"
)

// pluginInstaller is the part of plugininstall.Installer the CLI needs.
type pluginInstaller interface {
	Install(ctx context.Context, spec, version string) (plugininstall.Result, error)
	Remove(id string) (string, error)
}

// cliDeps are the injectable dependencies of the command line.
type cliDeps struct {
	Installer  pluginInstaller
	PluginDirs []string // search directories in precedence order
}

const cliUsage = `Usage:
  conduitd                                   run the daemon
  conduitd plugin install <id|owner/repo> [--version vX.Y.Z]
  conduitd plugin list
  conduitd plugin remove <id>
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
	}
	return usageErr("unknown plugin subcommand %q", args[1])
}
