<h1 align="center">gouci</h1>

<p align="center">
  <strong>纯 Go 实现、权威对齐 OpenWrt <code>libuci</code> 的 UCI 解析器、格式化器、语义 Diff 与事务型文件存储库。</strong><br>
  零 CGO · 零第三方依赖 · 生产零 <code>os/exec</code> · MIPS / ARM / x86
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
  <a href="https://pkg.go.dev/github.com/honeybbq/gouci">文档</a> ·
  <a href="docs/architecture.md">架构</a> ·
  <a href="docs/design.md">设计</a> ·
  <a href="CHANGELOG.md">更新日志</a> ·
  <a href="README.md">English</a>
</p>

## 概览

`gouci` 是与 [`goubus`](https://github.com/honeybbq/goubus) 平级互补的 OpenWrt 纯 Go 基础设施库——`goubus` 负责运行态 IPC 总线，`gouci` 负责持久态 `/etc/config/*` 有序事务型配置文件存储。它以 OpenWrt 上游 `libuci`（`openwrt/uci`）为唯一权威真源，适用于无 `rpcd`、无 LuCI、生产零 `os/exec("uci")` 的精简 OpenWrt 路由器常驻守护进程，并与 `/sbin/uci`、`netifd`、`firewall4`、`dnsmasq`、`uci-defaults` 和平共存。

- **100% 对齐 `libuci` 词法与数据模型**：完整支持同 token 混合引号拼接（`'foo'\''bar'`、`'a'"b"c`）、单引号字面反斜杠 vs 双引号转义与续行、多行字符串值、`#` 注释、`;` 同行多语句、`p`/`c`/`o`/`l` 简写、严格保序的段与列表（单元素 `list` 永不退化为 `option`）、含命名段与匿名段混排的 `@type[idx]`/`@type[-1]`/`@[idx]` 寻址，以及与 `libuci` 一致的 `djbhash` 匿名段 ID（`cfg%02x%04x`）。
- **完整兼容 `/tmp/.uci` Delta 与 `/var/run/uci` (`Conf2Dir`)**：支持全部 7 种 `libuci` delta 指令（`+`、无前缀、`-`、`@`、`^`、`|`、`~`）；默认 `DeltaMerge` 策略在 `Load` 时重放叠加、`Update` 时在锁内合并落盘并 `ftruncate(0)` 清空；`DeltaReject` 遇到外部未提交改动直接返回 `ErrDirtyDelta`（自动识别 `/sbin/uci commit` 留下的 0 字节文件为干净状态）；支持 `Conf2Dir` 整文件覆盖读写。
- **护闪存、断电安全的事务型 `Store.Update`**：遵循 `libuci` 锁序（先 `conf` 后 `delta`）施加 `flock` 并校验 `inode` 替换重试、支持 `ctx` 取消；通过语义 `Diff` 实现**无净变更零写盘**（保留原文件 `mtime` 与 `inode`，保护 SPI-NAND 闪存寿命）；保留既有文件权限（新建默认 `0600`）；经由点号临时文件 + `fsync` + `rename` + 父目录 `fsync` 原子提交。
- **快照与恢复原语**：提供加锁的 `ExportRaw` / `RestoreRaw`，便于上层管理系统实现跨重启防断网 Confirmed Apply 回滚状态机，同时保持库本身零 `ubus` 耦合、零业务 schema 耦合。

## 安装

```sh
go get github.com/honeybbq/gouci@latest
```

需要 Go 1.27 或更高版本。

## 快速开始

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

## 兼容性矩阵

| 目标 | 验证方式 |
| --- | --- |
| OpenWrt 21.02 – 25.12 | CI 并行拉起真实 `21.02`、`22.03`、`23.05`、`24.10`、`25.12` 容器，执行 Go ↔ `/sbin/uci` 双向黄金对拍（含特殊字符、单元素 list、`cfgXXXXXX` ID、`/tmp/.uci` delta 与 `/var/run/uci` `Conf2Dir`）。 |
| Go | 1.27+（CI 覆盖 `-race`、Go 原生 Fuzz，并交叉编译 `linux/mipsle`、`linux/arm`、`linux/386`、`linux/arm64`、`windows/amd64`）；纯 Go 零 CGO。 |

## 许可协议

[MIT](LICENSE)
