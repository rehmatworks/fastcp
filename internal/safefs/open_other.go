//go:build darwin

package safefs

func openBeneath(dirfd int, parts []string, flags int) (int, error) {
	return openWalk(dirfd, parts, flags)
}
