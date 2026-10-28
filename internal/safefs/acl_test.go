package safefs

import (
	"reflect"
	"testing"
)

func TestACLRoundTrip(t *testing.T) {
	in := []aclEntry{
		{Tag: tagUserObj, Perm: 7, ID: aclUndefinedID},
		{Tag: tagUser, Perm: 5, ID: 997},
		{Tag: tagGroupObj, Perm: 5, ID: aclUndefinedID},
		{Tag: tagMask, Perm: 5, ID: aclUndefinedID},
		{Tag: tagOther, Perm: 0, ID: aclUndefinedID},
	}
	out, err := decodeACL(encodeACL(in))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(in, out) {
		t.Fatalf("round trip mismatch: %v != %v", out, in)
	}
	if _, err := decodeACL([]byte{1, 0, 0, 0}); err == nil {
		t.Fatal("accepted wrong version")
	}
	if _, err := decodeACL([]byte{2, 0, 0, 0, 1}); err == nil {
		t.Fatal("accepted truncated entry")
	}
}

func TestWithUserRecomputesMaskAndSorts(t *testing.T) {
	acl := withUser(aclFromMode(0o750), 997, ACLExec)
	want := []aclEntry{
		{Tag: tagUserObj, Perm: 7, ID: aclUndefinedID},
		{Tag: tagUser, Perm: 1, ID: 997},
		{Tag: tagGroupObj, Perm: 5, ID: aclUndefinedID},
		{Tag: tagMask, Perm: 5, ID: aclUndefinedID},
		{Tag: tagOther, Perm: 0, ID: aclUndefinedID},
	}
	if !reflect.DeepEqual(acl, want) {
		t.Fatalf("got %v, want %v", acl, want)
	}

	acl = withUser(acl, 997, ACLRead|ACLExec)
	acl = withUser(acl, 1001, 0)
	if p, ok := userPerm(acl, 997); !ok || p != 5 {
		t.Fatalf("caddy entry = %d %v", p, ok)
	}
	if p, ok := userPerm(acl, 1001); !ok || p != 0 {
		t.Fatalf("deny entry = %d %v", p, ok)
	}
	masks := 0
	for _, e := range acl {
		if e.Tag == tagMask {
			masks++
		}
	}
	if masks != 1 {
		t.Fatalf("expected one mask entry, got %d", masks)
	}
}

func TestGrantKeepsInheritedEntriesWithinMask(t *testing.T) {
	// A 0644 file created under default:group::r-x gets group::r-x and
	// mask::r--, so the group can only read it.
	inherited := []aclEntry{
		{Tag: tagUserObj, Perm: 6, ID: aclUndefinedID},
		{Tag: tagUser, Perm: 5, ID: 997},
		{Tag: tagGroupObj, Perm: 5, ID: aclUndefinedID},
		{Tag: tagMask, Perm: 4, ID: aclUndefinedID},
		{Tag: tagOther, Perm: 0, ID: aclUndefinedID},
	}
	acl := withUser(inherited, 997, ACLRead)
	for _, e := range acl {
		if (e.Tag == tagGroupObj || e.Tag == tagMask) && e.Perm != ACLRead {
			t.Fatalf("group class widened: %v", acl)
		}
	}
}

func TestPrivateFileGrantDoesNotWidenGroup(t *testing.T) {
	acl := withUser(aclFromMode(0o600), 997, ACLRead)
	for _, e := range acl {
		if e.Tag == tagGroupObj && e.Perm != 0 {
			t.Fatalf("owning group gained access: %v", acl)
		}
		if e.Tag == tagOther && e.Perm != 0 {
			t.Fatalf("other gained access: %v", acl)
		}
	}
}
