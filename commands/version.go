package commands

import (
	"fmt"
	"os"
	"runtime"
	"runtime/debug"

	"imgsync/internal/version"
)

// Version prints build information for bug reports.
func Version(args []string) int {
	if len(args) != 0 {
		fmt.Fprintln(os.Stderr, "usage: imgsync version")
		return 2
	}
	fmt.Fprintln(os.Stdout, VersionString())
	return 0
}

// VersionString returns the one-line build report printed by the version
// command. Release builds get their version from -ldflags; local builds fall
// back to the Go module version when one is available.
func VersionString() string {
	v := version.Version
	commit := ""
	if info, ok := debug.ReadBuildInfo(); ok {
		if v == "dev" && info.Main.Version != "" && info.Main.Version != "(devel)" {
			v = info.Main.Version
		}
		for _, s := range info.Settings {
			switch s.Key {
			case "vcs.revision":
				commit = s.Value
			case "vcs.modified":
				if s.Value == "true" {
					commit += "-dirty"
				}
			}
		}
	}
	if commit != "" {
		return fmt.Sprintf("imgsync %s (%s) %s/%s", v, commit, runtime.GOOS, runtime.GOARCH)
	}
	return fmt.Sprintf("imgsync %s %s/%s", v, runtime.GOOS, runtime.GOARCH)
}
