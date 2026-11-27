package gouci

import "testing"

const diffSample = `
config rule
	option name 'r0'

config rule
	option name 'r1'

config rule
	option name 'r2'

config interface 'lan'
	option ipaddr '192.168.1.1'
	list dns '1.1.1.1'
`

func TestDiffCloneDeleteAndReorder(t *testing.T) {
	base, err := ParseBytes([]byte(diffSample))
	if err != nil {
		t.Fatalf("ParseBytes: %v", err)
	}
	if diff := Diff(base, base.Clone()); len(diff) != 0 {
		t.Fatalf("Diff(base, clone) = %+v, want empty", diff)
	}

	delMid := base.Clone()
	delMid.DeleteSection("@rule[1]")
	changes := Diff(base, delMid)
	if len(changes) != 1 || changes[0].Kind != ChangeSectionDeleted || changes[0].Section != "@rule[1]" {
		t.Fatalf("delete middle anon diff = %+v, want single ChangeSectionDeleted @rule[1]", changes)
	}

	reordered := base.Clone()
	mustNilErr(t, reordered.ReorderSection("@rule[2]", 0))
	changes = Diff(base, reordered)
	if len(changes) != 1 || changes[0].Kind != ChangeSectionReordered || changes[0].Section != "@rule[0]" {
		t.Fatalf("reorder diff = %+v, want single ChangeSectionReordered @rule[0]", changes)
	}
}

func TestDiffCloneMutations(t *testing.T) {
	base, err := ParseBytes([]byte(diffSample))
	if err != nil {
		t.Fatalf("ParseBytes: %v", err)
	}

	mut := base.Clone()
	_ = mut.RenameSection("@rule[0]", "named_r0")
	r0, _ := mut.Section("named_r0")
	_ = r0.SetType("redirect")
	lan, _ := mut.Section("lan")
	_ = lan.Set("ipaddr", "10.0.0.1")
	_ = lan.AddList("dns", "8.8.8.8")
	_ = lan.Set("proto", "static")
	r1, _ := mut.Section("@rule[0]")
	r1.Delete("name")
	_, _ = mut.AddSection("zone", "wan")

	changes := Diff(base, mut)
	seen := make(map[ChangeKind]int)
	for _, ch := range changes {
		seen[ch.Kind]++
	}
	wantKinds := []ChangeKind{
		ChangeSectionAdded,
		ChangeSectionRenamed,
		ChangeSectionTypeChanged,
		ChangeOptionSet,
		ChangeOptionDeleted,
		ChangeListChanged,
	}
	for _, kind := range wantKinds {
		if seen[kind] == 0 {
			t.Errorf("expected ChangeKind %s in diff %+v", kind, changes)
		}
	}
}

func TestDiffIndependentPackagesAndNil(t *testing.T) {
	a, _ := ParseBytes([]byte("config rule\n\toption a '1'\nconfig iface 'lan'\n\toption ip '1.1.1.1'\n"))
	b, _ := ParseBytes([]byte("config rule\n\toption a '1'\nconfig iface 'lan'\n\toption ip '1.1.1.1'\n"))
	if changes := Diff(a, b); len(changes) != 0 {
		t.Fatalf("identical independent packages produced diff: %+v", changes)
	}

	if changes := Diff(nil, a); len(changes) != 2 {
		t.Fatalf("Diff(nil, a) = %+v, want 2 additions", changes)
	}
	if changes := Diff(a, nil); len(changes) != 2 {
		t.Fatalf("Diff(a, nil) = %+v, want 2 deletions", changes)
	}
}
