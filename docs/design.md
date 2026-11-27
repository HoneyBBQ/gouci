# Design

`gouci` treats OpenWrt's upstream `libuci` (`openwrt/uci`: `file.c`, `delta.c`, `list.c`, `util.c`) as the authoritative specification. Every lexer rule, validation predicate, anonymous ID hash, delta command, and file-lock ordering matches `libuci` so `gouci` can coexist safely with `/sbin/uci`, `rpcd`, `ucode-mod-uci` (`firewall4`), `netifd`, and `uci-defaults` across OpenWrt 21.02 through 25.12.

## Lexer and parser semantics (`file.c`)

- **Keywords**: `package`/`p`, `config`/`c`, `option`/`o`, `list`/`l` (case-sensitive).
- **Adjacent mixed-quote token concatenation**: Within a single non-whitespace token, unquoted runs, `'...'` runs, and `"..."` runs concatenate seamlessly (`'foo'\''bar'`, `'a'"b"c`, `foo'bar'`), terminating only at unquoted whitespace, `;`, `#`, or EOL.
- **Quoting & escaping**:
  - Inside `'...'`: backslashes are strictly literal; only `'` ends the span. Embedded newlines are preserved (multiline values).
  - Inside `"..."` and unquoted text: `\` followed by `\n` or `\r\n` is a line continuation (both `\` and the line break are stripped); `\` followed by any other byte emits that byte literally (`\"` -> `"`, `\'` -> `'`, `\\` -> `\`, matching `parse_backslash`).
- **Comments & multi-statement lines**: `#` outside quotes starts a comment to EOL (including mid-token in unquoted text); `;` outside quotes separates multiple statements on a single line.
- **Strict mode**: Extra trailing tokens on a statement, unterminated quotes, `option`/`list` before any `config`, invalid identifiers, or duplicate named sections with conflicting types return `*ParseError` (wrapping `ErrInvalidSyntax`) with `File`, `Line`, `Byte`, and `Reason`.
- **No fixed line-length cap**: Input is read dynamically rather than via `bufio.Scanner`'s 64 KiB cap, supporting large multiline values (tested up to 8 KiB+ in upstream `libuci`).

## Data model, validation, and addressing (`list.c`, `util.c`)

- **Strict ordering**: Sections within a `Package`, entries within a `Section`, and values within a `KindList` are stored in ordered slices—never maps—because order is semantically significant in OpenWrt (e.g., `firewall4` rules and `network` routes).
- **Named vs anonymous sections**:
  - `Section.Anonymous()` distinguishes anonymous sections (`config <type>`) from named sections (`config <type> '<name>'`).
  - `Section.Name()` returns `""` for anonymous sections; `Format()` and `Export()` never write synthetic names onto disk for anonymous sections.
  - `Section.ID()` returns `Name()` for named sections and `libuci`'s exact deterministic `cfg%02x%04x` identifier (`djbhash(type)` + 1-based section counter `n_section`) for anonymous sections.
- **Addressing (`Package.Section(ref)`)**:
  - Named sections (`"lan"`)
  - Extended typed index (`"@rule[0]"`, `"@rule[-1]"`) counting **all** sections of that type (named and anonymous combined, matching `uci_lookup_ext_section`)
  - Extended untyped index (`"@[0]"`, `"@[-1]"`) across all sections in the package
  - Synthetic anonymous ID (`"cfg01xxxx"`)
- **Scalars vs lists**:
  - `KindScalar` (`option`) and `KindList` (`list`) are distinct. A 1-element list stays `KindList` and never degrades to `option`.
  - Following `libuci` (`list.c:700`), `option` cannot hold an empty string: `Parse` ignores `option key ''` without creating a key, and `Section.Set(key, "")` deletes `key` if present (no-op if absent). `list key ''` preserves `""` as a valid list element.
  - `Section.Set` / `Section.SetList` overwrite an existing key in place (preserving option order); `SetList(key, nil/empty)` deletes `key` because UCI has no representation for a zero-element list.
  - `Section.AddList` upgrades an existing `KindScalar` to a `KindList` (preserving the old scalar as element 0 and option order).
  - `Section.DelList` removes all occurrences matching `value` (matching `uci_del_list`) and deletes the key when empty.
- **Validation**:
  - Package names: non-empty `[A-Za-z0-9_-]` (prevents path traversal and `.` filenames).
  - Section names and option keys: non-empty `[A-Za-z0-9_]` (`uci_validate_name`).
  - Section types: non-empty printable ASCII `0x21..0x7e` (`uci_validate_type`, permitting `wifi-device`, `wifi-iface`, etc.).
  - Values: `uci_validate_text` (disallows control bytes `< 0x20` except `\t`, `\n`, `\r`; permits UTF-8).

## Formatting and semantic diffing

- **`Format()` / `WriteTo()`**: Emits clean canonical `/etc/config/<pkg>` output—no `package` header, no leading blank line, single blank line between sections, `\toption` / `\tlist` indentation, values single-quoted with internal `'` escaped as `'\''`, and a single trailing newline.
- **`Export()`**: Emits `package <name>\n\n` followed by canonical section output (matching `uci -N export`).
- **`Diff(before, after)`**: `Package.Clone()` preserves internal section identities (`uid`), so `Store.Update` produces accurate section-added, section-deleted, section-renamed, section-type-changed, section-reordered, option-set, option-deleted, and list-changed events even when anonymous sections are inserted, deleted, or reordered in the middle of a package. Independent packages with disjoint identities fall back to `(Type, ordinal)` pairing for anonymous sections.

## Delta overlays (`/tmp/.uci`) and `Conf2Dir` (`/var/run/uci`)

- **`Conf2Dir` precedence (`file.c:933-956`, OpenWrt 24.10+)**: When `Conf2Dir` is non-empty and `<Conf2Dir>/<pkg>` exists, `Load`, `Update`, `ExportRaw`, and `RestoreRaw` operate on `<Conf2Dir>/<pkg>` as a whole-file override (no field merging with `ConfDir`). On OpenWrt 21.02–23.05 where `/var/run/uci` does not exist, lookup transparently falls through to `ConfDir`. `Packages()` enumerates `ConfDir` only, skipping directories, filenames containing `.`, and invalid package names (`uci_list_config_files`).
- **Delta grammar (`delta.c`)**: Parses and replays all 7 `libuci` delta commands: `+` (add anonymous section), unprefixed (set section type or option), `-` (delete section, option, or list item by 0-based index), `@` (rename section/option), `^` (reorder section), `|` (`add_list`), and `~` (`del_list`), including multiline quoted delta values.
- **`DeltaPolicy`**:
  - `DeltaMerge` (default): `Load` replays `<DeltaDir>/<pkg>` on top of the base config; `Update` merges external deltas with `fn`'s edits under lock, commits if net changes exist, and clears `<DeltaDir>/<pkg>` with `ftruncate(fd, 0)` (matching `delta.c:342`). Corrupt single delta lines are skipped and logged via `WithLogger`, matching `uci_parse_delta`.
  - `DeltaReject`: Returns `ErrDirtyDelta` if `<DeltaDir>/<pkg>` contains non-whitespace content. Note that an existing 0-byte `<DeltaDir>/<pkg>` left behind by `/sbin/uci commit`'s `ftruncate(0)` is treated as clean.

## Transactional locking and crash-safe commits (`Store`)

- **Lock ordering (deadlock-free with `/sbin/uci`)**: Always acquires an in-process per-package `sync.RWMutex`, then the target config file's `flock` (`LOCK_SH` for reads, `LOCK_EX` for writes), and only then `<DeltaDir>/<pkg>`'s `flock`—matching `libuci`'s exact lock order.
- **Inode-replacement protection & `ctx` cancellation**: After acquiring `flock`, `gouci` verifies `fstat(fd)` matches `stat(path)` (`Dev`/`Ino`) and retries if a concurrent writer atomically replaced the file while waiting. When `ctx.Done() != nil`, `LOCK_NB` with bounded jittered backoff honors context cancellation without leaking goroutines.
- **Zero-write no-op optimization (SPI-NAND flash protection)**: `Store.Update` compares the pre-transaction on-disk state against the post-`fn` tree using `Diff`. When `Changed == false`, it skips temporary-file creation, `fsync`, and `rename` completely (preserving file `mtime` and `inode`); if an external delta was a net no-op under `DeltaMerge`, only the delta file is truncated.
- **Atomic commit protocol**:
  1. Resolve symlinks on existing targets (`filepath.EvalSymlinks`, matching `realpath` in `file.c:821`).
  2. Preserve existing file mode (or apply `WithFileMode`, defaulting to `0600`).
  3. Create a dot-prefixed temp file `.<pkg>.uci-XXXXXX` in the target directory (ignored by `libuci`'s `get_filename`), `Chmod(mode)`, write full formatted payload, `file.Sync()`, `file.Close()`.
  4. `os.Rename(tmp, target)` while still holding `LOCK_EX`, then `dir.Sync()` on the parent directory.
  5. Remove the temp file on any error; never create a 0-byte config file when creating a new package whose `Update` callback errors or makes no changes.

## Public API snapshots and engineering gates

`api/gouci.txt` is generated and verified by `go run ./internal/tools/apidump -check`, alongside 100% GoDoc coverage enforcement, `golangci-lint` (`gofumpt`, `gci`, `decorder`, `funcorder`), `go test -race`, native fuzzing (`FuzzParse`, `FuzzRoundTrip`), and live bidirectional OpenWrt `21.02–25.12` container tests against `/sbin/uci`.
