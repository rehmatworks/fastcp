// Package ownerfs is a Caddy file system that does not let links publish
// files of other users. A file reached without following a link is served
// as usual; through a link, only if it belongs to the site's user. A site
// cannot serve another account's files, or the web server's own
// certificates and keys, by linking to them.
//
//	{
//		filesystem alice fastcp_owner 1001
//	}
//	example.com {
//		fs alice
//		...
//	}
package ownerfs

import (
	"errors"
	"io/fs"
	"math"
	"os"
	"strconv"

	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/caddyconfig/caddyfile"
	"golang.org/x/sys/unix"
)

func init() {
	caddy.RegisterModule(FS{})
}

// FS reads from the local file system like Caddy's default one. Names are
// absolute paths.
type FS struct {
	// UID must own whatever is reached through a link.
	UID int `json:"uid"`
}

func (FS) CaddyModule() caddy.ModuleInfo {
	return caddy.ModuleInfo{
		ID:  "caddy.fs.fastcp_owner",
		New: func() caddy.Module { return new(FS) },
	}
}

func (f *FS) Provision(caddy.Context) error {
	if f.UID <= 0 || f.UID >= math.MaxUint32 {
		return errors.New("fastcp_owner: uid must be a non-root user")
	}
	return nil
}

// UnmarshalCaddyfile reads `fastcp_owner <uid>`.
func (f *FS) UnmarshalCaddyfile(d *caddyfile.Dispenser) error {
	d.Next()
	var arg string
	if !d.Args(&arg) || d.NextArg() || d.NextBlock(0) {
		return d.ArgErr()
	}
	// uid_t is 32 bits and (uid_t)-1 means "no user".
	uid, err := strconv.ParseUint(arg, 10, 32)
	if err != nil || uid == 0 || uid == math.MaxUint32 {
		return d.Errf("invalid uid %q", arg)
	}
	f.UID = int(uid)
	return nil
}

func (f *FS) Open(name string) (fs.File, error) {
	// Non-blocking, so a FIFO cannot hold the request before it is refused.
	fd, err := f.open(name, unix.O_RDONLY|unix.O_NONBLOCK|unix.O_NOCTTY)
	if err == nil {
		if err = unix.SetNonblock(fd, false); err != nil {
			unix.Close(fd)
		}
	}
	if err != nil {
		return nil, &fs.PathError{Op: "open", Path: name, Err: err}
	}
	return os.NewFile(uintptr(fd), name), nil
}

func (f *FS) Stat(name string) (fs.FileInfo, error) {
	fd, err := f.open(name, statFlags)
	if err != nil {
		return nil, &fs.PathError{Op: "stat", Path: name, Err: err}
	}
	file := os.NewFile(uintptr(fd), name)
	defer file.Close()
	return file.Stat()
}

// check decides on an opened file. Refused files are reported as missing,
// so responses do not reveal that they exist.
func (f *FS) check(fd int, linked bool) error {
	var st unix.Stat_t
	if err := unix.Fstat(fd, &st); err != nil {
		return err
	}
	switch st.Mode & unix.S_IFMT {
	case unix.S_IFREG, unix.S_IFDIR:
	default:
		return fs.ErrNotExist
	}
	if linked && int(st.Uid) != f.UID {
		return fs.ErrNotExist
	}
	return nil
}

var (
	_ fs.StatFS             = (*FS)(nil)
	_ caddy.Provisioner     = (*FS)(nil)
	_ caddyfile.Unmarshaler = (*FS)(nil)
)
