// Package version exposes build metadata for every binary in go-cli-tools.
//
// Release builds inject the values with -ldflags, for example:
//
//	go build -ldflags "-X github.com/tonklasocute/go-cli-tools/internal/version.Version=v0.1.0"
//
// When the variables are left at their defaults (e.g. `go install ...@latest`),
// the information is recovered from the module build info instead.
package version

import (
	"fmt"
	"runtime"
	"runtime/debug"
)

// Build metadata. These are variables (not constants) so -ldflags -X can set them.
var (
	// Version is the semantic version of the build, e.g. "v0.1.0".
	Version = "dev"
	// Commit is the git commit the binary was built from.
	Commit = "none"
	// Date is the build date in RFC 3339 format.
	Date = "unknown"
)

// readBuildInfo is swapped out in tests.
var readBuildInfo = debug.ReadBuildInfo

// Info is the resolved build metadata.
type Info struct {
	Version   string `json:"version"`
	Commit    string `json:"commit"`
	Date      string `json:"date"`
	GoVersion string `json:"go_version"`
	Platform  string `json:"platform"`
}

// Get returns the build metadata, falling back to the embedded module build
// info for fields that were not injected at link time.
func Get() Info {
	info := Info{
		Version:   Version,
		Commit:    Commit,
		Date:      Date,
		GoVersion: runtime.Version(),
		Platform:  runtime.GOOS + "/" + runtime.GOARCH,
	}
	bi, ok := readBuildInfo()
	if !ok {
		return info
	}
	if info.Version == "dev" && bi.Main.Version != "" && bi.Main.Version != "(devel)" {
		info.Version = bi.Main.Version
	}
	for _, s := range bi.Settings {
		switch s.Key {
		case "vcs.revision":
			if info.Commit == "none" {
				info.Commit = shortCommit(s.Value)
			}
		case "vcs.time":
			if info.Date == "unknown" {
				info.Date = s.Value
			}
		}
	}
	return info
}

// String renders a one-line version banner for the named tool.
func String(tool string) string {
	i := Get()
	return fmt.Sprintf("%s %s (commit %s, built %s, %s %s)\n",
		tool, i.Version, i.Commit, i.Date, i.GoVersion, i.Platform)
}

func shortCommit(c string) string {
	if len(c) > 12 {
		return c[:12]
	}
	return c
}
