// Package buildinfo holds build-time version metadata injected via -ldflags.
package buildinfo

import "fmt"

// Build metadata injected at link time via -ldflags -X.
var (
	Version = "dev"
	Commit  = "none"
	Date    = "unknown"
)

// Summary returns a human-readable version string combining Version, Commit, and Date.
func Summary() string {
	v := Version
	if v == "" {
		v = "dev"
	}
	hasCommit := Commit != "" && Commit != "none"
	hasDate := Date != "" && Date != "unknown"
	switch {
	case hasCommit && hasDate:
		return fmt.Sprintf("%s (commit %s, built %s)", v, Commit, Date)
	case hasCommit:
		return fmt.Sprintf("%s (commit %s)", v, Commit)
	case hasDate:
		return fmt.Sprintf("%s (built %s)", v, Date)
	default:
		return v
	}
}
