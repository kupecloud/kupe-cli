// Package build holds build-time metadata injected via -ldflags at link time.
// The values here are the dev defaults used when building without goreleaser
// or the Makefile.
package build

// These variables are overwritten by ldflags during a release build.
// See .goreleaser.yaml and Makefile for the injection points.
var (
	Version = "dev"
	Commit  = "none"
	Date    = "unknown"
)
