// Package gouci implements OpenWrt's Unified Configuration Interface (UCI)
// storage format and transactional file store in pure Go.
//
// It aligns directly with OpenWrt's upstream libuci (file.c, delta.c, list.c,
// util.c) with zero CGO, zero third-party dependencies, and zero os/exec calls,
// enabling long-running Go daemons on OpenWrt routers to read and commit
// /etc/config/* safely alongside /sbin/uci, netifd, firewall4, dnsmasq, and
// uci-defaults.
//
// # Pure in-memory editing
//
// Parse, inspect, mutate, diff, and format UCI configuration packages without
// touching the filesystem using [Parse], [ParseBytes], [NewPackage],
// [Package.Format], [Package.Export], and [Diff]:
//
//	pkg, err := gouci.ParseBytes(raw)
//	if err != nil {
//		return err
//	}
//	if lan, ok := pkg.Section("lan"); ok {
//		_ = lan.Set("ipaddr", "192.168.10.1")
//	}
//	out, err := pkg.Format()
//
// # Transactional filesystem store
//
// Use [NewStore] and [Store.Update] to perform race-free read-modify-write
// transactions protected by advisory flock(2), inode-replacement verification,
// /var/run/uci (Conf2Dir) override resolution, /tmp/.uci delta replay, semantic
// zero-write no-op skipping (to protect SPI-NAND flash), and atomic temp-file
// sync + rename + directory fsync:
//
//	store := gouci.NewStore()
//	res, err := store.Update(ctx, "network", func(pkg *gouci.Package) error {
//		lan, ok := pkg.Section("lan")
//		if !ok {
//			return gouci.ErrNotFound
//		}
//		return lan.Set("ipaddr", "192.168.10.1")
//	})
//	if err != nil {
//		return err
//	}
//	if res.Changed {
//		// Trigger service reload (for example via goubus).
//	}
//
// # Errors
//
// Errors wrap the exported sentinels ([ErrNotFound], [ErrInvalidSyntax],
// [ErrInvalidName], [ErrInvalidValue], [ErrDuplicate], [ErrDirtyDelta],
// [ErrReadOnly]) so callers can match causes with [errors.Is]. Syntax errors
// return [*ParseError] with File, Line, Byte, and Reason fields inspectable via
// [errors.AsType].
package gouci
