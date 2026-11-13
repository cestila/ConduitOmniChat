package pluginmgr

import (
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

// BinaryPrefix is the file name prefix every plugin executable carries.
const BinaryPrefix = "conduit-plugin-"

// SearchDirs returns the plugin directories in precedence order:
// $CONDUIT_PLUGIN_DIR entries, <dataDir>/plugins, <execDir>/plugins,
// <execDir> and <cwd>/bin/plugins. Empty and repeated entries are dropped.
func SearchDirs(envDirs, dataDir, execDir, cwd string) []string {
	var candidates []string
	for _, dir := range filepath.SplitList(envDirs) {
		candidates = append(candidates, dir)
	}
	if dataDir != "" {
		candidates = append(candidates, filepath.Join(dataDir, "plugins"))
	}
	if execDir != "" {
		candidates = append(candidates, filepath.Join(execDir, "plugins"), execDir)
	}
	if cwd != "" {
		candidates = append(candidates, filepath.Join(cwd, "bin", "plugins"))
	}

	seen := map[string]bool{}
	var dirs []string
	for _, dir := range candidates {
		if strings.TrimSpace(dir) == "" {
			continue
		}
		dir = filepath.Clean(dir)
		if seen[dir] {
			continue
		}
		seen[dir] = true
		dirs = append(dirs, dir)
	}
	return dirs
}

// pluginName returns the executable name without the Windows .exe suffix.
func pluginName(fileName string) string {
	if runtime.GOOS == "windows" {
		return strings.TrimSuffix(strings.ToLower(fileName), ".exe")
	}
	return fileName
}

func isExecutable(info os.FileInfo) bool {
	if !info.Mode().IsRegular() {
		return false
	}
	if runtime.GOOS == "windows" {
		return strings.EqualFold(filepath.Ext(info.Name()), ".exe")
	}
	return info.Mode().Perm()&0o111 != 0
}

// FindBinaries lists the plugin executables found in dirs. When the same
// executable name appears in several directories the first one wins.
func FindBinaries(dirs []string) []string {
	seen := map[string]bool{}
	var found []string
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
		for _, entry := range entries {
			name := pluginName(entry.Name())
			if !strings.HasPrefix(name, BinaryPrefix) || seen[name] {
				continue
			}
			info, err := entry.Info()
			if err != nil || !isExecutable(info) {
				continue
			}
			seen[name] = true
			found = append(found, filepath.Join(dir, entry.Name()))
		}
	}
	return found
}
