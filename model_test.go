package gouci

import (
	"bytes"
	"errors"
	"testing"
)

func TestPackageAndSectionSemantics(t *testing.T) {
	pkg := mustNewPackage(t, "firewall")
	s0 := mustAddSection(t, pkg, "rule", "named_first")
	s1 := mustAddSection(t, pkg, "rule", "")
	s2 := mustAddSection(t, pkg, "zone", "")
	s3 := mustAddSection(t, pkg, "rule", "")

	if s0.Anonymous() || s0.Name() != "named_first" || s0.ID() != "named_first" {
		t.Errorf("unexpected named metadata: anon=%v name=%q id=%q", s0.Anonymous(), s0.Name(), s0.ID())
	}
	if !s1.Anonymous() || s1.Name() != "" || s1.ID() == "" {
		t.Errorf("unexpected anon metadata: anon=%v name=%q id=%q", s1.Anonymous(), s1.Name(), s1.ID())
	}

	assertSectionRef(t, pkg, "named_first", s0)
	assertSectionRef(t, pkg, "@rule[0]", s0)
	assertSectionRef(t, pkg, "@rule[1]", s1)
	assertSectionRef(t, pkg, "@rule[-1]", s3)
	assertSectionRef(t, pkg, "@rule[-3]", s0)
	assertSectionRef(t, pkg, "@[2]", s2)
	assertSectionRef(t, pkg, "@[-1]", s3)
	assertSectionRef(t, pkg, s1.ID(), s1)

	badRefs := []string{
		"", "@", "@rule", "@rule[]", "@rule[x]",
		"@rule[99]", "@rule[-99]", "@bad type[0]", "missing",
	}
	for _, bad := range badRefs {
		if _, ok := pkg.Section(bad); ok {
			t.Errorf("Section(%q) unexpectedly succeeded", bad)
		}
	}
	if got := pkg.SectionsByType("rule"); len(got) != 3 {
		t.Fatalf("SectionsByType(rule) = %d, want 3", len(got))
	}

	secs := pkg.Sections()
	secs[0] = nil
	if pkg.Sections()[0] == nil {
		t.Error("mutating Sections() slice corrupted Package")
	}
}

func TestSectionScalarAndListMutations(t *testing.T) {
	pkg := mustNewPackage(t, "network")
	lan := mustAddSection(t, pkg, "interface", "lan")

	mustNilErr(t, lan.Set("proto", "static"))
	if _, ok := lan.GetList("proto"); ok {
		t.Error("GetList on scalar should return false")
	}

	mustNilErr(t, lan.AddList("device", "vane0"))
	if _, ok := lan.Get("device"); ok {
		t.Error("Get on list should return false")
	}
	entry, ok := lan.GetEntry("device")
	if !ok || entry.Kind != KindList || len(entry.Values) != 1 || entry.Values[0] != "vane0" {
		t.Fatalf("GetEntry(device) = %+v, %v", entry, ok)
	}
	entry.Values[0] = "mutated"
	assertList(t, lan, "device", []string{"vane0"})

	mustNilErr(t, lan.AddList("proto", "dhcp"))
	assertList(t, lan, "proto", []string{"static", "dhcp"})

	mustNilErr(t, lan.Set("proto", ""))
	if _, found := lan.GetEntry("proto"); found {
		t.Error("Set empty should remove proto")
	}
	mustNilErr(t, lan.SetList("device", nil))
	if _, found := lan.GetEntry("device"); found {
		t.Error("SetList nil should remove device")
	}
}

func TestSectionDelListRenameAndType(t *testing.T) {
	pkg := mustNewPackage(t, "network")
	lan := mustAddSection(t, pkg, "interface", "lan")

	_ = lan.AddList("dns", "1.1.1.1")
	_ = lan.AddList("dns", "8.8.8.8")
	_ = lan.AddList("dns", "1.1.1.1")
	if !lan.DelList("dns", "1.1.1.1") {
		t.Error("DelList(1.1.1.1) = false, want true")
	}
	assertList(t, lan, "dns", []string{"8.8.8.8"})
	if lan.DelList("dns", "9.9.9.9") {
		t.Error("DelList(missing) = true, want false")
	}
	if !lan.DelList("dns", "8.8.8.8") {
		t.Error("DelList(8.8.8.8) = false, want true")
	}
	if _, ok := lan.GetEntry("dns"); ok {
		t.Error("dns key should be deleted after removing last item")
	}

	_ = lan.Set("a", "1")
	_ = lan.Set("b", "2")
	mustNilErr(t, lan.RenameOption("a", "a_new"))
	assertErrIs(t, lan.RenameOption("a_new", "b"), ErrDuplicate)
	assertErrIs(t, lan.RenameOption("missing", "c"), ErrNotFound)
	mustNilErr(t, lan.SetType("bridge-vlan"))
	if lan.Type() != "bridge-vlan" {
		t.Errorf("Type() = %q, want bridge-vlan", lan.Type())
	}
}

func TestSectionRenameAndReorder(t *testing.T) {
	pkg := mustNewPackage(t, "test")
	_ = mustAddSection(t, pkg, "rule", "first")
	anon := mustAddSection(t, pkg, "rule", "")
	_ = mustAddSection(t, pkg, "rule", "last")

	mustNilErr(t, pkg.ReorderSection("last", 0))
	if pkg.Sections()[0].Name() != "last" {
		t.Fatalf("expected 'last' at index 0, got %q", pkg.Sections()[0].Name())
	}
	mustNilErr(t, pkg.ReorderSection("last", 100))
	if pkg.Sections()[2].Name() != "last" {
		t.Fatalf("expected 'last' at index 2, got %q", pkg.Sections()[2].Name())
	}

	mustNilErr(t, pkg.RenameSection(anon.ID(), "middle"))
	if anon.Anonymous() || anon.Name() != "middle" || anon.ID() != "middle" {
		t.Errorf("RenameSection did not clear anonymous: anon=%v name=%q id=%q",
			anon.Anonymous(), anon.Name(), anon.ID())
	}
	assertErrIs(t, pkg.RenameSection("middle", "first"), ErrDuplicate)
	_, err := pkg.AddSection("rule", "first")
	assertErrIs(t, err, ErrDuplicate)
	if !pkg.DeleteSection("middle") || pkg.DeleteSection("middle") {
		t.Error("DeleteSection return value mismatch")
	}
}

func TestValidationErrors(t *testing.T) {
	_, err := NewPackage("../bad")
	assertErrIs(t, err, ErrInvalidName)

	pkg := mustNewPackage(t, "test")
	sec := mustAddSection(t, pkg, "rule", "first")

	assertErrIs(t, pkg.SetName("bad.name"), ErrInvalidName)
	_, err = pkg.AddSection("bad type", "x")
	assertErrIs(t, err, ErrInvalidName)
	_, err = pkg.AddSection("rule", "bad-name")
	assertErrIs(t, err, ErrInvalidName)

	assertErrIs(t, sec.Set("bad-opt", "v"), ErrInvalidName)
	assertErrIs(t, sec.Set("ok", "bad\x01ctrl"), ErrInvalidValue)
	assertErrIs(t, sec.SetList("ok", []string{"ok", "bad\x00nul"}), ErrInvalidValue)
	assertErrIs(t, sec.AddList("ok", "bad\x1fctrl"), ErrInvalidValue)
}

func TestFormatExportAndStringers(t *testing.T) {
	pkg := mustNewPackage(t, "demo")
	s1 := mustAddSection(t, pkg, "interface", "lan")
	_ = s1.Set("pass", "a'b\\c\"d")
	_ = s1.SetList("dns", []string{"1.1.1.1"})
	s2 := mustAddSection(t, pkg, "rule", "")
	_ = s2.Set("name", "Allow-Ping")

	formatted, err := pkg.Format()
	mustNilErr(t, err)
	want := "config interface 'lan'\n" +
		"\toption pass 'a'\\''b\\c\"d'\n" +
		"\tlist dns '1.1.1.1'\n" +
		"\n" +
		"config rule\n" +
		"\toption name 'Allow-Ping'\n"
	if string(formatted) != want {
		t.Fatalf("Format() =\n%q\nwant:\n%q", string(formatted), want)
	}

	var buf bytes.Buffer
	n, err := pkg.WriteTo(&buf)
	mustNilErr(t, err)
	if int(n) != len(formatted) || buf.String() != want {
		t.Fatalf("WriteTo() = (%d, %q)", n, buf.String())
	}

	exported, err := pkg.Export()
	mustNilErr(t, err)
	if string(exported) != "package demo\n\n"+want {
		t.Fatalf("Export() =\n%q", string(exported))
	}

	reparsed, err := ParseBytes(formatted)
	mustNilErr(t, err)
	formatted2, err := reparsed.Format()
	mustNilErr(t, err)
	if !bytes.Equal(formatted, formatted2) {
		t.Fatalf("round-trip mismatch:\n%s\nvs\n%s", formatted, formatted2)
	}
	_, err = reparsed.Export()
	assertErrIs(t, err, ErrInvalidName)
}

func TestEnumStringers(t *testing.T) {
	if KindScalar.String() != "option" || KindList.String() != "list" || EntryKind(99).String() != "unknown" {
		t.Error("EntryKind.String() mismatch")
	}
	if DeltaMerge.String() != "merge" || DeltaReject.String() != "reject" || DeltaPolicy(99).String() != "unknown" {
		t.Error("DeltaPolicy.String() mismatch")
	}
	if ChangeSectionAdded.String() != "section_added" || ChangeKind(99).String() != "unknown" {
		t.Error("ChangeKind.String() mismatch")
	}
}

func mustNewPackage(t *testing.T, name string) *Package {
	t.Helper()
	pkg, err := NewPackage(name)
	if err != nil {
		t.Fatalf("NewPackage(%q): %v", name, err)
	}

	return pkg
}

func mustAddSection(t *testing.T, pkg *Package, typ, name string) *Section {
	t.Helper()
	sec, err := pkg.AddSection(typ, name)
	if err != nil {
		t.Fatalf("AddSection(%q, %q): %v", typ, name, err)
	}

	return sec
}

func mustNilErr(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func assertErrIs(t *testing.T, err, target error) {
	t.Helper()
	if !errors.Is(err, target) {
		t.Errorf("err = %v, want %v", err, target)
	}
}

func assertSectionRef(t *testing.T, pkg *Package, ref string, want *Section) {
	t.Helper()
	got, ok := pkg.Section(ref)
	if !ok {
		t.Fatalf("Section(%q) not found", ref)
	}
	if got != want {
		t.Errorf("Section(%q) = %p (%s), want %p (%s)", ref, got, got.ID(), want, want.ID())
	}
}
