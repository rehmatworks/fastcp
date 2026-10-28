//go:build linux

package safefs

import (
	"errors"

	"golang.org/x/sys/unix"
)

// ACLSupported reports whether named-user ACLs can be applied on this OS.
const ACLSupported = true

func fgetACL(fd int, attr string) ([]aclEntry, bool, error) {
	size, err := unix.Fgetxattr(fd, attr, nil)
	if errors.Is(err, unix.ENODATA) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	buf := make([]byte, size)
	n, err := unix.Fgetxattr(fd, attr, buf)
	if err != nil {
		return nil, false, err
	}
	acl, err := decodeACL(buf[:n])
	return acl, true, err
}

// setUserACL grants uid perm on fd's access ACL and, when def >= 0, adds
// the same kind of entry to a directory's default ACL so new children
// inherit it. The existing entries are preserved.
func setUserACL(fd int, info Info, uid int, perm uint16, def int) error {
	access, ok, err := fgetACL(fd, aclXattrAccess)
	if err != nil {
		return err
	}
	if !ok {
		access = aclFromMode(info.Mode)
	}
	if cur, has := userPerm(access, uint32(uid)); !has || cur != perm {
		if err := unix.Fsetxattr(fd, aclXattrAccess, encodeACL(withUser(access, uint32(uid), perm)), 0); err != nil {
			return err
		}
	}
	if def < 0 || !info.IsDir() {
		return nil
	}
	dacl, ok, err := fgetACL(fd, aclXattrDefault)
	if err != nil {
		return err
	}
	if !ok {
		dacl = baseEntries(access)
	}
	if cur, has := userPerm(dacl, uint32(uid)); has && cur == uint16(def) {
		return nil
	}
	return unix.Fsetxattr(fd, aclXattrDefault, encodeACL(withUser(dacl, uint32(uid), uint16(def))), 0)
}

// SetUserACL grants uid perm on the directory itself. When def >= 0 the
// directory's default ACL also gets an entry with that permission.
func (d *Dir) SetUserACL(uid int, perm uint16, def int) error {
	info, err := d.Stat()
	if err != nil {
		return err
	}
	return d.err("setfacl", "", setUserACL(d.fd, info, uid, perm, def))
}

// SetUserACLAt is SetUserACL for a direct child directory or regular file.
// Other file types are skipped.
func (d *Dir) SetUserACLAt(name string, uid int, perm uint16, def int) error {
	fd, info, err := d.openMeta(name)
	if err != nil {
		if errors.Is(err, ErrNotFile) || errors.Is(err, ErrSymlink) || errors.Is(err, unix.ENOENT) {
			return nil
		}
		return err
	}
	defer unix.Close(fd)
	return d.err("setfacl", name, setUserACL(fd, info, uid, perm, def))
}
