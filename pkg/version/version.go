// Package version holds build metadata injected through -ldflags.
package version

// Version and Commit are set at build time with
// -ldflags "-X github.com/0xHoaxen/shogun/pkg/version.Version=..." and are the
// only package-level variables allowed outside main.
var (
	Version = "dev"
	Commit  = "none"
)

// String returns "<version> (<commit>)".
func String() string {
	return Version + " (" + Commit + ")"
}
