// Package version holds build metadata shared by the panel and the agent.
// Values are injected at link time; see the Makefile.
package version

var (
	Version   = "dev"
	BuildTime = "unknown"
)

// Tag returns the release tag for this build, for example "v3.0.0".
func Tag() string {
	if Version == "" || Version == "dev" {
		return "dev"
	}
	if Version[0] == 'v' {
		return Version
	}
	return "v" + Version
}
