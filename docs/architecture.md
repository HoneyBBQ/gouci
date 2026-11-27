# Architecture

```text
gouci/
├── *.go                      # package gouci: pure-Go OpenWrt UCI parser, ordered
│                             #   Package/Section/Entry AST, formatter/exporter,
│                             #   semantic Diff, /tmp/.uci delta replayer, and
│                             #   transactional flock-backed Store
├── api/
│   └── gouci.txt             # exported API snapshot (verified by apidump -check)
├── ci/images/                # pinned OpenWrt 21.02–25.12 container rootfs digests
├── docs/
│   ├── architecture.md       # repository layout and layering boundaries
│   └── design.md             # libuci compatibility contract and Store protocol
├── scripts/
│   ├── ci-image.sh           # resolves pinned OpenWrt container image references
│   ├── openwrt-lab.sh        # starts and stops ephemeral OpenWrt /sbin/uci containers
│   └── release-notes.sh      # extracts tag release notes from CHANGELOG.md
└── internal/
    ├── errs/                 # shared sentinel errors and %w wrapping helpers
    └── tools/
        └── apidump/          # generates and verifies api/gouci.txt + GoDoc coverage
```

## Two-layer separation: pure in-memory AST vs filesystem `Store`

`gouci` is intentionally split into two orthogonal layers inside one root package (`github.com/honeybbq/gouci`), with **zero third-party dependencies**, **zero CGO**, and **zero `os/exec` calls** in production code:

1. **In-memory configuration layer (`Package`, `Section`, `Entry`, `Parse`, `Format`, `Export`, `Diff`).**
   Performs zero filesystem I/O. It implements `openwrt/uci`'s exact lexical rules, strict statement grammar, deterministic `djbhash` (`cfg%02x%04x`) anonymous section IDs, ordered sections and options, single-quote escaping, and identity-aware semantic diffing. Because it is pure Go over `io.Reader`/`[]byte`, it can be unit-tested and fuzzed directly without touching disk.
2. **Transactional storage layer (`Store`, `Option`, `DeltaPolicy`, `CommitResult`).**
   Coordinates safe concurrent access to `/etc/config/*` (and optional `/var/run/uci` `Conf2Dir` overrides and `/tmp/.uci` `DeltaDir` overlays) alongside OpenWrt's native `/sbin/uci`, `netifd`, `firewall4`, `dnsmasq`, and `uci-defaults`.

## Companion boundary with `goubus`

`gouci` and [`goubus`](https://github.com/honeybbq/goubus) form a clean two-library pair for pure-Go OpenWrt daemons:

- **`gouci`** owns persistent `/etc/config/*` files: parsing, editing, semantic diffing, delta merging, and crash-safe atomic commits. It does **not** carry business-specific schema structs (`network`, `firewall`, `wireless`, `dhcp`), does **not** invoke `ubus` or service reloads, and does **not** implement cross-reboot rollback state machines.
- **`goubus`** owns runtime IPC (`ubusd`): calling `network.reload`, `service.event`, querying live state, and publishing Go `ubus` objects.

Upper-layer management daemons compose `Store.Update` (using `CommitResult.Changed` to avoid unnecessary flash writes and daemon reloads) together with `Store.ExportRaw` / `Store.RestoreRaw` for confirmed-apply rollback workflows.
