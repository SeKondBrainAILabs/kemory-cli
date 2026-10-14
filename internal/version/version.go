// Package version holds the build version, set at link time:
//
//	go build -ldflags "-X github.com/SeKondBrainAILabs/kemory-cli/internal/version.Version=1.2.3"
package version

// Version is the CLI version. "dev" in an unreleased build.
var Version = "dev"
