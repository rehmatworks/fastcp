package safefs

import (
	"encoding/binary"
	"errors"
	"sort"
)

// POSIX ACL permission bits.
const (
	ACLRead  uint16 = 4
	ACLWrite uint16 = 2
	ACLExec  uint16 = 1
)

const (
	aclXattrAccess  = "system.posix_acl_access"
	aclXattrDefault = "system.posix_acl_default"
	aclVersion      = 2

	tagUserObj  uint16 = 0x01
	tagUser     uint16 = 0x02
	tagGroupObj uint16 = 0x04
	tagGroup    uint16 = 0x08
	tagMask     uint16 = 0x10
	tagOther    uint16 = 0x20

	aclUndefinedID uint32 = 0xFFFFFFFF
)

type aclEntry struct {
	Tag  uint16
	Perm uint16
	ID   uint32
}

func decodeACL(b []byte) ([]aclEntry, error) {
	if len(b) < 4 || (len(b)-4)%8 != 0 || binary.LittleEndian.Uint32(b) != aclVersion {
		return nil, errors.New("malformed POSIX ACL")
	}
	var out []aclEntry
	for off := 4; off < len(b); off += 8 {
		out = append(out, aclEntry{
			Tag:  binary.LittleEndian.Uint16(b[off:]),
			Perm: binary.LittleEndian.Uint16(b[off+2:]),
			ID:   binary.LittleEndian.Uint32(b[off+4:]),
		})
	}
	return out, nil
}

func encodeACL(entries []aclEntry) []byte {
	b := make([]byte, 4+8*len(entries))
	binary.LittleEndian.PutUint32(b, aclVersion)
	for i, e := range entries {
		off := 4 + 8*i
		binary.LittleEndian.PutUint16(b[off:], e.Tag)
		binary.LittleEndian.PutUint16(b[off+2:], e.Perm)
		binary.LittleEndian.PutUint32(b[off+4:], e.ID)
	}
	return b
}

// aclFromMode builds the minimal ACL equivalent to a file mode.
func aclFromMode(mode uint32) []aclEntry {
	return []aclEntry{
		{Tag: tagUserObj, Perm: uint16(mode>>6) & 7, ID: aclUndefinedID},
		{Tag: tagGroupObj, Perm: uint16(mode>>3) & 7, ID: aclUndefinedID},
		{Tag: tagOther, Perm: uint16(mode) & 7, ID: aclUndefinedID},
	}
}

// baseEntries returns the owner, owning group and other entries of acl.
func baseEntries(acl []aclEntry) []aclEntry {
	var out []aclEntry
	for _, e := range acl {
		if e.Tag == tagUserObj || e.Tag == tagGroupObj || e.Tag == tagOther {
			out = append(out, e)
		}
	}
	return out
}

// withUser sets the named user entry for uid and recomputes the mask the
// way setfacl does: the union of the owning group and all named entries.
// Entries are first reduced to what the old mask let through, so the new
// mask cannot widen them; otherwise a file created as 0644 in a directory
// whose default ACL has group::r-x would become group-executable.
func withUser(acl []aclEntry, uid uint32, perm uint16) []aclEntry {
	oldMask := uint16(7)
	for _, e := range acl {
		if e.Tag == tagMask {
			oldMask = e.Perm
		}
	}
	out := make([]aclEntry, 0, len(acl)+2)
	for _, e := range acl {
		if e.Tag == tagMask || (e.Tag == tagUser && e.ID == uid) {
			continue
		}
		if e.Tag == tagGroupObj || e.Tag == tagUser || e.Tag == tagGroup {
			e.Perm &= oldMask
		}
		out = append(out, e)
	}
	out = append(out, aclEntry{Tag: tagUser, Perm: perm & 7, ID: uid})
	var mask uint16
	for _, e := range out {
		if e.Tag == tagGroupObj || e.Tag == tagUser || e.Tag == tagGroup {
			mask |= e.Perm
		}
	}
	out = append(out, aclEntry{Tag: tagMask, Perm: mask, ID: aclUndefinedID})
	sort.Slice(out, func(i, j int) bool {
		if out[i].Tag != out[j].Tag {
			return out[i].Tag < out[j].Tag
		}
		return out[i].ID < out[j].ID
	})
	return out
}

func userPerm(acl []aclEntry, uid uint32) (uint16, bool) {
	for _, e := range acl {
		if e.Tag == tagUser && e.ID == uid {
			return e.Perm, true
		}
	}
	return 0, false
}
