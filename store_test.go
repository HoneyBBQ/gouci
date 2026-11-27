package gouci

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"sync"
	"testing"
	"time"
)

var errTestAbort = errors.New("abort test transaction")

func newTestStore(t *testing.T, opts ...Option) (*Store, string, string, string) {
	t.Helper()
	root := t.TempDir()
	confDir := filepath.Join(root, "config")
	conf2Dir := filepath.Join(root, "conf2")
	deltaDir := filepath.Join(root, "delta")
	for _, dir := range []string{confDir, conf2Dir, deltaDir} {
		err := os.MkdirAll(dir, 0o700)
		if err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}
	allOpts := make([]Option, 0, 3+len(opts))
	allOpts = append(allOpts, WithConfDir(confDir), WithConf2Dir(conf2Dir), WithDeltaDir(deltaDir))
	allOpts = append(allOpts, opts...)

	return NewStore(allOpts...), confDir, conf2Dir, deltaDir
}

func TestStorePackagesFiltering(t *testing.T) {
	store, confDir, _, _ := newTestStore(t)

	writeFile(t, filepath.Join(confDir, "network"), "config interface 'lan'\n", 0o600)
	writeFile(t, filepath.Join(confDir, "ddns-scripts"), "config service 'a'\n", 0o600)
	writeFile(t, filepath.Join(confDir, ".hidden"), "config x\n", 0o600)
	writeFile(t, filepath.Join(confDir, "network.bak"), "config x\n", 0o600)
	mustNilErr(t, os.MkdirAll(filepath.Join(confDir, "subdir"), 0o700))

	realTarget := filepath.Join(t.TempDir(), "real_wireless")
	writeFile(t, realTarget, "config wifi-device 'radio0'\n", 0o600)
	mustNilErr(t, os.Symlink(realTarget, filepath.Join(confDir, "wireless")))

	pkgs, err := store.Packages()
	mustNilErr(t, err)
	want := []string{"ddns-scripts", "network", "wireless"}
	if !slices.Equal(pkgs, want) {
		t.Fatalf("Packages() = %v, want %v", pkgs, want)
	}
}

func TestStoreUpdateNoOpPreservesMtimeAndInode(t *testing.T) {
	store, confDir, _, _ := newTestStore(t)
	target := filepath.Join(confDir, "network")
	initial := "# hand-written comment\nconfig interface \"lan\"\n\toption proto \"static\"\n"
	writeFile(t, target, initial, 0o640)

	oldTime := time.Now().Add(-10 * time.Minute)
	mustNilErr(t, os.Chtimes(target, oldTime, oldTime))
	beforeStat, err := os.Stat(target)
	mustNilErr(t, err)

	res, err := store.Update(context.Background(), "network", func(pkg *Package) error {
		lan, _ := pkg.Section("lan")

		return lan.Set("proto", "static")
	})
	mustNilErr(t, err)
	if res.Changed || len(res.Changes) != 0 {
		t.Fatalf("expected Changed=false, got %+v", res)
	}
	assertSameMtimeAndInode(t, target, beforeStat)
}

func TestStoreUpdatePreservesModeAndCleansTempFiles(t *testing.T) {
	store, confDir, _, _ := newTestStore(t)
	target := filepath.Join(confDir, "network")
	writeFile(t, target, "config interface 'lan'\n\toption proto 'static'\n", 0o640)

	res, err := store.Update(context.Background(), "network", func(pkg *Package) error {
		lan, _ := pkg.Section("lan")

		return lan.Set("proto", "dhcp")
	})
	mustNilErr(t, err)
	if !res.Changed || len(res.Changes) != 1 {
		t.Fatalf("expected 1 change, got %+v", res)
	}
	st, err := os.Stat(target)
	mustNilErr(t, err)
	if st.Mode().Perm() != 0o640 {
		t.Errorf("mode = %o, want 0640 preserved", st.Mode().Perm())
	}
	entries, err := os.ReadDir(confDir)
	mustNilErr(t, err)
	if len(entries) != 1 {
		t.Errorf("unexpected leftover files in confDir: %d", len(entries))
	}
}

func TestStoreNewPackageNoOrphanOnErrorOrNoOp(t *testing.T) {
	store, confDir, _, _ := newTestStore(t, WithFileMode(0o600))
	target := filepath.Join(confDir, "fresh")

	_, err := store.Update(context.Background(), "fresh", func(pkg *Package) error {
		_, _ = pkg.AddSection("system", "main")

		return errTestAbort
	})
	assertErrIs(t, err, errTestAbort)
	assertNotExists(t, target)

	res, err := store.Update(context.Background(), "fresh", func(*Package) error { return nil })
	mustNilErr(t, err)
	if res.Changed {
		t.Fatalf("expected Changed=false, got %+v", res)
	}
	assertNotExists(t, target)

	res, err = store.Update(context.Background(), "fresh", func(pkg *Package) error {
		s, addErr := pkg.AddSection("system", "main")
		if addErr != nil {
			return addErr
		}

		return s.Set("hostname", "router")
	})
	mustNilErr(t, err)
	if !res.Changed {
		t.Fatal("expected Changed=true when creating package")
	}
	st, err := os.Stat(target)
	mustNilErr(t, err)
	if st.Mode().Perm() != 0o600 {
		t.Errorf("new file mode = %o, want 0600", st.Mode().Perm())
	}
}

func TestStoreConf2DirAndSymlink(t *testing.T) {
	store, confDir, conf2Dir, _ := newTestStore(t)
	writeFile(t, filepath.Join(confDir, "system"), "config system\n\toption hostname 'flash'\n", 0o600)
	writeFile(t, filepath.Join(conf2Dir, "system"), "config system\n\toption hostname 'runtime'\n", 0o600)

	pkg, err := store.Load("system")
	mustNilErr(t, err)
	sys, _ := pkg.Section("@system[0]")
	assertScalar(t, sys, "hostname", "runtime")

	_, err = store.Update(context.Background(), "system", func(p *Package) error {
		s, _ := p.Section("@system[0]")

		return s.Set("hostname", "runtime2")
	})
	mustNilErr(t, err)

	flashRaw, _ := os.ReadFile(filepath.Join(confDir, "system"))
	if !bytes.Contains(flashRaw, []byte("'flash'")) {
		t.Errorf("ConfDir file should be untouched, got:\n%s", flashRaw)
	}
	runtimeRaw, _ := os.ReadFile(filepath.Join(conf2Dir, "system"))
	if !bytes.Contains(runtimeRaw, []byte("'runtime2'")) {
		t.Errorf("Conf2Dir file not updated, got:\n%s", runtimeRaw)
	}

	realPath := filepath.Join(t.TempDir(), "real_dhcp")
	writeFile(t, realPath, "config dnsmasq\n\toption domain 'lan'\n", 0o600)
	linkPath := filepath.Join(confDir, "dhcp")
	mustNilErr(t, os.Symlink(realPath, linkPath))

	_, err = store.Update(context.Background(), "dhcp", func(p *Package) error {
		s, _ := p.Section("@dnsmasq[0]")

		return s.Set("domain", "home.arpa")
	})
	mustNilErr(t, err)
	linfo, err := os.Lstat(linkPath)
	mustNilErr(t, err)
	if linfo.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("expected %s to remain a symlink", linkPath)
	}
	gotReal, _ := os.ReadFile(realPath)
	if !bytes.Contains(gotReal, []byte("'home.arpa'")) {
		t.Errorf("symlink target not updated: %s", gotReal)
	}
}

func TestStoreDeltaRejectAndMerge(t *testing.T) {
	storeMerge, confDir, _, deltaDir := newTestStore(t, WithDeltaPolicy(DeltaMerge))
	storeReject := NewStore(
		WithConfDir(confDir),
		WithConf2Dir(""),
		WithDeltaDir(deltaDir),
		WithDeltaPolicy(DeltaReject),
	)

	writeFile(t, filepath.Join(confDir, "network"), "config interface 'lan'\n\toption ipaddr '192.168.1.1'\n", 0o600)
	writeFile(t, filepath.Join(deltaDir, "network"), "", 0o600)

	_, err := storeReject.Load("network")
	mustNilErr(t, err)

	writeFile(t, filepath.Join(deltaDir, "network"), "network.lan.ipaddr='10.10.0.1'\n", 0o600)
	_, err = storeReject.Load("network")
	assertErrIs(t, err, ErrDirtyDelta)
	_, err = storeReject.Update(context.Background(), "network", func(*Package) error { return nil })
	assertErrIs(t, err, ErrDirtyDelta)

	loaded, err := storeMerge.Load("network")
	mustNilErr(t, err)
	lan, _ := loaded.Section("lan")
	assertScalar(t, lan, "ipaddr", "10.10.0.1")

	res, err := storeMerge.Update(context.Background(), "network", func(p *Package) error {
		s, _ := p.Section("lan")

		return s.Set("netmask", "255.255.255.0")
	})
	mustNilErr(t, err)
	if !res.Changed {
		t.Fatal("expected Changed=true")
	}
	assertDeltaTruncated(t, filepath.Join(deltaDir, "network"))
}

func TestStoreDeltaNetNoOpSkipsConfigWrite(t *testing.T) {
	storeMerge, confDir, _, deltaDir := newTestStore(t, WithDeltaPolicy(DeltaMerge))
	confPath := filepath.Join(confDir, "network")
	deltaPath := filepath.Join(deltaDir, "network")
	writeFile(t, confPath, "config interface 'lan'\n\toption ipaddr '10.10.0.1'\n", 0o600)
	writeFile(t, deltaPath, "network.lan.ipaddr='10.10.0.1'\n", 0o600)

	beforeStat, err := os.Stat(confPath)
	mustNilErr(t, err)

	res, err := storeMerge.Update(context.Background(), "network", func(*Package) error { return nil })
	mustNilErr(t, err)
	if res.Changed {
		t.Fatalf("expected Changed=false, got %+v", res)
	}
	afterStat, err := os.Stat(confPath)
	mustNilErr(t, err)
	if !os.SameFile(beforeStat, afterStat) {
		t.Error("net no-op delta should not rewrite config file")
	}
	assertDeltaTruncated(t, deltaPath)
}

func TestStoreExportRestoreAndReadOnly(t *testing.T) {
	store, confDir, _, deltaDir := newTestStore(t)
	orig := "# keep comment\nconfig interface 'lan'\n\toption ipaddr '192.168.1.1'\n"
	writeFile(t, filepath.Join(confDir, "network"), orig, 0o600)

	snap, err := store.ExportRaw("network")
	mustNilErr(t, err)
	if string(snap) != orig {
		t.Fatalf("ExportRaw mismatch: %q", snap)
	}

	_, err = store.Update(context.Background(), "network", func(p *Package) error {
		s, _ := p.Section("lan")

		return s.Set("ipaddr", "10.99.0.1")
	})
	mustNilErr(t, err)

	writeFile(t, filepath.Join(deltaDir, "network"), "network.lan.ipaddr='10.88.0.1'\n", 0o600)
	mustNilErr(t, store.RestoreRaw(context.Background(), "network", snap))
	got, _ := os.ReadFile(filepath.Join(confDir, "network"))
	if string(got) != orig {
		t.Fatalf("restored content mismatch: %q", got)
	}
	assertDeltaTruncated(t, filepath.Join(deltaDir, "network"))

	err = store.RestoreRaw(context.Background(), "network", []byte("invalid syntax\n"))
	assertErrIs(t, err, ErrInvalidSyntax)
	assertReadOnlyAndMissingErrors(t, confDir, snap)
}

func assertReadOnlyAndMissingErrors(t *testing.T, confDir string, snap []byte) {
	t.Helper()
	roStore := NewStore(WithConfDir(confDir), WithReadOnly(true))
	_, err := roStore.Update(context.Background(), "network", func(*Package) error { return nil })
	assertErrIs(t, err, ErrReadOnly)
	assertErrIs(t, roStore.RestoreRaw(context.Background(), "network", snap), ErrReadOnly)
	_, err = roStore.Load("missing")
	assertErrIs(t, err, ErrNotFound)
	_, err = roStore.ExportRaw("missing")
	assertErrIs(t, err, ErrNotFound)
	_, err = roStore.Load("../bad")
	assertErrIs(t, err, ErrInvalidName)
}

func TestStoreConcurrentUpdatesZeroLostUpdates(t *testing.T) {
	store, confDir, _, _ := newTestStore(t)
	writeFile(t, filepath.Join(confDir, "batch"), "config bag 'items'\n", 0o600)

	const workers = 16
	var wg sync.WaitGroup
	errCh := make(chan error, workers)
	for i := range workers {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			_, err := store.Update(context.Background(), "batch", func(p *Package) error {
				bag, ok := p.Section("items")
				if !ok {
					return ErrNotFound
				}

				return bag.AddList("entry", fmt.Sprintf("item-%02d", id))
			})
			if err != nil {
				errCh <- err
			}
		}(i)
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Fatalf("concurrent Update failed: %v", err)
	}

	pkg, err := store.Load("batch")
	mustNilErr(t, err)
	bag, _ := pkg.Section("items")
	items, ok := bag.GetList("entry")
	if !ok || len(items) != workers {
		t.Fatalf("got %d items (%v), want %d", len(items), items, workers)
	}
}

func TestStoreFlockContextCancellation(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("flock test requires unix")
	}
	store, confDir, _, _ := newTestStore(t)
	target := filepath.Join(confDir, "locked")
	writeFile(t, target, "config item 'a'\n\toption v '1'\n", 0o600)

	holder, err := os.OpenFile(target, os.O_RDWR, 0)
	mustNilErr(t, err)
	err = flockFile(context.Background(), holder, true)
	if err != nil {
		_ = holder.Close()
		t.Fatal(err)
	}
	defer unlockAndClose(holder)

	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()
	_, err = store.Update(ctx, "locked", func(p *Package) error {
		s, _ := p.Section("a")

		return s.Set("v", "2")
	})
	assertErrIs(t, err, context.DeadlineExceeded)
}

func assertSameMtimeAndInode(t *testing.T, target string, before os.FileInfo) {
	t.Helper()
	after, err := os.Stat(target)
	mustNilErr(t, err)
	if !os.SameFile(before, after) {
		t.Error("no-op Update replaced inode; expected zero disk writes")
	}
	if !before.ModTime().Equal(after.ModTime()) {
		t.Errorf("no-op Update modified mtime: %v -> %v", before.ModTime(), after.ModTime())
	}
}

func assertNotExists(t *testing.T, path string) {
	t.Helper()
	_, err := os.Stat(path)
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected %s not to exist, stat err = %v", path, err)
	}
}

func assertDeltaTruncated(t *testing.T, deltaPath string) {
	t.Helper()
	data, err := os.ReadFile(deltaPath)
	mustNilErr(t, err)
	if len(data) != 0 {
		t.Errorf("delta file should be truncated to 0 bytes, got %q", data)
	}
}

func writeFile(t *testing.T, path, content string, mode os.FileMode) {
	t.Helper()
	err := os.WriteFile(path, []byte(content), mode)
	if err != nil {
		t.Fatalf("WriteFile(%s): %v", path, err)
	}
	err = os.Chmod(path, mode)
	if err != nil {
		t.Fatalf("Chmod(%s): %v", path, err)
	}
}
