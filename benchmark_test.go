package gouci

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

var benchSample = []byte(`
config defaults
	option syn_flood '1'
	option input 'REJECT'
	option output 'ACCEPT'
	option forward 'REJECT'

config zone
	option name 'lan'
	list network 'lan'
	option input 'ACCEPT'
	option output 'ACCEPT'
	option forward 'ACCEPT'

config zone
	option name 'wan'
	list network 'wan'
	list network 'wan6'
	option input 'REJECT'
	option output 'ACCEPT'
	option forward 'REJECT'
	option masq '1'

config rule
	option name 'Allow-Ping'
	option src 'wan'
	option proto 'icmp'
	list icmp_type 'echo-request'
	option family 'ipv4'
	option target 'ACCEPT'
`)

func BenchmarkParseBytes(b *testing.B) {
	b.ReportAllocs()
	for range b.N {
		_, err := ParseBytes(benchSample)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkFormat(b *testing.B) {
	pkg, err := ParseBytes(benchSample)
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		_, err = pkg.Format()
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkDiff(b *testing.B) {
	before, err := ParseBytes(benchSample)
	if err != nil {
		b.Fatal(err)
	}
	after := before.Clone()
	rule, _ := after.Section("@rule[0]")
	_ = rule.Set("target", "DROP")

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		_ = Diff(before, after)
	}
}

func BenchmarkStoreUpdateNoOp(b *testing.B) {
	dir := b.TempDir()
	err := os.WriteFile(filepath.Join(dir, "firewall"), benchSample, 0o600)
	if err != nil {
		b.Fatal(err)
	}
	store := NewStore(WithConfDir(dir), WithConf2Dir(""), WithDeltaDir(""))

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		res, updateErr := store.Update(context.Background(), "firewall", func(*Package) error { return nil })
		if updateErr != nil || res.Changed {
			b.Fatalf("res=%+v err=%v", res, updateErr)
		}
	}
}
