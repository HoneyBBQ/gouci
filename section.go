package gouci

import (
	"slices"

	"github.com/honeybbq/gouci/internal/errs"
)

// SetType changes the section's type after validating it as a UCI section type.
func (s *Section) SetType(typ string) error {
	if !validateType(typ) {
		return errs.Wrapf(ErrInvalidName, "invalid section type %q", typ)
	}
	s.typ = typ

	return nil
}

// Set assigns a scalar option value, preserving key order if the key already
// exists (including replacing a list with a scalar). Following libuci uci_set
// semantics, setting value to "" removes the key if present.
func (s *Section) Set(key, value string) error {
	if !validateName(key) {
		return errs.Wrapf(ErrInvalidName, "invalid option name %q", key)
	}
	if !validateText(value) {
		return errs.Wrapf(ErrInvalidValue, "invalid value for option %q", key)
	}
	if value == "" {
		s.Delete(key)

		return nil
	}
	s.setScalarUnchecked(key, value)

	return nil
}

// SetList assigns an ordered list value, preserving key order if the key already
// exists (including replacing a scalar option). Passing an empty or nil slice
// deletes the key because UCI has no representation for a zero-element list.
func (s *Section) SetList(key string, values []string) error {
	if !validateName(key) {
		return errs.Wrapf(ErrInvalidName, "invalid option name %q", key)
	}
	for _, v := range values {
		if !validateText(v) {
			return errs.Wrapf(ErrInvalidValue, "invalid list value for %q", key)
		}
	}
	if len(values) == 0 {
		s.Delete(key)

		return nil
	}
	entry := Entry{
		Key:    key,
		Kind:   KindList,
		Values: slices.Clone(values),
	}
	if idx := s.entryIndex(key); idx >= 0 {
		s.entries[idx] = entry
	} else {
		s.entries = append(s.entries, entry)
	}

	return nil
}

// AddList appends value to a list option, creating a single-element list if the
// key does not exist or upgrading an existing scalar option in place (keeping
// the prior scalar as the first list element), matching libuci uci_add_list.
func (s *Section) AddList(key, value string) error {
	if !validateName(key) {
		return errs.Wrapf(ErrInvalidName, "invalid option name %q", key)
	}
	if !validateText(value) {
		return errs.Wrapf(ErrInvalidValue, "invalid list value for %q", key)
	}
	s.addListUnchecked(key, value)

	return nil
}

// DelList removes all occurrences of value from a list option, deleting the key
// if no elements remain. It returns true if at least one element was removed.
func (s *Section) DelList(key, value string) bool {
	idx := s.entryIndex(key)
	if idx < 0 || s.entries[idx].Kind != KindList {
		return false
	}
	oldVals := s.entries[idx].Values
	kept := make([]string, 0, len(oldVals))
	removed := false
	for _, v := range oldVals {
		if v == value {
			removed = true

			continue
		}
		kept = append(kept, v)
	}
	if !removed {
		return false
	}
	if len(kept) == 0 {
		s.entries = slices.Delete(s.entries, idx, idx+1)
	} else {
		s.entries[idx].Values = kept
	}

	return true
}

// RenameOption renames an existing option or list in place while preserving its
// declaration order.
func (s *Section) RenameOption(oldKey, newKey string) error {
	if !validateName(newKey) {
		return errs.Wrapf(ErrInvalidName, "invalid option name %q", newKey)
	}
	idx := s.entryIndex(oldKey)
	if idx < 0 {
		return errs.Wrapf(ErrNotFound, "option %q", oldKey)
	}
	if oldKey == newKey {
		return nil
	}
	if s.entryIndex(newKey) >= 0 {
		return errs.Wrapf(ErrDuplicate, "option %q already exists", newKey)
	}
	s.entries[idx].Key = newKey

	return nil
}

// Delete removes the option or list named key and returns true if it existed.
func (s *Section) Delete(key string) bool {
	idx := s.entryIndex(key)
	if idx < 0 {
		return false
	}
	s.entries = slices.Delete(s.entries, idx, idx+1)

	return true
}

func (s *Section) setScalarUnchecked(key, value string) {
	entry := Entry{
		Key:   key,
		Kind:  KindScalar,
		Value: value,
	}
	if idx := s.entryIndex(key); idx >= 0 {
		s.entries[idx] = entry
	} else {
		s.entries = append(s.entries, entry)
	}
}

func (s *Section) addListUnchecked(key, value string) {
	idx := s.entryIndex(key)
	if idx < 0 {
		s.entries = append(s.entries, Entry{
			Key:    key,
			Kind:   KindList,
			Values: []string{value},
		})

		return
	}
	if s.entries[idx].Kind == KindScalar {
		s.entries[idx] = Entry{
			Key:    key,
			Kind:   KindList,
			Values: []string{s.entries[idx].Value, value},
		}

		return
	}
	s.entries[idx].Values = append(s.entries[idx].Values, value)
}

// deleteListIndex removes the 0-based element at pos from a list option (used by
// libuci delta "-pkg.sec.opt='idx'" commands).
func (s *Section) deleteListIndex(key string, pos int) bool {
	idx := s.entryIndex(key)
	if idx < 0 || s.entries[idx].Kind != KindList {
		return false
	}
	vals := s.entries[idx].Values
	if pos < 0 || pos >= len(vals) {
		return false
	}
	vals = slices.Delete(vals, pos, pos+1)
	if len(vals) == 0 {
		s.entries = slices.Delete(s.entries, idx, idx+1)
	} else {
		s.entries[idx].Values = vals
	}

	return true
}
