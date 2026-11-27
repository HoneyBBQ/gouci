package gouci

import (
	"bytes"
	"io"
)

type lexer struct {
	file      string
	data      []byte
	pos       int
	line      int
	lineStart int
}

// Parse parses a single UCI configuration package stream using libuci's strict
// lexical and grammar rules.
func Parse(r io.Reader) (*Package, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}

	return parsePackage(data, "", "", false)
}

// ParseBytes parses a single UCI configuration package from data using libuci's
// strict lexical and grammar rules.
func ParseBytes(data []byte) (*Package, error) {
	return parsePackage(data, "", "", false)
}

func parsePackage(data []byte, file, forcedName string, single bool) (*Package, error) {
	if idx := bytes.IndexByte(data, 0); idx >= 0 {
		line, col := offsetToLineCol(data, idx)

		return nil, &ParseError{File: file, Line: line, Byte: col, Reason: "unexpected NUL byte"}
	}
	lex := &lexer{
		file: file,
		data: data,
		line: 1,
	}
	pkg := &Package{name: forcedName}
	var curSec *Section
	for !lex.eof() {
		var err error
		curSec, err = lex.parseLine(pkg, curSec, single)
		if err != nil {
			return nil, err
		}
	}

	return pkg, nil
}

func offsetToLineCol(data []byte, idx int) (int, int) {
	line := 1
	lineStart := 0
	for i := range idx {
		if data[i] == '\n' {
			line++
			lineStart = i + 1
		}
	}

	return line, idx - lineStart
}

func (l *lexer) eof() bool {
	return l.pos >= len(l.data)
}

func (l *lexer) peek() byte {
	return l.data[l.pos]
}

func (l *lexer) errf(reason string) error {
	return &ParseError{
		File:   l.file,
		Line:   l.line,
		Byte:   l.pos - l.lineStart,
		Reason: reason,
	}
}

func (l *lexer) skipInlineSpace() {
	for !l.eof() && isInlineSpace(l.peek()) {
		l.pos++
	}
}

func (l *lexer) consumeLineEnd() {
	for !l.eof() {
		c := l.peek()
		l.pos++
		if c == '\n' {
			l.line++
			l.lineStart = l.pos

			return
		}
	}
}

func (l *lexer) parseLine(pkg *Package, curSec *Section, single bool) (*Section, error) {
	for {
		l.skipInlineSpace()
		if l.eof() {
			return curSec, nil
		}
		switch l.peek() {
		case '\n':
			l.consumeLineEnd()

			return curSec, nil
		case '#':
			l.consumeLineEnd()

			return curSec, nil
		}

		more, err := l.parseStatement(pkg, &curSec, single)
		if err != nil {
			return nil, err
		}
		if !more {
			return curSec, nil
		}
	}
}

func (l *lexer) parseStatement(pkg *Package, curSec **Section, single bool) (bool, error) {
	cmd := l.readCommandWord()
	switch cmd {
	case "p", "package":
		return l.parsePackageStmt(pkg, single)
	case "c", "config":
		sec, more, err := l.parseConfigStmt(pkg)
		if err != nil {
			return false, err
		}
		*curSec = sec

		return more, nil
	case "o", "option":
		return l.parseOptionStmt(*curSec, false)
	case "l", "list":
		return l.parseOptionStmt(*curSec, true)
	default:
		return false, l.errf("invalid command")
	}
}

// readCommandWord reads the leading keyword separated by space/tab (matching
// strtok(..., " \t") in file.c).
func (l *lexer) readCommandWord() string {
	start := l.pos
	for !l.eof() {
		c := l.peek()
		if c == ' ' || c == '\t' || c == '\r' || c == '\n' {
			break
		}
		l.pos++
	}

	return string(l.data[start:l.pos])
}

func (l *lexer) parsePackageStmt(pkg *Package, single bool) (bool, error) {
	name, ok, err := l.nextArg()
	if err != nil {
		return false, err
	}
	if !ok || name == "" {
		return false, l.errf("package without name")
	}
	if !validatePackageName(name) {
		return false, l.errf("invalid character in package name")
	}
	more, err := l.assertEOL()
	if err != nil {
		return false, err
	}
	if !single {
		if pkg.name != "" && pkg.name != name {
			return false, l.errf("multiple packages in single-package stream")
		}
		pkg.name = name
	}

	return more, nil
}

func (l *lexer) parseConfigStmt(pkg *Package) (*Section, bool, error) {
	typ, ok, err := l.nextArg()
	if err != nil {
		return nil, false, err
	}
	if !ok || typ == "" {
		return nil, false, l.errf("insufficient arguments")
	}
	if !validateType(typ) {
		return nil, false, l.errf("invalid character in type field")
	}

	name, _, err := l.nextArg()
	if err != nil {
		return nil, false, err
	}
	if name != "" && !validateName(name) {
		return nil, false, l.errf("invalid character in name field")
	}
	more, err := l.assertEOL()
	if err != nil {
		return nil, false, err
	}
	sec, err := l.applyConfigSection(pkg, typ, name)

	return sec, more, err
}

func (l *lexer) applyConfigSection(pkg *Package, typ, name string) (*Section, error) {
	if name == "" {
		return pkg.appendAnonSection(typ), nil
	}
	if existing := pkg.findNamedSection(name); existing != nil {
		if existing.typ != typ {
			return nil, l.errf("section of different type overwrites prior section with same name")
		}

		return existing, nil
	}

	return pkg.appendNamedSection(typ, name), nil
}

func (l *lexer) parseOptionStmt(curSec *Section, isList bool) (bool, error) {
	if curSec == nil {
		return false, l.errf("option/list command found before the first section")
	}
	key, ok, err := l.nextArg()
	if err != nil {
		return false, err
	}
	if !ok || key == "" {
		return false, l.errf("insufficient arguments")
	}
	if !validateName(key) {
		return false, l.errf("invalid character in name field")
	}

	val, _, err := l.nextArg()
	if err != nil {
		return false, err
	}
	more, err := l.assertEOL()
	if err != nil {
		return false, err
	}
	if isList {
		curSec.addListUnchecked(key, val)
	} else if val != "" {
		curSec.setScalarUnchecked(key, val)
	}

	return more, nil
}

// assertEOL verifies no extra arguments follow a statement, consuming a ';'
// separator (returning true) or advancing past the end of line (returning false).
func (l *lexer) assertEOL() (bool, error) {
	l.skipInlineSpace()
	if l.eof() {
		return false, nil
	}
	switch l.peek() {
	case ';':
		l.pos++

		return true, nil
	case '\n', '#':
		l.consumeLineEnd()

		return false, nil
	default:
		return false, l.errf("too many arguments")
	}
}

// nextArg parses the next UCI argument token (possibly mixed-quoted and
// multiline), stopping before unquoted whitespace, ';', '#', or EOL.
func (l *lexer) nextArg() (string, bool, error) {
	l.skipInlineSpace()
	if l.eof() {
		return "", false, nil
	}
	switch l.peek() {
	case '\n', ';', '#':
		return "", false, nil
	}
	var buf bytes.Buffer
	err := l.parseStr(&buf)
	if err != nil {
		return "", false, err
	}

	return buf.String(), true, nil
}

func (l *lexer) parseStr(buf *bytes.Buffer) error {
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

func (l *lexer) parseSingleQuote(buf *bytes.Buffer) error {
	l.pos++ // skip opening '\''
	for !l.eof() {
		c := l.peek()
		l.pos++
		if c == '\'' {
			return nil
		}
		if c == '\n' {
			l.line++
			l.lineStart = l.pos
		}
		buf.WriteByte(c)
	}

	return l.errf("EOF with unterminated '")
}

func (l *lexer) parseDoubleQuote(buf *bytes.Buffer) error {
	l.pos++ // skip opening '"'
	for !l.eof() {
		c := l.peek()
		switch c {
		case '"':
			l.pos++

			return nil
		case '\\':
			l.parseBackslash(buf)
		default:
			l.pos++
			if c == '\n' {
				l.line++
				l.lineStart = l.pos
			}
			buf.WriteByte(c)
		}
	}

	return l.errf(`EOF with unterminated "`)
}

// parseBackslash matches libuci parse_backslash: strips '\' + '\r'?\n line
// continuations and emits any other escaped byte literally.
func (l *lexer) parseBackslash(buf *bytes.Buffer) {
	l.pos++ // skip '\\'
	if l.eof() {
		return
	}
	if l.peek() == '\n' {
		l.pos++
		l.line++
		l.lineStart = l.pos

		return
	}
	if l.peek() == '\r' && l.pos+1 < len(l.data) && l.data[l.pos+1] == '\n' {
		l.pos += 2
		l.line++
		l.lineStart = l.pos

		return
	}
	buf.WriteByte(l.peek())
	l.pos++
}

func isInlineSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\r' || c == '\v' || c == '\f'
}
