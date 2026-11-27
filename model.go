package gouci

import (
	"slices"
	"sync/atomic"

	"github.com/honeybbq/gouci/internal/errs"
)

const (
	// KindScalar identifies a single-value UCI option statement.
	KindScalar EntryKind = iota + 1
	// KindList identifies an ordered multi-value UCI list statement.
	KindList
)

var nextSectionUID atomic.Uint64

// EntryKind distinguishes scalar UCI options from ordered UCI lists.
type EntryKind uint8

// Entry represents a single scalar option or ordered list inside a Section.
type Entry struct {
	// Key is the option or list identifier ([A-Za-z0-9_]).
	Key string
	// Value holds the scalar string when Kind is KindScalar.
	Value string
	// Values holds the ordered list elements when Kind is KindList.
	Values []string
	// Kind indicates whether this entry is a KindScalar option or a KindList.
	Kind EntryKind
}

// Package holds an ordered in-memory UCI configuration package.
type Package struct {
	name     string
	sections []*Section
	nSection int
}

// Section represents a single UCI config block inside a Package.
type Section struct {
	pkg       *Package
	typ       string
	name      string
	id        string
	entries   []Entry
	uid       uint64
	anonymous bool
}

// NewPackage constructs an empty Package with the given UCI package name.
func NewPackage(name string) (*Package, error) {
	if !validatePackageName(name) {
		return nil, errs.Wrapf(ErrInvalidName, "invalid package name %q", name)
	}

	return &Package{name: name}, nil
}

// String returns the UCI keyword corresponding to k ("option" or "list").
func (k EntryKind) String() string {
	switch k {
	case KindScalar:
		return "option"
	case KindList:
		return "list"
	default:
		return "unknown"
	}
}

func (e Entry) clone() Entry {
	out := Entry{
		Key:   e.Key,
		Value: e.Value,
		Kind:  e.Kind,
	}
	if e.Kind == KindList {
		out.Values = slices.Clone(e.Values)
	}

	return out
}

// Name returns the package name, or an empty string if parsed from a stream
// without a package header.
func (p *Package) Name() string {
	return p.name
}

// SetName updates the package name after validating it as a UCI package identifier.
func (p *Package) SetName(name string) error {
	if !validatePackageName(name) {
		return errs.Wrapf(ErrInvalidName, "invalid package name %q", name)
	}
	p.name = name

	return nil
}

// Type returns the section type (for example "interface" or "rule").
func (s *Section) Type() string {
	return s.typ
}

// Name returns the explicit section name, or an empty string for anonymous sections.
func (s *Section) Name() string {
	return s.name
}

// ID returns the section's lookup identifier: Name() for named sections, or
// libuci's deterministic cfg%02x%04x identifier for anonymous sections.
func (s *Section) ID() string {
	return s.id
}

// Anonymous reports whether the section has no explicit name in the config file.
func (s *Section) Anonymous() bool {
	return s.anonymous
}

// Entries returns a defensive copy of all options and lists in declaration order.
func (s *Section) Entries() []Entry {
	if len(s.entries) == 0 {
		return nil
	}
	out := make([]Entry, len(s.entries))
	for i, e := range s.entries {
		out[i] = e.clone()
	}

	return out
}

// Get returns the value of a scalar option and true, or ("", false) if the key
// is missing or is a list.
func (s *Section) Get(key string) (string, bool) {
	idx := s.entryIndex(key)
	if idx < 0 || s.entries[idx].Kind != KindScalar {
		return "", false
	}

	return s.entries[idx].Value, true
}

// GetList returns a defensive copy of a list option's values and true, or
// (nil, false) if the key is missing or is a scalar option.
func (s *Section) GetList(key string) ([]string, bool) {
	idx := s.entryIndex(key)
	if idx < 0 || s.entries[idx].Kind != KindList {
		return nil, false
	}

	return slices.Clone(s.entries[idx].Values), true
}

// GetEntry returns a defensive copy of the Entry for key (scalar or list) and true,
// or (Entry{}, false) if the key does not exist.
func (s *Section) GetEntry(key string) (Entry, bool) {
	idx := s.entryIndex(key)
	if idx < 0 {
		return Entry{}, false
	}

	return s.entries[idx].clone(), true
}

func (s *Section) entryIndex(key string) int {
	for i := range s.entries {
		if s.entries[i].Key == key {
			return i
		}
	}

	return -1
}
