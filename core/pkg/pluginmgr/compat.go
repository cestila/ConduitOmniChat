package pluginmgr

import (
	"fmt"
	"strconv"
	"strings"
)

type parsedSemver struct {
	major int
	minor int
	patch int
	pre   []string
}

func parseSemver(raw string) (parsedSemver, bool) {
	s := strings.TrimSpace(raw)
	s = strings.TrimPrefix(s, "v")
	s = strings.TrimPrefix(s, "V")
	if s == "" {
		return parsedSemver{}, false
	}

	// Ignore build metadata per SemVer 2.0.0.
	if idx := strings.IndexByte(s, '+'); idx >= 0 {
		s = s[:idx]
	}

	var pre []string
	if idx := strings.IndexByte(s, '-'); idx >= 0 {
		prePart := s[idx+1:]
		s = s[:idx]
		if prePart == "" {
			return parsedSemver{}, false
		}
		pre = strings.Split(prePart, ".")
		for _, p := range pre {
			if p == "" {
				return parsedSemver{}, false
			}
		}
	}

	parts := strings.Split(s, ".")
	if len(parts) < 1 || len(parts) > 3 {
		return parsedSemver{}, false
	}

	var nums [3]int
	for i, part := range parts {
		if part == "" {
			return parsedSemver{}, false
		}
		n, err := strconv.Atoi(part)
		if err != nil || n < 0 {
			return parsedSemver{}, false
		}
		nums[i] = n
	}

	return parsedSemver{
		major: nums[0],
		minor: nums[1],
		patch: nums[2],
		pre:   pre,
	}, true
}

func isNumericIdent(s string) (int, bool) {
	if s == "" {
		return 0, false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return 0, false
		}
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0, false
	}
	return n, true
}

func comparePrerelease(a, b []string) int {
	switch {
	case len(a) == 0 && len(b) == 0:
		return 0
	case len(a) == 0:
		return 1
	case len(b) == 0:
		return -1
	}

	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	for i := 0; i < n; i++ {
		if a[i] == b[i] {
			continue
		}
		numA, isNumA := isNumericIdent(a[i])
		numB, isNumB := isNumericIdent(b[i])
		switch {
		case isNumA && isNumB:
			if numA < numB {
				return -1
			}
			return 1
		case isNumA && !isNumB:
			return -1
		case !isNumA && isNumB:
			return 1
		default:
			return strings.Compare(a[i], b[i])
		}
	}
	if len(a) < len(b) {
		return -1
	}
	if len(a) > len(b) {
		return 1
	}
	return 0
}

// CompareSemver compares two semantic versions (with or without leading "v").
// Returns -1 if a < b, 0 if a == b, and 1 if a > b.
func CompareSemver(a, b string) int {
	pa, okA := parseSemver(a)
	pb, okB := parseSemver(b)
	if !okA || !okB {
		na := strings.TrimPrefix(strings.TrimPrefix(strings.TrimSpace(a), "v"), "V")
		nb := strings.TrimPrefix(strings.TrimPrefix(strings.TrimSpace(b), "v"), "V")
		return strings.Compare(na, nb)
	}

	if pa.major != pb.major {
		if pa.major < pb.major {
			return -1
		}
		return 1
	}
	if pa.minor != pb.minor {
		if pa.minor < pb.minor {
			return -1
		}
		return 1
	}
	if pa.patch != pb.patch {
		if pa.patch < pb.patch {
			return -1
		}
		return 1
	}
	return comparePrerelease(pa.pre, pb.pre)
}

// CheckCompatibility validates SDK protocol bounds, minimum core version, and
// required plugin dependencies for a registry plugin entry.
func CheckCompatibility(entry RegistryPlugin, coreVersion string, protoVersion uint32, installedVersions map[string]string) (compatible bool, reason string) {
	if entry.MinProtocolVersion > 0 && protoVersion < entry.MinProtocolVersion {
		return false, fmt.Sprintf("Requires SDK protocol >= v%d (current v%d)", entry.MinProtocolVersion, protoVersion)
	}
	if entry.MaxProtocolVersion > 0 && protoVersion > entry.MaxProtocolVersion {
		return false, fmt.Sprintf("Requires SDK protocol <= v%d (current v%d)", entry.MaxProtocolVersion, protoVersion)
	}

	coreVer := strings.TrimSpace(coreVersion)
	if coreVer != "" && coreVer != "dev" && entry.MinCoreVersion != "" {
		if _, ok := parseSemver(coreVer); ok && CompareSemver(coreVer, entry.MinCoreVersion) < 0 {
			return false, fmt.Sprintf("Requires Conduit Core >= v%s (current %s)", strings.TrimPrefix(entry.MinCoreVersion, "v"), coreVer)
		}
	}

	for _, dep := range entry.Dependencies {
		installed := strings.TrimSpace(installedVersions[dep.PluginID])
		if installed == "" {
			if dep.MinVersion != "" {
				return false, fmt.Sprintf("Missing required plugin %q (requires >= v%s)", dep.PluginID, strings.TrimPrefix(dep.MinVersion, "v"))
			}
			return false, fmt.Sprintf("Missing required plugin %q", dep.PluginID)
		}
		if dep.MinVersion != "" && installed != "dev" && CompareSemver(installed, dep.MinVersion) < 0 {
			return false, fmt.Sprintf("Requires plugin %q >= v%s (installed %s)", dep.PluginID, strings.TrimPrefix(dep.MinVersion, "v"), installed)
		}
	}

	return true, ""
}
