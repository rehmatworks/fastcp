package ownerfs

import (
	"errors"

	"golang.org/x/sys/unix"
)

const statFlags = unix.O_PATH

// open opens name without following links if it can. When the path
// contains a link, or openat2 is unavailable, it follows links and requires
// the file to belong to the user.
func (f *FS) open(name string, flags int) (int, error) {
	fd, err := unix.Openat2(unix.AT_FDCWD, name, &unix.OpenHow{
		Flags:   uint64(flags | unix.O_CLOEXEC),
		Resolve: unix.RESOLVE_NO_SYMLINKS,
	})
	linked := false
	if errors.Is(err, unix.ELOOP) || errors.Is(err, unix.ENOSYS) || errors.Is(err, unix.EPERM) {
		linked = true
		fd, err = unix.Open(name, flags|unix.O_CLOEXEC, 0)
	}
	if err != nil {
		return -1, err
	}
	if err := f.check(fd, linked); err != nil {
		unix.Close(fd)
		return -1, err
	}
	return fd, nil
}
