package gouci

import (
	"bytes"
	"log/slog"
	"strconv"
	"strings"
)

const (
	deltaCmdAdd deltaCmd = iota + 1
	deltaCmdRemove
	deltaCmdChange
	deltaCmdRename
	deltaCmdReorder
	deltaCmdListAdd
	deltaCmdListDel
)

type deltaCmd uint8

type deltaTuple struct {
	pkg      string
	section  string
	option   string
	value    string
	cmd      deltaCmd
	hasValue bool
}

// replayDelta parses and replays libuci delta lines (delta.c) onto pkg.
// Malformed or unapplicable individual lines are skipped and logged via logger,
// matching libuci's uci_parse_delta fault tolerance.
func replayDelta(pkg *Package, data []byte, logger *slog.Logger) int {
	if bytes.IndexByte(data, 0) >= 0 {
		logger.Warn("ignoring delta file containing NUL byte", "package", pkg.name)

		return 0
	}
	lex := &lexer{data: data, line: 1}
	applied := 0
	for !lex.eof() {
		lineNum := lex.line
		arg, ok, malformed := lex.nextDeltaRecord()
		if !ok {
			if malformed {
				logger.Warn("skipping malformed delta line", "package", pkg.name, "line", lineNum)
			}

			continue
		}
		tuple, valid := parseDeltaTuple(arg)
		if !valid || tuple.pkg != pkg.name {
			logger.Warn("skipping invalid delta tuple", "package", pkg.name, "line", lineNum)

			continue
		}
		if applyDeltaTuple(pkg, tuple) {
			applied++
		} else {
			logger.Warn("skipping unapplicable delta tuple", "package", pkg.name, "line", lineNum, "section", tuple.section)
		}
	}

	return applied
}

// nextDeltaRecord reads a single shell-quoted argument occupying one logical
// delta record (possibly spanning multiple lines inside quotes), rejecting any
// trailing non-whitespace tokens on the same line (matching delta.c:165-168).
func (l *lexer) nextDeltaRecord() (string, bool, bool) {
	l.skipInlineSpace()
	if l.eof() {
		return "", false, false
	}
	if l.peek() == '\n' {
		l.consumeLineEnd()

		return "", false, false
	}

	var buf bytes.Buffer
	err := l.parseDeltaStr(&buf)
	if err != nil {
		l.consumeLineEnd()

		return "", false, true
	}

	// Trailing characters before newline make the delta record invalid
	// (tests/shunit2/tests.d/100_changes: test_changes_tailing_parts).
	if !l.eof() && l.peek() != '\n' && l.peek() != '\r' {
		l.consumeLineEnd()

		return "", false, true
	}
	l.consumeLineEnd()
	if buf.Len() == 0 {
		return "", false, false
	}

	return buf.String(), true, false
}

func (l *lexer) parseDeltaStr(buf *bytes.Buffer) error {
	for !l.eof() {
		c := l.peek()
		if isInlineSpace(c) || c == '\n' || c == ';' || c == '#' {
			return nil
		}
		switch c {
		case '\'':
			err := l.parseSingleQuote(buf)
			if err != nil {
				return err
			}
		case '"':
			err := l.parseDoubleQuote(buf)
			if err != nil {
				return err
			}
		case '\\':
			l.parseBackslash(buf)
		default:
			buf.WriteByte(c)
			l.pos++
		}
	}

	return nil
}

func parseDeltaTuple(arg string) (deltaTuple, bool) {
	if arg == "" {
		return deltaTuple{}, false
	}
	cmd, rest := splitDeltaPrefix(arg)
	lhs, val, hasVal := strings.Cut(rest, "=")

	pkgName, secName, optName, ok := splitDeltaPath(lhs)
	if !ok {
		return deltaTuple{}, false
	}
	if hasVal && !validateText(val) {
		return deltaTuple{}, false
	}
	tuple := deltaTuple{
		pkg:      pkgName,
		section:  secName,
		option:   optName,
		value:    val,
		cmd:      cmd,
		hasValue: hasVal,
	}
	if !validateDeltaTuple(tuple) {
		return deltaTuple{}, false
	}

	return tuple, true
}

func splitDeltaPrefix(arg string) (deltaCmd, string) {
	switch arg[0] {
	case '+':
		return deltaCmdAdd, arg[1:]
	case '-':
		return deltaCmdRemove, arg[1:]
	case '@':
		return deltaCmdRename, arg[1:]
	case '^':
		return deltaCmdReorder, arg[1:]
	case '|':
		return deltaCmdListAdd, arg[1:]
	case '~':
		return deltaCmdListDel, arg[1:]
	default:
		return deltaCmdChange, arg
	}
}

func splitDeltaPath(lhs string) (string, string, string, bool) {
	parts := strings.Split(lhs, ".")
	if len(parts) < 2 || len(parts) > 3 {
		return "", "", "", false
	}
	pkgName := parts[0]
	secName := parts[1]
	optName := ""
	if len(parts) == 3 {
		optName = parts[2]
		if !validateName(optName) {
			return "", "", "", false
		}
	}
	if !validatePackageName(pkgName) || !validateName(secName) {
		return "", "", "", false
	}

	return pkgName, secName, optName, true
}

func validateDeltaTuple(t deltaTuple) bool {
	if t.cmd != deltaCmdRemove && !t.hasValue {
		return false
	}
	switch t.cmd {
	case deltaCmdReorder:
		return t.option == "" && t.hasValue
	case deltaCmdRename:
		return t.hasValue && validateName(t.value)
	case deltaCmdListAdd, deltaCmdListDel:
		return t.option != ""
	case deltaCmdAdd, deltaCmdRemove, deltaCmdChange:
		return true
	default:
		return false
	}
}

func applyDeltaTuple(pkg *Package, t deltaTuple) bool {
	switch t.cmd {
	case deltaCmdAdd:
		return applyDeltaAdd(pkg, t)
	case deltaCmdChange:
		return applyDeltaChange(pkg, t)
	case deltaCmdRemove:
		return applyDeltaRemove(pkg, t)
	case deltaCmdRename:
		return applyDeltaRename(pkg, t)
	case deltaCmdReorder:
		pos, ok := parseDeltaUint(t.value)
		if !ok {
			return false
		}

		return pkg.ReorderSection(t.section, pos) == nil
	case deltaCmdListAdd:
		sec, ok := pkg.Section(t.section)
		if !ok {
			return false
		}

		return sec.AddList(t.option, t.value) == nil
	case deltaCmdListDel:
		sec, ok := pkg.Section(t.section)
		if !ok {
			return false
		}
		sec.DelList(t.option, t.value)

		return true
	default:
		return false
	}
}

func applyDeltaAdd(pkg *Package, t deltaTuple) bool {
	if t.option != "" {
		return applyDeltaOptionSet(pkg, t.section, t.option, t.value)
	}
	if t.value == "" {
		pkg.DeleteSection(t.section)

		return true
	}
	if !validateType(t.value) {
		return false
	}
	if existing, ok := pkg.Section(t.section); ok {
		existing.typ = t.value
		existing.anonymous = true
		existing.name = ""

		return true
	}
	pkg.appendSectionWithID(t.value, t.section, true)

	return true
}

func applyDeltaChange(pkg *Package, t deltaTuple) bool {
	if t.option != "" {
		return applyDeltaOptionSet(pkg, t.section, t.option, t.value)
	}
	if t.value == "" {
		pkg.DeleteSection(t.section)

		return true
	}
	if !validateType(t.value) {
		return false
	}
	if existing, ok := pkg.Section(t.section); ok {
		existing.typ = t.value

		return true
	}
	pkg.appendNamedSection(t.value, t.section)

	return true
}

func applyDeltaOptionSet(pkg *Package, secRef, key, val string) bool {
	sec, ok := pkg.Section(secRef)
	if !ok {
		return false
	}

	return sec.Set(key, val) == nil
}

func applyDeltaRemove(pkg *Package, t deltaTuple) bool {
	sec, ok := pkg.Section(t.section)
	if !ok {
		return false
	}
	if t.option == "" {
		return pkg.DeleteSection(t.section)
	}
	if t.hasValue && t.value != "" {
		idx, err := strconv.Atoi(t.value)
		if err != nil {
			return false
		}
		sec.deleteListIndex(t.option, idx)

		return true
	}
	sec.Delete(t.option)

	return true
}

func applyDeltaRename(pkg *Package, t deltaTuple) bool {
	if t.option == "" {
		return pkg.RenameSection(t.section, t.value) == nil
	}
	sec, ok := pkg.Section(t.section)
	if !ok {
		return false
	}

	return sec.RenameOption(t.option, t.value) == nil
}

// parseDeltaUint parses a base-10 position matching strtoul(..., 10) in delta.c.
func parseDeltaUint(s string) (int, bool) {
	v, err := strconv.Atoi(s)
	if err != nil {
		return 0, false
	}

	return v, true
}
