# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- Pure-Go OpenWrt UCI parser (`Parse`, `ParseBytes`), ordered in-memory AST (`Package`, `Section`, `Entry`), canonical formatter (`Format`, `WriteTo`), and `uci -N export` exporter (`Export`) aligned with upstream `openwrt/uci` (`libuci`).
- Deterministic `djbhash` (`cfg%02x%04x`) anonymous section IDs (`Section.ID()`) and full section addressing (`name`, `@type[idx]`, `@type[-1]`, `@[idx]`, `@[-1]`, and `cfgXXXXXX`).
- Identity-aware semantic configuration diffing (`Diff`, `Change`, `ChangeKind`) with minimal LIS reorder detection.
- Full `/tmp/.uci` delta replay (`+`, unprefixed, `-`, `@`, `^`, `|`, `~`) with `DeltaMerge` and `DeltaReject` policies, plus `/var/run/uci` (`Conf2Dir`) whole-file override support.
- Crash-safe transactional `Store` (`Packages`, `Load`, `Update`, `ExportRaw`, `RestoreRaw`) featuring `libuci`-compatible `flock` ordering, inode-replacement verification, context cancellation, zero-write semantic no-op skipping, and dot-temp `fsync` + `rename` + parent-dir `fsync`.
- Public API snapshot gate (`api/gouci.txt`), 100% GoDoc coverage check, native fuzz targets (`FuzzParse`, `FuzzRoundTrip`), and live OpenWrt `21.02–25.12` bidirectional `/sbin/uci` integration suite.
