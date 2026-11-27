package gouci

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/honeybbq/gouci/internal/errs"
)

// Clone returns an independent deep copy of p, preserving internal section
// identities so Diff accurately tracks renames, deletions, and reorders across
// cloned packages.
func (p *Package) Clone() *Package {
	if p == nil {
		return nil
	}
	cp := &Package{
		sections: make([]*Section, len(p.sections)),
		name:     p.name,
		nSection: p.nSection,
	}
	for i, s := range p.sections {
		secCopy := &Section{
			pkg:       cp,
			entries:   make([]Entry, len(s.entries)),
			typ:       s.typ,
			name:      s.name,
			id:        s.id,
			uid:       s.uid,
			anonymous: s.anonymous,
		}
		for j, e := range s.entries {
			secCopy.entries[j] = e.clone()
		}
		cp.sections[i] = secCopy
	}

	return cp
}

// Sections returns a copy of the package's section slice in declaration order.
func (p *Package) Sections() []*Section {
	if len(p.sections) == 0 {
		return nil
	}

	return slices.Clone(p.sections)
}

// SectionsByType returns all sections of type typ (named and anonymous) in
// declaration order.
func (p *Package) SectionsByType(typ string) []*Section {
	var out []*Section
	for _, s := range p.sections {
		if s.typ == typ {
			out = append(out, s)
		}
	}

	return out
}

// Section resolves ref to a section in p. It supports named sections ("lan"),
// extended typed indices ("@rule[0]", "@rule[-1]" counting both named and
// anonymous sections of that type), untyped indices ("@[0]", "@[-1]"), and
// synthetic anonymous IDs ("cfg01xxxx").
func (p *Package) Section(ref string) (*Section, bool) {
	idx := p.sectionIndex(ref)
	if idx < 0 {
		return nil, false
	}

	return p.sections[idx], true
}

// AddSection appends a new section of type typ to p. Pass name == "" to create
// an anonymous section; pass a valid UCI name to create a named section.
func (p *Package) AddSection(typ, name string) (*Section, error) {
	if !validateType(typ) {
		return nil, errs.Wrapf(ErrInvalidName, "invalid section type %q", typ)
	}
	if name == "" {
		return p.appendAnonSection(typ), nil
	}
	if !validateName(name) {
		return nil, errs.Wrapf(ErrInvalidName, "invalid section name %q", name)
	}
	if p.findNamedSection(name) != nil {
		return nil, errs.Wrapf(ErrDuplicate, "section %q already exists", name)
	}

	return p.appendNamedSection(typ, name), nil
}

// DeleteSection removes the section resolved by ref and returns true if found.
func (p *Package) DeleteSection(ref string) bool {
	idx := p.sectionIndex(ref)
	if idx < 0 {
		return false
	}
	p.sections = slices.Delete(p.sections, idx, idx+1)

	return true
}

// RenameSection assigns newName to the section resolved by ref and clears its
// anonymous flag, matching libuci uci_rename.
func (p *Package) RenameSection(ref, newName string) error {
	if !validateName(newName) {
		return errs.Wrapf(ErrInvalidName, "invalid section name %q", newName)
	}
	idx := p.sectionIndex(ref)
	if idx < 0 {
		return errs.Wrapf(ErrNotFound, "section %q", ref)
	}
	target := p.sections[idx]
	if existing := p.findNamedSection(newName); existing != nil && existing != target {
		return errs.Wrapf(ErrDuplicate, "section %q already exists", newName)
	}
	target.name = newName
	target.id = newName
	target.anonymous = false

	return nil
}

// ReorderSection moves the section resolved by ref to 0-based index pos.
// Positions <= 0 move the section to index 0; positions beyond the end move it
// to the last index, matching libuci uci_reorder_section.
func (p *Package) ReorderSection(ref string, pos int) error {
	idx := p.sectionIndex(ref)
	if idx < 0 {
		return errs.Wrapf(ErrNotFound, "section %q", ref)
	}
	sec := p.sections[idx]
	p.sections = slices.Delete(p.sections, idx, idx+1)
	target := min(max(pos, 0), len(p.sections))
	p.sections = slices.Insert(p.sections, target, sec)

	return nil
}

func (p *Package) appendAnonSection(typ string) *Section {
	p.nSection++
	sec := &Section{
		pkg:       p,
		typ:       typ,
		id:        anonSectionID(p.nSection, typ),
		uid:       nextSectionUID.Add(1),
		anonymous: true,
	}
	p.sections = append(p.sections, sec)

	return sec
}

func (p *Package) appendNamedSection(typ, name string) *Section {
	p.nSection++
	sec := &Section{
		pkg:  p,
		typ:  typ,
		name: name,
		id:   name,
		uid:  nextSectionUID.Add(1),
	}
	p.sections = append(p.sections, sec)

	return sec
}

// appendSectionWithID creates a section with an explicit synthetic ID (used by
// delta replay for "+pkg.cfgXXXXXX='type'").
func (p *Package) appendSectionWithID(typ, id string, anonymous bool) *Section {
	p.nSection++
	name := id
	if anonymous {
		name = ""
	}
	sec := &Section{
		pkg:       p,
		typ:       typ,
		name:      name,
		id:        id,
		uid:       nextSectionUID.Add(1),
		anonymous: anonymous,
	}
	p.sections = append(p.sections, sec)

	return sec
}

func (p *Package) findNamedSection(name string) *Section {
	for _, s := range p.sections {
		if !s.anonymous && s.name == name {
			return s
		}
	}

	return nil
}

func (p *Package) sectionIndex(ref string) int {
	if ref == "" {
		return -1
	}
	if ref[0] == '@' {
		return p.extendedSectionIndex(ref)
	}
	for i, s := range p.sections {
		if !s.anonymous && s.name == ref {
			return i
		}
	}
	for i, s := range p.sections {
		if s.anonymous && s.id == ref {
			return i
		}
	}

	return -1
}

func (p *Package) extendedSectionIndex(ref string) int {
	typ, want, ok := parseExtendedRef(ref)
	if !ok {
		return -1
	}
	if want < 0 {
		count := 0
		for _, s := range p.sections {
			if typ == "" || s.typ == typ {
				count++
			}
		}
		want += count
	}
	if want < 0 {
		return -1
	}
	seen := 0
	for i, s := range p.sections {
		if typ != "" && s.typ != typ {
			continue
		}
		if seen == want {
			return i
		}
		seen++
	}

	return -1
}

// parseExtendedRef parses "@type[idx]" or "@[idx]" matching uci_lookup_ext_section.
func parseExtendedRef(ref string) (string, int, bool) {
	if !strings.HasPrefix(ref, "@") || !strings.HasSuffix(ref, "]") {
		return "", 0, false
	}
	inner := ref[1 : len(ref)-1]
	typ, idxStr, ok := strings.Cut(inner, "[")
	if !ok {
		return "", 0, false
	}
	if typ != "" && !validateType(typ) {
		return "", 0, false
	}
	if idxStr == "" {
		return "", 0, false
	}
	idx, err := strconv.Atoi(idxStr)
	if err != nil {
		return "", 0, false
	}

	return typ, idx, true
}

func (p *Package) sectionLabel(target *Section) string {
	if !target.anonymous {
		return target.name
	}
	ordinal := 0
	for _, s := range p.sections {
		if s == target {
			return fmt.Sprintf("@%s[%d]", target.typ, ordinal)
		}
		if s.typ == target.typ {
			ordinal++
		}
	}

	return target.id
}
