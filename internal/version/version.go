// Package version reports what build this is.
package version

import "runtime/debug"

// Set by goreleaser via -ldflags. Left empty for a `go install` build, where
// the module version is recovered from the build info instead -- so
// `go install ...@v0.3.0` reports v0.3.0 without anyone passing ldflags.
var (
	Version = ""
	Commit  = ""
	Date    = ""
)

// Version strings for a build with no information at all. Reported honestly
// rather than guessed at: a wrong version in a report is worse than "devel".
const unknown = "devel"

// String returns the release version.
func String() string {
	if Version != "" {
		return Version
	}
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}
	return unknown
}

// Full returns a human-readable build description.
func Full() string {
	out := String()
	if Commit != "" {
		out += " (commit: " + Commit
		if Date != "" {
			out += ", built: " + Date
		}
		out += ")"
	}
	return out
}
