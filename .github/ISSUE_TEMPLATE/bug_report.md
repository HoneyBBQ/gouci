---
name: Bug Report
about: Report a parser, delta, diff, or Store bug in gouci
title: '[BUG] '
labels: bug
assignees: ''
---

## Summary

Describe what happened and how `gouci` differed from OpenWrt's `libuci` (`/sbin/uci`).

## Minimal reproduction

```go
// Paste a minimal Go snippet or UCI config / delta sample that reproduces the issue.
```

## Expected vs actual behavior

Paste the output of `/sbin/uci` (e.g. `uci -X show <pkg>` or `uci -N export <pkg>`) alongside the `gouci` result or returned error (`*gouci.ParseError`, sentinel error).

## Environment

- `gouci` version: (for example commit SHA or tag)
- Go version: (for example `go1.27.4`)
- OpenWrt release & target: (for example `24.10.8 x86/64` or `25.12.6 mediatek/filogic`)
