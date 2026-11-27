<h1 align="center">gouci</h1>

<p align="center">
  <strong>Pure-Go OpenWrt <code>libuci</code>-compatible UCI parser, formatter, semantic differ, and transactional file store.</strong><br>
  Zero CGO · Zero third-party dependencies · Zero <code>os/exec</code> · MIPS / ARM / x86
</p>

<p align="center">
  <a href="https://pkg.go.dev/github.com/honeybbq/gouci"><img alt="Go Reference" src="https://pkg.go.dev/badge/github.com/honeybbq/gouci.svg"></a>
  <a href="https://github.com/honeybbq/gouci/actions/workflows/ci.yml"><img alt="CI" src="https://github.com/honeybbq/gouci/actions/workflows/ci.yml/badge.svg"></a>
  <a href="https://codecov.io/gh/honeybbq/gouci"><img alt="Codecov" src="https://codecov.io/gh/honeybbq/gouci/branch/main/graph/badge.svg"></a>
  <a href="https://scorecard.dev/viewer/?uri=github.com/HoneyBBQ/gouci"><img alt="OpenSSF Scorecard" src="https://api.scorecard.dev/projects/github.com/HoneyBBQ/gouci/badge"></a>
  <a href="https://github.com/honeybbq/gouci/releases"><img alt="Release" src="https://img.shields.io/github/v/release/honeybbq/gouci?include_prereleases&amp;sort=semver"></a>
  <a href="go.mod"><img alt="Go version" src="https://img.shields.io/github/go-mod/go-version/honeybbq/gouci"></a>
  <a href="LICENSE"><img alt="License: MIT" src="https://img.shields.io/badge/License-MIT-blue.svg"></a>
</p>

<p align="center">
  <a href="https://pkg.go.dev/github.com/honeybbq/gouci">Docs</a> ·
  <a href="docs/architecture.md">Architecture</a> ·
  <a href="docs/design.md">Design</a> ·
  <a href="CHANGELOG.md">Changelog</a> ·
  <a href="README_CN.md">中文文档</a>
</p>

## Overview

`gouci` is the persistent-config companion to [`goubus`](https://github.com/honeybbq/goubus) for pure-Go OpenWrt router daemons. It directly implements upstream `openwrt/uci` (`libuci`) semantics so Go services can safely read, edit, diff, and atomically commit `/etc/config/*` alongside `/sbin/uci`, `netifd`, `firewall4`, `dnsmasq`, and `uci-defaults`—with no `rpcd`, no LuCI, no CGO, and zero `os/exec("uci")` calls.

- **Exact `libuci` syntax & data model.** Handles mixed-quote token concatenation (`'foo'\''bar'`, `'a'"b"c`), single-quote literal backslashes vs double-quote continuations, multiline values, `#` comments, `;` multi-statement lines, `p`/`c`/`o`/`l` shorthands, ordered sections and lists (preserving 1-element `list`), named + anonymous `@type[idx]`/`@type[-1]`/`@[idx]` indexing, and deterministic `djbhash` (`cfg%02x%04x`) anonymous IDs.
- **Full `/tmp/.uci` delta & `/var/run/uci` (`Conf2Dir`) interoperability.** Replays all 7 `libuci` delta commands (`+`, unprefixed, `-`, `@`, `^`, `|`, `~`) under `DeltaMerge` (or refuses uncommitted external edits under `DeltaReject` while treating 0-byte truncated files as clean), and respects `Conf2Dir` whole-file overrides.
- **Flash-friendly, crash-safe `Store.Update`.** Serializes concurrent readers and writers using `libuci`'s exact `flock` ordering with inode-replacement verification and context cancellation, skips all disk writes on semantic no-ops (`Changed == false`, preserving `mtime` and `inode` on SPI-NAND flash), preserves existing file permissions (`0600` default for new files), and commits via dot-prefixed temp file + `fsync` + `rename` + directory `fsync`.
- **Snapshot/restore primitives.** `ExportRaw` and `RestoreRaw` provide locked raw byte snapshots so upper-layer daemons can build confirmed-apply rollback state machines without coupling `gouci` to `ubus` or service reloads.

## Install

```sh
go get github.com/honeybbq/gouci@latest
```

Requires Go 1.27 or newer.

## Quick start

```go
package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/honeybbq/gouci"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	store := gouci.NewStore()
	res, err := store.Update(ctx, "network", func(pkg *gouci.Package) error {
		lan, ok := pkg.Section("lan")
		if !ok {
			return gouci.ErrNotFound
		}
		if err := lan.Set("ipaddr", "192.168.10.1"); err != nil {
			return err
		}

		return lan.AddList("dns", "1.1.1.1")
	})
	if err != nil {
		log.Fatal(err)
	}
	if res.Changed {
		fmt.Printf("committed %d semantic change(s)\n", len(res.Changes))
	}
}
```

## Usage

### In-memory parsing, addressing, and formatting

```go
pkg, err := gouci.ParseBytes(raw)
if err != nil {
	if parseErr, ok := errors.AsType[*gouci.ParseError](err); ok {
		log.Printf("syntax error at line %d byte %d: %s", parseErr.Line, parseErr.Byte, parseErr.Reason)
	}
	return err
}

// Address by name, @type[idx], @type[-1], @[idx], or libuci cfgXXXXXX ID:
if lastRule, ok := pkg.Section("@rule[-1]"); ok {
	_ = lastRule.Set("target", "ACCEPT")
}

out, err := pkg.Format() // canonical /etc/config/<pkg> format
```

### Confirmed-apply snapshot and rollback primitives

```go
snap, err := store.ExportRaw("network")
if err != nil {
	return err
}

if _, err := store.Update(ctx, "network", applyChanges); err != nil {
	return err
}

// If connectivity check fails, restore the exact prior file verbatim:
if err := store.RestoreRaw(ctx, "network", snap); err != nil {
	return err
}
```

## Documentation

Full API reference: [pkg.go.dev/github.com/honeybbq/gouci](https://pkg.go.dev/github.com/honeybbq/gouci).

- [Architecture](docs/architecture.md): layering boundaries, repository layout, and companion split with `goubus`.
- [Design](docs/design.md): authoritative `libuci` lexer, model, delta, `Conf2Dir`, diff, and `flock` commit protocol.
- [Changelog](CHANGELOG.md): release notes and version history.

## Compatibility

| Target | Verified against |
| --- | --- |
| OpenWrt 21.02 – 25.12 | Live `21.02`, `22.03`, `23.05`, `24.10`, and `25.12` containers in CI with bidirectional `/sbin/uci` golden parity tests (Go → C, C → Go, `/tmp/.uci` delta merge/reject, and `/var/run/uci` `Conf2Dir` override). |
| Go | 1.27 and newer (CI tests Go 1.27 with `-race` + native fuzzing and cross-builds `linux/mipsle`, `linux/arm`, `linux/386`, `linux/arm64`, and `windows/amd64`); pure Go, zero CGO. |

## Contributing

See [CONTRIBUTING.md](.github/CONTRIBUTING.md) for local setup, formatting (`golangci-lint fmt`), API snapshot checks, fuzzing, and running the live OpenWrt container suite.

## License

[MIT](LICENSE)
