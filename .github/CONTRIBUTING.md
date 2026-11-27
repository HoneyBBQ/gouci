# Contributing to gouci

Thank you for contributing to `gouci`.

## Requirements

- Go 1.27 or newer
- `golangci-lint` v2.15.0 (for formatting and static checks)
- Docker (only required for live OpenWrt `21.02–25.12` integration tests)

## Engineering rules

- **Zero third-party dependencies**: `go.mod` must remain standard-library only (`CGO_ENABLED=0`).
- **Zero `os/exec` in production code**: `/sbin/uci` is only invoked inside `integration_test.go`.
- **100% GoDoc coverage**: every exported type, field, function, method, constant, and variable must be documented.
- **Frozen public API**: changes to exported symbols must update `api/gouci.txt` via `go run ./internal/tools/apidump`.

## Local verification

```sh
golangci-lint fmt
golangci-lint run
go run ./internal/tools/apidump -check
go test -race ./...
go test -fuzz=FuzzParse -fuzztime=10s .
go test -fuzz=FuzzRoundTrip -fuzztime=10s .
```

## Live OpenWrt integration tests

```sh
./scripts/openwrt-lab.sh start 25-12 /tmp/gouci-lab
. /tmp/gouci-lab/env.sh
go test -v -race -count=1 -tags integration ./...
./scripts/openwrt-lab.sh stop /tmp/gouci-lab
```
