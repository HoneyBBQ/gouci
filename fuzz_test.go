package gouci

import (
	"bytes"
	"testing"
)

func FuzzParse(f *testing.F) {
	seeds := [][]byte{
		[]byte("config interface 'lan'\n\toption proto 'static'\n\tlist dns '1.1.1.1'\n"),
		[]byte("package 'net'\n\nc rule ; o name 'a'\\''b' ; l proto ''\n"),
		[]byte("config a\n\toption m \"line1\\\nline2\"\n\toption sq 'a\\b\nc'\n"),
		[]byte("config alias' 'a'\n"),
		[]byte("# comment only\n\n"),
		{},
	}
	for _, seed := range seeds {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		pkg, err := ParseBytes(data)
		if err != nil {
			return
		}
		out1, err := pkg.Format()
		if err != nil {
			t.Fatalf("Format() failed on parsed package: %v", err)
		}
		pkg2, err := ParseBytes(out1)
		if err != nil {
			t.Fatalf("ParseBytes(Format()) failed: %v\nformatted:\n%s", err, out1)
		}
		out2, err := pkg2.Format()
		if err != nil {
			t.Fatalf("Format() #2 failed: %v", err)
		}
		if !bytes.Equal(out1, out2) {
			t.Fatalf("Format -> Parse -> Format not idempotent:\n--- 1 ---\n%s\n--- 2 ---\n%s", out1, out2)
		}
	})
}

func FuzzRoundTrip(f *testing.F) {
	f.Add("interface", "lan", "ipaddr", "192.168.1.1", "dns", "1.1.1.1", "8.8.8.8")
	f.Add("wifi-iface", "", "ssid", "O'Reilly \"WiFi\" \\ Home\nLine2", "maclist", "", "aa:bb:cc")
	f.Add("rule", "r1", "extra", "a'b''c\\d", "proto", "tcp", "udp")

	f.Fuzz(func(t *testing.T, typ, name, optKey, optVal, listKey, listItem1, listItem2 string) {
		pkg, err := NewPackage("fuzz")
		if err != nil {
			t.Fatal(err)
		}
		sec, err := pkg.AddSection(typ, name)
		if err != nil {
			return
		}
		_ = sec.Set(optKey, optVal)
		_ = sec.AddList(listKey, listItem1)
		_ = sec.AddList(listKey, listItem2)

		out1, err := pkg.Format()
		if err != nil {
			t.Fatalf("Format() error: %v", err)
		}
		reparsed, err := ParseBytes(out1)
		if err != nil {
			t.Fatalf("ParseBytes(Format()) error: %v\ninput:\n%s", err, out1)
		}
		out2, err := reparsed.Format()
		if err != nil {
			t.Fatalf("Format() #2 error: %v", err)
		}
		if !bytes.Equal(out1, out2) {
			t.Fatalf("round-trip mismatch:\n--- 1 ---\n%s\n--- 2 ---\n%s", out1, out2)
		}
		if changes := Diff(pkg, reparsed); len(changes) != 0 {
			t.Fatalf("Diff(pkg, reparsed) = %+v, want empty", changes)
		}
	})
}
