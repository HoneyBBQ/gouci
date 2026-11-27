package gouci

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

func TestReplayDeltaAllCommands(t *testing.T) {
	base, err := parsePackage([]byte(`
config interface 'lan'
	option proto 'static'
	option ipaddr '192.168.1.1'
	list dns '1.1.1.1'
	list dns '8.8.8.8'
	list dns '9.9.9.9'

config rule
	option name 'First-Rule'
`), "", "network", true)
	if err != nil {
		t.Fatalf("parsePackage: %v", err)
	}

	anonSec, _ := base.Section("@rule[0]")
	deltaData := buildAllCommandsDelta(anonSec.ID())

	var logBuf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logBuf, nil))
	if applied := replayDelta(base, []byte(deltaData), logger); applied != 16 {
		t.Fatalf("applied = %d, want 16 (logs: %s)", applied, logBuf.String())
	}

	assertAllCommandsResult(t, base, anonSec)
}

func buildAllCommandsDelta(anonID string) string {
	return strings.Join([]string{
		"network.lan.ipaddr='10.0.0.1'",
		"network.lan.note='line1",
		"line2'\\''end'",
		"|network.lan.dns='4.2.2.1'",
		"~network.lan.dns='8.8.8.8'",
		"-network.lan.dns='0'",
		"@network.lan.proto='protocol'",
		"network." + anonID + ".name='Updated-Anon'",
		"+network.cfg99abcd='route'",
		"network.cfg99abcd.target='10.20.0.0/16'",
		"@network.cfg99abcd='corp_route'",
		"^network.corp_route='0'",
		"network.wan='interface'",
		"network.wan.proto='dhcp'",
		"-network.wan.proto",
		"network.temp='interface'",
		"-network.temp",
	}, "\n") + "\n"
}

func assertAllCommandsResult(t *testing.T, base *Package, anonSec *Section) {
	t.Helper()
	first := base.Sections()[0]
	if first.Name() != "corp_route" || first.Anonymous() {
		t.Fatalf("section[0] = (%q, anon=%v), want named corp_route", first.Name(), first.Anonymous())
	}
	corp, _ := base.Section("corp_route")
	assertScalar(t, corp, "target", "10.20.0.0/16")

	lan, _ := base.Section("lan")
	assertScalar(t, lan, "ipaddr", "10.0.0.1")
	assertScalar(t, lan, "note", "line1\nline2'end")
	assertScalar(t, lan, "protocol", "static")
	if _, ok := lan.GetEntry("proto"); ok {
		t.Error("proto should have been renamed to protocol")
	}
	assertList(t, lan, "dns", []string{"9.9.9.9", "4.2.2.1"})
	assertScalar(t, anonSec, "name", "Updated-Anon")

	if _, ok := base.Section("wan"); !ok {
		t.Error("wan should exist")
	}
	if _, ok := base.Section("temp"); ok {
		t.Error("temp should have been deleted")
	}
}

func TestReplayDeltaSkipsMalformedLines(t *testing.T) {
	base, _ := NewPackage("network")
	_, _ = base.AddSection("interface", "lan")

	malformed := strings.Join([]string{
		"network.foo bar",
		"+network.foo bar=baz",
		"@network.foo",
		"^network.lan=notanint",
		"|network.lan='no_option'",
		"~network.lan='no_option'",
		"otherpkg.lan.ip='1.2.3.4'",
		"network.@interface[0].ip='1.2.3.4'",
		"network.lan.ok='valid'",
	}, "\n") + "\n"

	var logBuf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logBuf, nil))
	if applied := replayDelta(base, []byte(malformed), logger); applied != 1 {
		t.Fatalf("applied = %d, want 1", applied)
	}
	lan, _ := base.Section("lan")
	assertScalar(t, lan, "ok", "valid")
	if logBuf.Len() == 0 {
		t.Error("expected malformed delta warnings to be logged")
	}
}
