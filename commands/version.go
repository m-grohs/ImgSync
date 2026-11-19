package commands

import (
	"fmt"
	"os"
	"runtime"
	"runtime/debug"
)

// Version prints build information for bug reports.
func Version(args []string) int {
	if len(args) != 0 {
		fmt.Fprintln(os.Stderr, "usage: imgsync version")
		return 2
	}
	version := "dev"
	commit := ""
	if info, ok := debug.ReadBuildInfo(); ok {
		if info.Main.Version != "" && info.Main.Version != "(devel)" {
			version = info.Main.Version
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
		fmt.Printf("imgsync %s (%s) %s/%s\n", version, commit, runtime.GOOS, runtime.GOARCH)
	} else {
		fmt.Printf("imgsync %s %s/%s\n", version, runtime.GOOS, runtime.GOARCH)
	}
	return 0
}
