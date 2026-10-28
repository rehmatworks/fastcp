//go:build linux || darwin

// Package safefs performs privileged filesystem operations inside directory
// trees that an unprivileged user can modify, such as tenant home
// directories. Paths are resolved relative to an open directory handle,
// symbolic links are never followed, ".." is rejected and recursion does not
// cross filesystem boundaries, so a tenant cannot redirect a root-owned
// write, chown or delete to a file outside their own tree.
package safefs

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"strings"

	"golang.org/x/sys/unix"
)

var (
	ErrSymlink  = errors.New("refusing to follow a symbolic link")
	ErrNotDir   = errors.New("not a directory")
	ErrNotFile  = errors.New("not a regular file")
	ErrBadPath  = errors.New("invalid path component")
	ErrTooLarge = errors.New("file is too large")
)

// Info is the subset of stat(2) data callers need for ownership checks.
type Info struct {
	Mode  uint32
	UID   uint32
	GID   uint32
	Nlink uint64
	Size  int64
	Dev   uint64
	Ino   uint64
}

func infoFrom(st *unix.Stat_t) Info {
	return Info{
		Mode:  uint32(st.Mode),
		UID:   st.Uid,
		GID:   st.Gid,
		Nlink: uint64(st.Nlink),
		Size:  st.Size,
		Dev:   uint64(st.Dev),
		Ino:   uint64(st.Ino),
	}
}

func (i Info) IsDir() bool     { return i.Mode&unix.S_IFMT == unix.S_IFDIR }
func (i Info) IsRegular() bool { return i.Mode&unix.S_IFMT == unix.S_IFREG }
func (i Info) IsSymlink() bool { return i.Mode&unix.S_IFMT == unix.S_IFLNK }
func (i Info) Perm() uint32    { return i.Mode & 0o7777 }

// Dir is an open directory. All methods resolve names relative to it.
type Dir struct {
	fd   int
	path string
}

// OpenRoot opens a directory whose path is controlled by root, for example
// /home/alice (alice cannot replace it because /home is root-owned).
// Everything beneath the returned Dir is resolved without following links.
func OpenRoot(p string) (*Dir, error) {
	if !path.IsAbs(p) || path.Clean(p) != p {
		return nil, fmt.Errorf("%w: %q must be an absolute clean path", ErrBadPath, p)
	}
	fd, err := unix.Open(p, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, &os.PathError{Op: "open", Path: p, Err: err}
	}
	return &Dir{fd: fd, path: p}, nil
}

func (d *Dir) Close() error {
	if d == nil || d.fd < 0 {
		return nil
	}
	err := unix.Close(d.fd)
	d.fd = -1
	return err
}

// Path is the path the directory was reached by. It is informational only
// and must not be used to reopen the directory with privileges.
func (d *Dir) Path() string { return d.path }

// Fd exposes the descriptor for passing to child processes.
func (d *Dir) Fd() int { return d.fd }

func (d *Dir) Stat() (Info, error) {
	var st unix.Stat_t
	if err := unix.Fstat(d.fd, &st); err != nil {
		return Info{}, d.err("fstat", "", err)
	}
	return infoFrom(&st), nil
}

func (d *Dir) Chown(uid, gid int) error {
	return d.err("fchown", "", unix.Fchown(d.fd, uid, gid))
}

func (d *Dir) Chmod(perm uint32) error {
	return d.err("fchmod", "", unix.Fchmod(d.fd, perm))
}

func (d *Dir) err(op, name string, err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, unix.ELOOP) {
		err = ErrSymlink
	}
	p := d.path
	if name != "" {
		p = path.Join(d.path, name)
	}
	return &os.PathError{Op: op, Path: p, Err: err}
}

func checkName(name string) error {
	if name == "" || name == "." || name == ".." || strings.ContainsAny(name, "/\x00") {
		return fmt.Errorf("%w: %q", ErrBadPath, name)
	}
	return nil
}

func splitRel(rel string) ([]string, error) {
	if rel == "" || strings.HasPrefix(rel, "/") {
		return nil, fmt.Errorf("%w: %q must be relative", ErrBadPath, rel)
	}
	parts := strings.Split(rel, "/")
	for _, p := range parts {
		if err := checkName(p); err != nil {
			return nil, err
		}
	}
	return parts, nil
}

// OpenDir opens a directory beneath d. No component may be a symlink.
func (d *Dir) OpenDir(rel string) (*Dir, error) {
	parts, err := splitRel(rel)
	if err != nil {
		return nil, err
	}
	fd, err := openBeneath(d.fd, parts, unix.O_RDONLY|unix.O_DIRECTORY)
	if err != nil {
		return nil, d.err("open", rel, err)
	}
	return &Dir{fd: fd, path: path.Join(d.path, rel)}, nil
}

// openChild opens one component without following it.
func openChild(dirfd int, name string, flags int) (int, error) {
	fd, err := unix.Openat(dirfd, name, flags|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if errors.Is(err, unix.ENOTDIR) {
		var st unix.Stat_t
		if unix.Fstatat(dirfd, name, &st, unix.AT_SYMLINK_NOFOLLOW) == nil && st.Mode&unix.S_IFMT == unix.S_IFLNK {
			err = unix.ELOOP
		}
	}
	return fd, err
}

// openWalk resolves parts one component at a time with O_NOFOLLOW.
func openWalk(dirfd int, parts []string, flags int) (int, error) {
	cur := dirfd
	for i, p := range parts {
		f := unix.O_RDONLY | unix.O_DIRECTORY
		if i == len(parts)-1 {
			f = flags
		}
		next, err := openChild(cur, p, f)
		if cur != dirfd {
			unix.Close(cur)
		}
		if err != nil {
			return -1, err
		}
		cur = next
	}
	return cur, nil
}

// Lstat stats a direct child without following it.
func (d *Dir) Lstat(name string) (Info, error) {
	if err := checkName(name); err != nil {
		return Info{}, err
	}
	var st unix.Stat_t
	if err := unix.Fstatat(d.fd, name, &st, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return Info{}, d.err("lstat", name, err)
	}
	return infoFrom(&st), nil
}

// Mkdir creates a direct child directory owned by uid:gid with the exact
// permissions given (umask does not apply), or opens it if it already
// exists. An existing entry that is not a real directory is an error.
// Existing directories keep their ownership and mode.
func (d *Dir) Mkdir(name string, perm uint32, uid, gid int) (*Dir, error) {
	if err := checkName(name); err != nil {
		return nil, err
	}
	created := true
	if err := unix.Mkdirat(d.fd, name, 0o700); err != nil {
		if !errors.Is(err, unix.EEXIST) {
			return nil, d.err("mkdir", name, err)
		}
		created = false
	}
	fd, err := openChild(d.fd, name, unix.O_RDONLY|unix.O_DIRECTORY)
	if err != nil {
		if errors.Is(err, unix.ENOTDIR) {
			err = ErrNotDir
		}
		return nil, d.err("open", name, err)
	}
	child := &Dir{fd: fd, path: path.Join(d.path, name)}
	if created {
		if err := child.Chown(uid, gid); err != nil {
			child.Close()
			return nil, err
		}
		if err := child.Chmod(perm); err != nil {
			child.Close()
			return nil, err
		}
	}
	return child, nil
}

// MkdirAll creates every missing directory in rel with the given owner and
// permissions and returns the leaf.
func (d *Dir) MkdirAll(rel string, perm uint32, uid, gid int) (*Dir, error) {
	parts, err := splitRel(rel)
	if err != nil {
		return nil, err
	}
	cur := d
	for _, p := range parts {
		next, err := cur.Mkdir(p, perm, uid, gid)
		if cur != d {
			cur.Close()
		}
		if err != nil {
			return nil, err
		}
		cur = next
	}
	return cur, nil
}

// EnsureDir is MkdirAll followed by forcing the leaf's owner and mode. It
// is used for directories FastCP manages, where a wrong owner left by an
// older version must be corrected.
func (d *Dir) EnsureDir(rel string, perm uint32, uid, gid int) (*Dir, error) {
	leaf, err := d.MkdirAll(rel, perm, uid, gid)
	if err != nil {
		return nil, err
	}
	info, err := leaf.Stat()
	if err == nil && (int(info.UID) != uid || int(info.GID) != gid) {
		err = leaf.Chown(uid, gid)
	}
	if err == nil && info.Perm() != perm {
		err = leaf.Chmod(perm)
	}
	if err != nil {
		leaf.Close()
		return nil, err
	}
	return leaf, nil
}

func tempName(name string) string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return ".fastcp-" + hex.EncodeToString(b[:]) + "-" + name
}

// WriteFile atomically replaces the direct child name with data, owned by
// uid:gid with mode perm. If name is an existing symlink or hard link, the
// directory entry is replaced and the link target is left untouched.
func (d *Dir) WriteFile(name string, data []byte, perm uint32, uid, gid int) error {
	if err := checkName(name); err != nil {
		return err
	}
	tmp := tempName(name)
	fd, err := unix.Openat(d.fd, tmp, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0o600)
	if err != nil {
		return d.err("create", tmp, err)
	}
	f := os.NewFile(uintptr(fd), path.Join(d.path, tmp))
	cleanup := func(err error) error {
		f.Close()
		unix.Unlinkat(d.fd, tmp, 0)
		return err
	}
	if _, err := f.Write(data); err != nil {
		return cleanup(d.err("write", tmp, err))
	}
	if err := unix.Fchown(fd, uid, gid); err != nil {
		return cleanup(d.err("fchown", tmp, err))
	}
	if err := unix.Fchmod(fd, perm); err != nil {
		return cleanup(d.err("fchmod", tmp, err))
	}
	if err := f.Sync(); err != nil {
		return cleanup(d.err("fsync", tmp, err))
	}
	if err := f.Close(); err != nil {
		unix.Unlinkat(d.fd, tmp, 0)
		return d.err("close", tmp, err)
	}
	if err := unix.Renameat(d.fd, tmp, d.fd, name); err != nil {
		unix.Unlinkat(d.fd, tmp, 0)
		return d.err("rename", name, err)
	}
	return nil
}

// OpenFile opens a direct child regular file read-only without following
// it. The caller must close the file.
func (d *Dir) OpenFile(name string) (*os.File, Info, error) {
	if err := checkName(name); err != nil {
		return nil, Info{}, err
	}
	fd, err := openChild(d.fd, name, unix.O_RDONLY|unix.O_NONBLOCK|unix.O_NOCTTY)
	if err != nil {
		return nil, Info{}, d.err("open", name, err)
	}
	var st unix.Stat_t
	if err := unix.Fstat(fd, &st); err != nil {
		unix.Close(fd)
		return nil, Info{}, d.err("fstat", name, err)
	}
	info := infoFrom(&st)
	if !info.IsRegular() {
		unix.Close(fd)
		return nil, Info{}, d.err("open", name, ErrNotFile)
	}
	return os.NewFile(uintptr(fd), path.Join(d.path, name)), info, nil
}

// ReadFile reads a direct child regular file of at most limit bytes.
func (d *Dir) ReadFile(name string, limit int64) ([]byte, Info, error) {
	f, info, err := d.OpenFile(name)
	if err != nil {
		return nil, Info{}, err
	}
	defer f.Close()
	if info.Size > limit {
		return nil, info, d.err("read", name, ErrTooLarge)
	}
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, info, d.err("read", name, err)
	}
	if int64(len(data)) > limit {
		return nil, info, d.err("read", name, ErrTooLarge)
	}
	return data, info, nil
}

// Lchown changes the owner of a direct child without following it.
func (d *Dir) Lchown(name string, uid, gid int) error {
	if err := checkName(name); err != nil {
		return err
	}
	return d.err("lchown", name, unix.Fchownat(d.fd, name, uid, gid, unix.AT_SYMLINK_NOFOLLOW))
}

// Names lists the entries of the directory.
func (d *Dir) Names() ([]string, error) {
	fd, err := unix.Openat(d.fd, ".", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, d.err("open", "", err)
	}
	f := os.NewFile(uintptr(fd), d.path)
	defer f.Close()
	names, err := f.Readdirnames(-1)
	if err != nil {
		return nil, d.err("readdir", "", err)
	}
	return names, nil
}

// Remove deletes a direct child file, symlink or empty directory.
func (d *Dir) Remove(name string) error {
	info, err := d.Lstat(name)
	if err != nil {
		if errors.Is(err, unix.ENOENT) {
			return nil
		}
		return err
	}
	flags := 0
	if info.IsDir() {
		flags = unix.AT_REMOVEDIR
	}
	if err := unix.Unlinkat(d.fd, name, flags); err != nil && !errors.Is(err, unix.ENOENT) {
		return d.err("remove", name, err)
	}
	return nil
}

// RemoveAll deletes a direct child and everything beneath it. Symlinks are
// removed, never followed, and directories on another filesystem are not
// entered.
func (d *Dir) RemoveAll(name string) error {
	self, err := d.Stat()
	if err != nil {
		return err
	}
	return d.removeAll(name, self.Dev, 0)
}

func (d *Dir) removeAll(name string, dev uint64, depth int) error {
	if depth > 512 {
		return d.err("remove", name, errors.New("directory tree too deep"))
	}
	info, err := d.Lstat(name)
	if err != nil {
		if errors.Is(err, unix.ENOENT) {
			return nil
		}
		return err
	}
	if !info.IsDir() {
		if err := unix.Unlinkat(d.fd, name, 0); err != nil && !errors.Is(err, unix.ENOENT) {
			return d.err("remove", name, err)
		}
		return nil
	}
	if info.Dev != dev {
		return d.err("remove", name, errors.New("refusing to cross a mount point"))
	}
	for attempt := 0; attempt < 5; attempt++ {
		child, err := d.OpenDir(name)
		if err != nil {
			if errors.Is(err, unix.ENOENT) {
				return nil
			}
			return err
		}
		names, err := child.Names()
		if err == nil {
			for _, n := range names {
				if err = child.removeAll(n, dev, depth+1); err != nil {
					break
				}
			}
		}
		child.Close()
		if err != nil {
			return err
		}
		err = unix.Unlinkat(d.fd, name, unix.AT_REMOVEDIR)
		if err == nil || errors.Is(err, unix.ENOENT) {
			return nil
		}
		if !errors.Is(err, unix.ENOTEMPTY) && !errors.Is(err, unix.EEXIST) {
			return d.err("remove", name, err)
		}
	}
	return d.err("remove", name, unix.ENOTEMPTY)
}

// WalkFunc is called for every entry beneath the walked directory. parent
// is the open directory that contains name.
type WalkFunc func(parent *Dir, name string, info Info) error

// Walk visits every entry beneath d, parents before children, without
// following symlinks or crossing filesystems.
func (d *Dir) Walk(fn WalkFunc) error {
	self, err := d.Stat()
	if err != nil {
		return err
	}
	return d.walk(fn, self.Dev, 0)
}

func (d *Dir) walk(fn WalkFunc, dev uint64, depth int) error {
	if depth > 512 {
		return d.err("walk", "", errors.New("directory tree too deep"))
	}
	names, err := d.Names()
	if err != nil {
		return err
	}
	for _, name := range names {
		info, err := d.Lstat(name)
		if err != nil {
			if errors.Is(err, unix.ENOENT) {
				continue
			}
			return err
		}
		if err := fn(d, name, info); err != nil {
			return err
		}
		if !info.IsDir() || info.Dev != dev {
			continue
		}
		child, err := d.OpenDir(name)
		if err != nil {
			if errors.Is(err, unix.ENOENT) || errors.Is(err, ErrSymlink) {
				continue
			}
			return err
		}
		err = child.walk(fn, dev, depth+1)
		child.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

// openMeta opens a direct child directory or regular file for metadata
// changes (fchown, fsetxattr) and reports what it is.
func (d *Dir) openMeta(name string) (int, Info, error) {
	if err := checkName(name); err != nil {
		return -1, Info{}, err
	}
	fd, err := openChild(d.fd, name, unix.O_RDONLY|unix.O_NONBLOCK|unix.O_NOCTTY)
	if err != nil {
		return -1, Info{}, d.err("open", name, err)
	}
	var st unix.Stat_t
	if err := unix.Fstat(fd, &st); err != nil {
		unix.Close(fd)
		return -1, Info{}, d.err("fstat", name, err)
	}
	info := infoFrom(&st)
	if !info.IsDir() && !info.IsRegular() {
		unix.Close(fd)
		return -1, Info{}, d.err("open", name, ErrNotFile)
	}
	return fd, info, nil
}
