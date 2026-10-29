//go:build !linux

package ownerfs

import "golang.org/x/sys/unix"

const statFlags = unix.O_RDONLY | unix.O_NONBLOCK | unix.O_NOCTTY

// open follows links and requires every file to belong to the user: without
// openat2 it cannot tell whether the path contained a link.
func (f *FS) open(name string, flags int) (int, error) {
	fd, err := unix.Open(name, flags|unix.O_CLOEXEC, 0)
	if err != nil {
		return -1, err
	}
	if err := f.check(fd, true); err != nil {
		unix.Close(fd)
		return -1, err
	}
	return fd, nil
}
