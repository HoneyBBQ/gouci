package gouci

import (
	"errors"
	"io"
	"strings"
	"testing"
)

type errReader struct{}

func (errReader) Read([]byte) (int, error) {
	return 0, io.ErrUnexpectedEOF
}

func TestParseLexicalMatrix(t *testing.T) {
	input := lexicalMatrixInput()
	pkg, err := Parse(strings.NewReader(input))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if pkg.Name() != "import-test" || len(pkg.Sections()) != 2 {
		t.Fatalf("Name=%q len(Sections)=%d", pkg.Name(), len(pkg.Sections()))
	}

	sec, ok := pkg.Section("section")
	if !ok {
		t.Fatal("section 'section' not found")
	}
	assertLexicalScalars(t, sec)
	assertLexicalEmptyAndLists(t, sec)

	rule, ok := pkg.Section("@rule[0]")
	if !ok || !rule.Anonymous() {
		t.Fatalf("@rule[0] missing or not anonymous: %v", ok)
	}
	assertScalar(t, rule, "name", "multi_stmt")
	assertList(t, rule, "proto", []string{"tcp", "udp"})
}

func lexicalMatrixInput() string {
	return strings.Join([]string{
		"package 'import-test'",
		"",
		"# leading comment",
		"   # indented comment",
		"config 'type' 'section'",
		"\toption 'opt' 'val'",
		"\toption empty_sq ''",
		"\toption empty_dq \"\"",
		"\toption empty_bare",
		"\tlist 'list_opt' 'val0'",
		"\tlist 'list_opt' 'val1'",
		"\tlist empty_list ''",
		"\toption mixed 'a'\"b\"c",
		"\toption unq_sq foo'bar'",
		"\toption single_quote 'foo'\\''bar'",
		"\toption inline_hash foo#not_part_of_value",
		"\toption quoted_hash '#ff0000'",
		"\toption mul_line_opt_sq \\''line 1",
		"line 2 \\",
		"line 3'\\'",
		"\toption mul_line_opt_dq \"\\\"Hello, \\",
		"World.\\'",
		"\"",
		"\toption crlf_cont \"alpha\\\r\nbeta\"",
		"c rule ; o name 'multi_stmt' ; l proto 'tcp'; l proto 'udp'",
		"p 'import-test'",
	}, "\n")
}

func assertLexicalScalars(t *testing.T, sec *Section) {
	t.Helper()
	assertScalar(t, sec, "opt", "val")
	assertScalar(t, sec, "mixed", "abc")
	assertScalar(t, sec, "unq_sq", "foobar")
	assertScalar(t, sec, "single_quote", "foo'bar")
	assertScalar(t, sec, "inline_hash", "foo")
	assertScalar(t, sec, "quoted_hash", "#ff0000")
	assertScalar(t, sec, "mul_line_opt_sq", "'line 1\nline 2 \\\nline 3'")
	assertScalar(t, sec, "mul_line_opt_dq", "\"Hello, World.'\n")
	assertScalar(t, sec, "crlf_cont", "alphabeta")
}

func assertLexicalEmptyAndLists(t *testing.T, sec *Section) {
	t.Helper()
	for _, key := range []string{"empty_sq", "empty_dq", "empty_bare"} {
		if _, found := sec.GetEntry(key); found {
			t.Errorf("option %q should be ignored per libuci semantics", key)
		}
	}
	assertList(t, sec, "list_opt", []string{"val0", "val1"})
	assertList(t, sec, "empty_list", []string{""})
}

func TestParseDuplicatesAndMerging(t *testing.T) {
	input := `
config interface 'lan'
	option proto 'static'
	option ipaddr '192.168.1.1'
	option ipaddr '192.168.2.1'
	option dns '1.1.1.1'
	list dns '8.8.8.8'
	list ports '80'
	option ports '443'

config interface 'lan'
	option netmask '255.255.255.0'
	option proto ''
`
	pkg, err := ParseBytes([]byte(input))
	if err != nil {
		t.Fatalf("ParseBytes() error = %v", err)
	}
	if len(pkg.Sections()) != 1 {
		t.Fatalf("expected duplicate named sections of same type to merge; got %d sections", len(pkg.Sections()))
	}
	lan, ok := pkg.Section("lan")
	if !ok {
		t.Fatal("lan section missing")
	}
	assertScalar(t, lan, "proto", "static")
	assertScalar(t, lan, "ipaddr", "192.168.2.1")
	assertList(t, lan, "dns", []string{"1.1.1.1", "8.8.8.8"})
	assertScalar(t, lan, "ports", "443")
	assertScalar(t, lan, "netmask", "255.255.255.0")

	entries := lan.Entries()
	wantKeys := []string{"proto", "ipaddr", "dns", "ports", "netmask"}
	if len(entries) != len(wantKeys) {
		t.Fatalf("len(entries) = %d, want %d", len(entries), len(wantKeys))
	}
	for i, key := range wantKeys {
		if entries[i].Key != key {
			t.Errorf("entries[%d].Key = %q, want %q", i, entries[i].Key, key)
		}
	}
}

func TestParseLongValueBeyond64KiB(t *testing.T) {
	big := strings.Repeat("a", 100*1024)
	input := "config blob 'b'\n\toption payload '" + big + "'\n"
	pkg, err := ParseBytes([]byte(input))
	if err != nil {
		t.Fatalf("ParseBytes() failed on 100KiB value: %v", err)
	}
	sec, _ := pkg.Section("b")
	assertScalar(t, sec, "payload", big)
}

func TestFormatQuotesSpecialSectionTypes(t *testing.T) {
	for _, typ := range []string{`"`, `'`, `#tag`, `a;b`, `a\b`} {
		pkg, err := NewPackage("special")
		if err != nil {
			t.Fatal(err)
		}
		_, err = pkg.AddSection(typ, "s1")
		if err != nil {
			t.Fatalf("AddSection(%q): %v", typ, err)
		}
		out, err := pkg.Format()
		if err != nil {
			t.Fatalf("Format(%q): %v", typ, err)
		}
		reparsed, err := ParseBytes(out)
		if err != nil {
			t.Fatalf("ParseBytes(Format(%q)): %v\nout:\n%s", typ, err, out)
		}
		sec, ok := reparsed.Section("s1")
		if !ok || sec.Type() != typ {
			t.Fatalf("round-trip type = %q, want %q", sec.Type(), typ)
		}
	}
}

func TestParseErrors(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{name: "nul_byte", input: "config rule\n\toption a 'x\x00y'\n"},
		{name: "invalid_command", input: "bogus foo\n"},
		{name: "package_without_name", input: "package\n"},
		{name: "package_invalid_name", input: "package '../etc'\n"},
		{name: "multiple_packages", input: "package a\npackage b\n"},
		{name: "config_without_type", input: "config\n"},
		{name: "config_invalid_type", input: "config 'bad type'\n"},
		{name: "config_invalid_name", input: "config rule 'bad-name'\n"},
		{name: "config_type_conflict", input: "config a 'dup'\nconfig b 'dup'\n"},
		{name: "option_before_config", input: "option a '1'\n"},
		{name: "option_without_name", input: "config rule\n\toption\n"},
		{name: "option_invalid_name", input: "config rule\n\toption bad-key '1'\n"},
		{name: "too_many_args", input: "config rule\n\toption a '1' extra\n"},
		{name: "unterminated_single_quote", input: "config alias' 'a'\n"},
		{name: "unterminated_double_quote", input: "config rule\n\toption a \"unterminated\n"},
		{name: "double_semicolon", input: "config rule;; option a '1'\n"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assertParseError(t, tc.input)
		})
	}

	_, err := Parse(errReader{})
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Errorf("Parse(errReader) = %v, want ErrUnexpectedEOF", err)
	}
}

func assertParseError(t *testing.T, input string) {
	t.Helper()
	_, err := ParseBytes([]byte(input))
	if err == nil {
		t.Fatal("expected parse error, got nil")
	}
	if !errors.Is(err, ErrInvalidSyntax) {
		t.Errorf("errors.Is(err, ErrInvalidSyntax) = false, err = %v", err)
	}
	parseErr, ok := errors.AsType[*ParseError](err)
	if !ok {
		t.Fatalf("expected *ParseError, got %T", err)
	}
	if parseErr.Line < 1 || parseErr.Reason == "" || parseErr.Error() == "" {
		t.Errorf("incomplete ParseError: %+v", parseErr)
	}
}

func assertScalar(t *testing.T, sec *Section, key, want string) {
	t.Helper()
	got, ok := sec.Get(key)
	if !ok {
		t.Fatalf("Get(%q) not found", key)
	}
	if got != want {
		t.Errorf("Get(%q) = %q, want %q", key, got, want)
	}
}

func assertList(t *testing.T, sec *Section, key string, want []string) {
	t.Helper()
	got, ok := sec.GetList(key)
	if !ok {
		t.Fatalf("GetList(%q) not found", key)
	}
	if len(got) != len(want) {
		t.Fatalf("GetList(%q) = %#v, want %#v", key, got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("GetList(%q)[%d] = %q, want %q", key, i, got[i], want[i])
		}
	}
}
