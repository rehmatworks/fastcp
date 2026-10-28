//go:build linux

package safefs

import (
	"errors"
	"strings"
	"sync/atomic"

	"golang.org/x/sys/unix"
)

var openat2Unavailable atomic.Bool

// openBeneath resolves parts beneath dirfd with openat2 when the kernel
// supports it, so the whole lookup is atomic, and falls back to a
// component-by-component O_NOFOLLOW walk otherwise.
func openBeneath(dirfd int, parts []string, flags int) (int, error) {
	if !openat2Unavailable.Load() {
		how := &unix.OpenHow{
			Flags:   uint64(flags | unix.O_NOFOLLOW | unix.O_CLOEXEC),
			Resolve: unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_MAGICLINKS,
		}
		fd, err := unix.Openat2(dirfd, strings.Join(parts, "/"), how)
		switch {
		case err == nil:
			return fd, nil
		case errors.Is(err, unix.ENOSYS), errors.Is(err, unix.EPERM):
			openat2Unavailable.Store(true)
		case errors.Is(err, unix.ENOTDIR):
			// Distinguish "is a symlink" from "is a file".
			return openWalk(dirfd, parts, flags)
		default:
			return -1, err
		}
	}
	return openWalk(dirfd, parts, flags)
}
