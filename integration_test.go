//go:build integration

package gouci_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/honeybbq/gouci"
)

type uciLab struct {
	t            *testing.T
	store        *gouci.Store
	container    string
	hostBase     string
	containerDir string
	confHost     string
	conf2Host    string
	deltaHost    string
	confCont     string
	conf2Cont    string
	deltaCont    string
	hasConf2Dir  bool
}

func newUCILab(t *testing.T) *uciLab {
	t.Helper()
	container := os.Getenv("UCI_TEST_CONTAINER")
	hostDir := os.Getenv("UCI_TEST_HOST_DIR")
	contDir := os.Getenv("UCI_TEST_CONTAINER_DIR")
	if container == "" || hostDir == "" || contDir == "" {
		t.Skip("UCI_TEST_CONTAINER / UCI_TEST_HOST_DIR / UCI_TEST_CONTAINER_DIR not set")
	}

	// Must live under hostDir (bind-mounted into the OpenWrt container), so t.TempDir() cannot be used.
	//nolint:usetesting // must be inside the Docker bind-mounted lab directory
	sub, err := os.MkdirTemp(hostDir, "case-*")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	//nolint:gosec // G703: sub is created by os.MkdirTemp under UCI_TEST_HOST_DIR
	t.Cleanup(func() { _ = os.RemoveAll(sub) })

	rel, err := filepath.Rel(hostDir, sub)
	if err != nil {
		t.Fatalf("Rel: %v", err)
	}
	contSub := path.Join(contDir, filepath.ToSlash(rel))

	lab := &uciLab{
		t:            t,
		container:    container,
		hostBase:     sub,
		containerDir: contSub,
		confHost:     filepath.Join(sub, "config"),
		conf2Host:    filepath.Join(sub, "conf2"),
		deltaHost:    filepath.Join(sub, "delta"),
		confCont:     path.Join(contSub, "config"),
		conf2Cont:    path.Join(contSub, "conf2"),
		deltaCont:    path.Join(contSub, "delta"),
	}
	for _, dir := range []string{lab.confHost, lab.conf2Host, lab.deltaHost} {
		//nolint:gosec // G703: dir is a fixed child of sub created by os.MkdirTemp
		err = os.MkdirAll(dir, 0o700)
		if err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}
	lab.hasConf2Dir = lab.probeConf2Dir()
	lab.store = gouci.NewStore(
		gouci.WithConfDir(lab.confHost),
		gouci.WithConf2Dir(lab.conf2Host),
		gouci.WithDeltaDir(lab.deltaHost),
	)

	return lab
}

func (l *uciLab) probeConf2Dir() bool {
	l.t.Helper()
	//nolint:gosec // G204: integration test inspects /sbin/uci help inside the ephemeral OpenWrt container
	cmd := exec.CommandContext(context.Background(), "docker", "exec", l.container, "/sbin/uci", "-h")
	out, _ := cmd.CombinedOutput()

	return strings.Contains(string(out), "-C <path>")
}

func (l *uciLab) runUCI(stdin string, args ...string) string {
	l.t.Helper()
	fullArgs := []string{
		"exec", "-i", l.container, "/sbin/uci",
		"-c", l.confCont,
	}
	if l.hasConf2Dir {
		fullArgs = append(fullArgs, "-C", l.conf2Cont)
	}
	fullArgs = append(fullArgs, "-t", l.deltaCont)
	fullArgs = append(fullArgs, args...)
	//nolint:gosec // G204: integration test runs /sbin/uci inside the ephemeral OpenWrt container
	cmd := exec.CommandContext(context.Background(), "docker", fullArgs...)
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		l.t.Fatalf("uci %v failed: %v\nstdout:\n%s\nstderr:\n%s", args, err, stdout.String(), stderr.String())
	}

	return stdout.String()
}

func TestIntegrationGoToCParity(t *testing.T) {
	lab := newUCILab(t)
	seedGoldenPackage(t, lab.store)

	pkg, err := lab.store.Load("golden")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	anonRule, ok := pkg.Section("@rule[1]")
	if !ok {
		t.Fatal("@rule[1] not found")
	}

	assertGoldenShowX(t, lab.runUCI("", "-X", "show", "golden"), anonRule.ID())

	exported := lab.runUCI("", "-N", "export", "golden")
	reparsed, err := gouci.ParseBytes([]byte(exported))
	if err != nil {
		t.Fatalf("ParseBytes(uci -N export): %v\n%s", err, exported)
	}
	if changes := gouci.Diff(pkg, reparsed); len(changes) != 0 {
		t.Fatalf("Diff(gouci, uci -N export) = %+v, want empty", changes)
	}
}

func seedGoldenPackage(t *testing.T, store *gouci.Store) {
	t.Helper()
	const utf8SSID = "\u8def\u7531\u5668WiFi"
	_, err := store.Update(context.Background(), "golden", func(pkg *gouci.Package) error {
		lan, addErr := pkg.AddSection("interface", "lan")
		if addErr != nil {
			return addErr
		}
		_ = lan.Set("proto", "static")
		_ = lan.Set("password", `p@ss'w''rd\slash"dq`)
		_ = lan.Set("multiline", "line 1\nline 2 'quoted' \\\nline 3")
		_ = lan.Set("color", "#ff0000")
		_ = lan.Set("utf8_ssid", utf8SSID)
		_ = lan.Set("cleared", "")
		_ = lan.AddList("device", "vane0")
		_ = lan.AddList("extra", "")
		_ = lan.AddList("extra", "two words\nline2 'q'")

		r0, addErr := pkg.AddSection("rule", "named_rule")
		if addErr != nil {
			return addErr
		}
		_ = r0.Set("target", "ACCEPT")

		r1, addErr := pkg.AddSection("rule", "")
		if addErr != nil {
			return addErr
		}
		_ = r1.Set("name", "Allow-Ping")
		_ = r1.AddList("icmp_type", "echo-request")

		return nil
	})
	if err != nil {
		t.Fatalf("Update golden: %v", err)
	}
}

func assertGoldenShowX(t *testing.T, showX, anonID string) {
	t.Helper()
	const utf8SSID = "\u8def\u7531\u5668WiFi"
	wantLines := []string{
		"golden.lan=interface",
		"golden.lan.proto='static'",
		"golden.lan.password='p@ss'\\''w'\\'''\\''rd\\slash\"dq'",
		"golden.lan.color='#ff0000'",
		"golden.lan.utf8_ssid='" + utf8SSID + "'",
		"golden.lan.device='vane0'",
		"golden.named_rule=rule",
		"golden.named_rule.target='ACCEPT'",
		"golden." + anonID + "=rule",
		"golden." + anonID + ".name='Allow-Ping'",
		"golden." + anonID + ".icmp_type='echo-request'",
	}
	for _, want := range wantLines {
		if !strings.Contains(showX, want) {
			t.Errorf("uci -X show missing %q\nfull output:\n%s", want, showX)
		}
	}
	if strings.Contains(showX, "cleared") {
		t.Errorf("cleared empty option unexpectedly appeared in uci -X show:\n%s", showX)
	}
}

func TestIntegrationCToGoBatchAndDeltaMerge(t *testing.T) {
	lab := newUCILab(t)
	rejectStore := gouci.NewStore(
		gouci.WithConfDir(lab.confHost),
		gouci.WithConf2Dir(lab.conf2Host),
		gouci.WithDeltaDir(lab.deltaHost),
		gouci.WithDeltaPolicy(gouci.DeltaReject),
	)

	seedBatchBaseline(t, lab)
	assertBaselineAfterCommit(t, rejectStore)

	stageCLIDeltas(lab)
	_, err := rejectStore.Load("nettest")
	if !errors.Is(err, gouci.ErrDirtyDelta) {
		t.Fatalf("DeltaReject Load = %v, want ErrDirtyDelta", err)
	}

	assertMergedMatchesUCI(t, lab)
	commitMergedAndVerifyClean(t, lab)
}

func seedBatchBaseline(t *testing.T, lab *uciLab) {
	t.Helper()
	err := os.WriteFile(filepath.Join(lab.confHost, "nettest"), nil, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	batch := strings.Join([]string{
		"set nettest.lan=interface",
		"set nettest.lan.proto='static'",
		"set nettest.lan.ipaddr='192.168.1.1'",
		"add_list nettest.lan.dns='1.1.1.1'",
		"add_list nettest.lan.dns='8.8.8.8'",
		"add nettest rule",
		"set nettest.@rule[-1].name='Anon-Rule-0'",
		"add_list nettest.@rule[-1].proto='tcp'",
		"commit nettest",
	}, "\n") + "\n"
	lab.runUCI(batch, "batch")
}

func assertBaselineAfterCommit(t *testing.T, rejectStore *gouci.Store) {
	t.Helper()
	pkg, err := rejectStore.Load("nettest")
	if err != nil {
		t.Fatalf("DeltaReject Load after uci commit failed: %v", err)
	}
	lan, ok := pkg.Section("lan")
	if !ok {
		t.Fatal("lan missing")
	}
	if ip, _ := lan.Get("ipaddr"); ip != "192.168.1.1" {
		t.Fatalf("lan.ipaddr = %q, want 192.168.1.1", ip)
	}
	if dns, _ := lan.GetList("dns"); !slices.Equal(dns, []string{"1.1.1.1", "8.8.8.8"}) {
		t.Fatalf("lan.dns = %v", dns)
	}
}

func stageCLIDeltas(lab *uciLab) {
	lab.runUCI("", "set", "nettest.@rule[0].name=Anon-Updated")
	lab.runUCI("", "add_list", "nettest.lan.dns=9.9.9.9")
	lab.runUCI("", "del_list", "nettest.lan.dns=1.1.1.1")
	lab.runUCI("", "add", "nettest", "route")
	lab.runUCI("", "set", "nettest.@route[-1].target=10.0.0.0/8")
	lab.runUCI("", "reorder", "nettest.@route[-1]=0")
	lab.runUCI("", "rename", "nettest.lan.proto=protocol")
}

func assertMergedMatchesUCI(t *testing.T, lab *uciLab) {
	t.Helper()
	merged, err := lab.store.Load("nettest")
	if err != nil {
		t.Fatalf("DeltaMerge Load: %v", err)
	}
	fromUCI, err := gouci.ParseBytes([]byte(lab.runUCI("", "-N", "export", "nettest")))
	if err != nil {
		t.Fatalf("ParseBytes(uci merged export): %v", err)
	}
	if changes := gouci.Diff(fromUCI, merged); len(changes) != 0 {
		t.Fatalf("Diff(uci delta view, gouci DeltaMerge Load) = %+v", changes)
	}
}

func commitMergedAndVerifyClean(t *testing.T, lab *uciLab) {
	t.Helper()
	res, err := lab.store.Update(context.Background(), "nettest", func(p *gouci.Package) error {
		s, ok := p.Section("lan")
		if !ok {
			return gouci.ErrNotFound
		}

		return s.Set("netmask", "255.255.255.0")
	})
	if err != nil || !res.Changed {
		t.Fatalf("DeltaMerge Update: res=%+v err=%v", res, err)
	}
	if pending := strings.TrimSpace(lab.runUCI("", "changes", "nettest")); pending != "" {
		t.Fatalf("expected empty uci changes after Update, got:\n%s", pending)
	}
	finalGo, err := lab.store.Load("nettest")
	if err != nil {
		t.Fatal(err)
	}
	finalUCI, err := gouci.ParseBytes([]byte(lab.runUCI("", "-N", "export", "nettest")))
	if err != nil {
		t.Fatal(err)
	}
	if changes := gouci.Diff(finalUCI, finalGo); len(changes) != 0 {
		t.Fatalf("final Diff(uci, gouci) = %+v", changes)
	}
}

func TestIntegrationConf2DirParity(t *testing.T) {
	lab := newUCILab(t)
	if !lab.hasConf2Dir {
		t.Skipf("OpenWrt %s /sbin/uci does not support -C (conf2dir)", os.Getenv("UCI_TEST_RELEASE"))
	}

	err := os.WriteFile(filepath.Join(lab.confHost, "sys"), []byte("config system\n\toption hostname 'flash'\n"), 0o600)
	if err != nil {
		t.Fatal(err)
	}
	err = os.WriteFile(filepath.Join(lab.conf2Host, "sys"), []byte("config system\n\toption hostname 'overlay'\n"), 0o600)
	if err != nil {
		t.Fatal(err)
	}

	if got := strings.TrimSpace(lab.runUCI("", "get", "sys.@system[0].hostname")); got != "overlay" {
		t.Fatalf("uci get with -C = %q, want overlay", got)
	}

	pkg, err := lab.store.Load("sys")
	if err != nil {
		t.Fatal(err)
	}
	sys, _ := pkg.Section("@system[0]")
	if got, _ := sys.Get("hostname"); got != "overlay" {
		t.Fatalf("gouci Load with Conf2Dir = %q, want overlay", got)
	}

	_, err = lab.store.Update(context.Background(), "sys", func(p *gouci.Package) error {
		s, _ := p.Section("@system[0]")

		return s.Set("hostname", "overlay2")
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(lab.runUCI("", "get", "sys.@system[0].hostname")); got != "overlay2" {
		t.Fatalf("uci get after Update = %q, want overlay2", got)
	}
	flashData, _ := os.ReadFile(filepath.Join(lab.confHost, "sys"))
	if !bytes.Contains(flashData, []byte("'flash'")) {
		t.Errorf("ConfDir file unexpectedly modified:\n%s", flashData)
	}
}
