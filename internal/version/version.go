// Package version holds the build-time version of the CLI.
package version

// Version is set to "dev" for local builds and overwritten for releases with
// -ldflags "-X imgsync/internal/version.Version=vX.Y.Z" (see the release
// workflow). Keeping it in its own package makes it overridable without
// changing any source files when a new release tag is pushed.
var Version = "dev"
