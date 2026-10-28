//go:build darwin

package safefs

const ACLSupported = false

func (d *Dir) SetUserACL(uid int, perm uint16, def int) error { return nil }

func (d *Dir) SetUserACLAt(name string, uid int, perm uint16, def int) error { return nil }
