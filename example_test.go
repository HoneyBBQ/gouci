package gouci_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/honeybbq/gouci"
)

func ExampleParseBytes() {
	raw := []byte(`
config interface 'lan'
	option proto 'static'
	option ipaddr '192.168.1.1'
	list dns '1.1.1.1'
`)
	pkg, err := gouci.ParseBytes(raw)
	if err != nil {
		panic(err)
	}
	lan, _ := pkg.Section("lan")
	ip, _ := lan.Get("ipaddr")
	dns, _ := lan.GetList("dns")
	fmt.Printf("lan=%s dns=%v\n", ip, dns)
	// Output:
	// lan=192.168.1.1 dns=[1.1.1.1]
}

func ExampleStore_Update() {
	dir, err := os.MkdirTemp("", "gouci-example-*")
	if err != nil {
		panic(err)
	}
	defer func() { _ = os.RemoveAll(dir) }()

	initial := "config interface 'lan'\n\toption ipaddr '192.168.1.1'\n"
	err = os.WriteFile(filepath.Join(dir, "network"), []byte(initial), 0o600)
	if err != nil {
		panic(err)
	}

	store := gouci.NewStore(
		gouci.WithConfDir(dir),
		gouci.WithConf2Dir(""),
		gouci.WithDeltaDir(""),
	)
	res, err := store.Update(context.Background(), "network", func(pkg *gouci.Package) error {
		lan, ok := pkg.Section("lan")
		if !ok {
			return gouci.ErrNotFound
		}

		return lan.Set("ipaddr", "192.168.10.1")
	})
	if err != nil {
		panic(err)
	}
	fmt.Printf("changed=%v count=%d\n", res.Changed, len(res.Changes))
	// Output:
	// changed=true count=1
}
