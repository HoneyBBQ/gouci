package gouci

import "slices"

const (
	// ChangeSectionAdded reports a newly added section.
	ChangeSectionAdded ChangeKind = iota + 1
	// ChangeSectionDeleted reports a removed section.
	ChangeSectionDeleted
	// ChangeSectionRenamed reports a section whose name or anonymity changed.
	ChangeSectionRenamed
	// ChangeSectionTypeChanged reports a section whose type changed.
	ChangeSectionTypeChanged
	// ChangeSectionReordered reports a section whose relative position changed.
	ChangeSectionReordered
	// ChangeOptionSet reports an added or modified scalar option (including list->scalar replacement).
	ChangeOptionSet
	// ChangeOptionDeleted reports a removed option or list key.
	ChangeOptionDeleted
	// ChangeListChanged reports an added or modified list (including scalar->list upgrade).
	ChangeListChanged
)

// ChangeKind classifies a semantic configuration change returned by Diff.
type ChangeKind uint8

// Change describes a single semantic difference between two Packages.
type Change struct {
	// Old is the previous Entry for option/list modifications or deletions, or nil.
	Old *Entry
	// New is the updated Entry for option/list additions or modifications, or nil.
	New *Entry
	// Section is the section's name (if named) or "@type[idx]" (if anonymous).
	Section string
	// Option is the option/list key for entry-level changes, or empty for section-level changes.
	Option string
	// Kind identifies the category of change.
	Kind ChangeKind
}

type sectionPair struct {
	beforeIdx int
	afterIdx  int
}

// String returns a human-readable label for k.
func (k ChangeKind) String() string {
	switch k {
	case ChangeSectionAdded:
		return "section_added"
	case ChangeSectionDeleted:
		return "section_deleted"
	case ChangeSectionRenamed:
		return "section_renamed"
	case ChangeSectionTypeChanged:
		return "section_type_changed"
	case ChangeSectionReordered:
		return "section_reordered"
	case ChangeOptionSet:
		return "option_set"
	case ChangeOptionDeleted:
		return "option_deleted"
	case ChangeListChanged:
		return "list_changed"
	default:
		return "unknown"
	}
}

// Diff computes the semantic differences between before and after.
// Nil packages are treated as empty. When after is derived from before.Clone(),
// internal section identities accurately track anonymous section deletions,
// renames, and reorders; independent packages pair named sections by name and
// anonymous sections by (type, ordinal).
func Diff(before, after *Package) []Change {
	if before == nil {
		before = &Package{}
	}
	if after == nil {
		after = &Package{}
	}
	pairs, matchedBefore, matchedAfter := pairSections(before.sections, after.sections)

	var changes []Change
	for i, s := range before.sections {
		if !matchedBefore[i] {
			changes = append(changes, Change{
				Kind:    ChangeSectionDeleted,
				Section: before.sectionLabel(s),
			})
		}
	}
	for i, s := range after.sections {
		if !matchedAfter[i] {
			changes = append(changes, Change{
				Kind:    ChangeSectionAdded,
				Section: after.sectionLabel(s),
			})
		}
	}

	changes = append(changes, diffReorders(after, pairs)...)
	for _, pair := range pairs {
		changes = append(changes, diffSection(after, before.sections[pair.beforeIdx], after.sections[pair.afterIdx])...)
	}

	return changes
}

func pairSections(before, after []*Section) ([]sectionPair, []bool, []bool) {
	matchedBefore := make([]bool, len(before))
	matchedAfter := make([]bool, len(after))
	afterToBefore := make([]int, len(after))
	for i := range afterToBefore {
		afterToBefore[i] = -1
	}

	pairByUID(before, after, matchedBefore, matchedAfter, afterToBefore)
	pairByName(before, after, matchedBefore, matchedAfter, afterToBefore)
	pairAnonByType(before, after, matchedBefore, matchedAfter, afterToBefore)

	pairs := make([]sectionPair, 0, len(after))
	for ai, bi := range afterToBefore {
		if bi >= 0 {
			pairs = append(pairs, sectionPair{beforeIdx: bi, afterIdx: ai})
		}
	}

	return pairs, matchedBefore, matchedAfter
}

func pairByUID(before, after []*Section, mb, ma []bool, a2b []int) {
	byUID := make(map[uint64]int, len(before))
	for i, s := range before {
		if s.uid != 0 {
			byUID[s.uid] = i
		}
	}
	for ai, s := range after {
		if s.uid == 0 {
			continue
		}
		if bi, ok := byUID[s.uid]; ok && !mb[bi] {
			mb[bi] = true
			ma[ai] = true
			a2b[ai] = bi
		}
	}
}

func pairByName(before, after []*Section, mb, ma []bool, a2b []int) {
	byName := make(map[string]int, len(before))
	for i, s := range before {
		if !mb[i] && !s.anonymous {
			byName[s.name] = i
		}
	}
	for ai, s := range after {
		if ma[ai] || s.anonymous {
			continue
		}
		if bi, ok := byName[s.name]; ok && !mb[bi] {
			mb[bi] = true
			ma[ai] = true
			a2b[ai] = bi
		}
	}
}

func pairAnonByType(before, after []*Section, mb, ma []bool, a2b []int) {
	queues := make(map[string][]int)
	for i, s := range before {
		if !mb[i] && s.anonymous {
			queues[s.typ] = append(queues[s.typ], i)
		}
	}
	for ai, s := range after {
		if ma[ai] || !s.anonymous {
			continue
		}
		q := queues[s.typ]
		if len(q) == 0 {
			continue
		}
		bi := q[0]
		queues[s.typ] = q[1:]
		mb[bi] = true
		ma[ai] = true
		a2b[ai] = bi
	}
}

// diffReorders uses longest increasing subsequence on matched before-indices to
// report only the minimal set of sections whose relative order changed.
func diffReorders(after *Package, pairs []sectionPair) []Change {
	if len(pairs) < 2 {
		return nil
	}
	inOrder := lisMask(pairs)
	var changes []Change
	for i, pair := range pairs {
		if !inOrder[i] {
			changes = append(changes, Change{
				Kind:    ChangeSectionReordered,
				Section: after.sectionLabel(after.sections[pair.afterIdx]),
			})
		}
	}

	return changes
}

func lisMask(pairs []sectionPair) []bool {
	n := len(pairs)
	dp := make([]int, n)
	prev := make([]int, n)
	bestLen, bestEnd := 0, 0
	for i := range n {
		dp[i] = 1
		prev[i] = -1
		for j := range i {
			if pairs[j].beforeIdx < pairs[i].beforeIdx && dp[j]+1 > dp[i] {
				dp[i] = dp[j] + 1
				prev[i] = j
			}
		}
		if dp[i] > bestLen {
			bestLen = dp[i]
			bestEnd = i
		}
	}
	mask := make([]bool, n)
	for cur := bestEnd; cur >= 0; cur = prev[cur] {
		mask[cur] = true
	}

	return mask
}

func diffSection(after *Package, bs, as *Section) []Change {
	label := after.sectionLabel(as)
	var changes []Change
	if bs.anonymous != as.anonymous || bs.name != as.name {
		changes = append(changes, Change{Kind: ChangeSectionRenamed, Section: label})
	}
	if bs.typ != as.typ {
		changes = append(changes, Change{Kind: ChangeSectionTypeChanged, Section: label})
	}

	for _, oldEntry := range bs.entries {
		if as.entryIndex(oldEntry.Key) < 0 {
			cp := oldEntry.clone()
			changes = append(changes, Change{
				Kind:    ChangeOptionDeleted,
				Section: label,
				Option:  oldEntry.Key,
				Old:     &cp,
			})
		}
	}
	for _, newEntry := range as.entries {
		if ch, changed := diffEntry(label, bs, newEntry); changed {
			changes = append(changes, ch)
		}
	}

	return changes
}

func diffEntry(label string, bs *Section, newEntry Entry) (Change, bool) {
	oldIdx := bs.entryIndex(newEntry.Key)
	if oldIdx < 0 {
		newCopy := newEntry.clone()

		return Change{
			Kind:    entryChangeKind(newEntry.Kind),
			Section: label,
			Option:  newEntry.Key,
			New:     &newCopy,
		}, true
	}
	oldEntry := bs.entries[oldIdx]
	if entriesEqual(oldEntry, newEntry) {
		return Change{}, false
	}
	oldCopy := oldEntry.clone()
	newCopy := newEntry.clone()

	return Change{
		Kind:    entryChangeKind(newEntry.Kind),
		Section: label,
		Option:  newEntry.Key,
		Old:     &oldCopy,
		New:     &newCopy,
	}, true
}

func entryChangeKind(kind EntryKind) ChangeKind {
	if kind == KindList {
		return ChangeListChanged
	}

	return ChangeOptionSet
}

func entriesEqual(a, b Entry) bool {
	if a.Kind != b.Kind {
		return false
	}
	if a.Kind == KindScalar {
		return a.Value == b.Value
	}

	return slices.Equal(a.Values, b.Values)
}
