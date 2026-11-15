# ClashGo

**A lightweight Linux desktop client for the [mihomo](https://github.com/MetaCubeX/mihomo) core — built with Go + MyGo.**

Subscriptions are just node pools. Your config, your rules.

ClashGo follows the "rocket-style" model: the master configuration is a file
*you* own, subscriptions only feed nodes into it via `proxy-providers`, and
your rules and policy groups live in the master config — switching or losing a
subscription can never silently take your rules with it.

> Status: **Phase 0** — project skeleton and desktop spike validation.
> Not yet usable. ClashGo is an independent third-party client and is not
> affiliated with MetaCubeX.

## Why

Mainstream Clash clients ship the airport's whole bundle (nodes + groups +
rules) as one profile, so user rules bound to a profile die quietly when the
profile changes — and the clients themselves are heavy (hundreds of MB of
resident memory for a tray icon and a node switcher). ClashGo is the opposite
trade: one small native binary, file-based state you can inspect and back up,
and a config pipeline that validates (`mihomo -t`), swaps atomically and
rolls back automatically, so a bad subscription refresh can never break your
network.

## Tech stack

| Layer | Choice |
|---|---|
| Language | Go 1.27 |
| UI | [MyGo](https://github.com/egoist/mygo) (native mode, GPU-drawn, no webview) |
| Components | [Onevra](https://github.com/oneo-me/onevra) v0.2026.18 |
| Core | mihomo as a sidecar process, driven via its REST API |
| State | Plain files under XDG dirs — no database |

## Roadmap

- **Phase 0** — Desktop spike: tray on the target desktop, IME input,
  launching mihomo and switching nodes via the API, system-proxy set/restore.
- **Phase 1 (MVP)** — System-proxy mode, subscription pools, user rules,
  config snapshots and rollback, tray + main window.
- **Phase 2** — TUN mode via a small privileged helper (polkit), kill switch.
- **Phase 3** — Packaging (AUR + GitHub Releases), polish, accessibility pass.

## License

GPL-3.0. See [LICENSE](LICENSE).

---

## 中文说明

**ClashGo 是一个为 [mihomo](https://github.com/MetaCubeX/mihomo) 内核写的轻量 Linux 桌面客户端，用 Go + MyGo 构建。**

它走小火箭式的分工模型：主配置是归你所有的一份文件，订阅只通过
`proxy-providers` 往里喂节点；规则与策略组恒在主配置里，换订阅、订阅过期，
都不会把你的规则一起带走。

> 状态：**第 0 期**——项目骨架与桌面尖刺验证阶段，尚不可用。
> ClashGo 是独立的第三方客户端，与 MetaCubeX 无关。

### 解决什么问题

主流 Clash 客户端用的是机场整包模型（节点+分组+规则打包下发），绑在某个
订阅上的用户规则会随切换订阅静默失效；客户端本体也偏重（一个托盘加切节点
的功能常驻数百 MB）。ClashGo 反着来：单个小体积原生二进制、全部状态文件化
（可查、可备份、无数据库），配置生效走「渲染 → `mihomo -t` 校验 → 原子替换
→ 热重载」管线，失败自动回滚，订阅刷新永远刷不断网。

### 路线图

- **第 0 期**——桌面尖刺：托盘、中文输入法、拉起 mihomo 并经 API 切节点、系统代理开关与崩溃还原；
- **第 1 期（MVP）**——系统代理模式、订阅节点池、用户规则、配置快照与回滚、托盘与主窗口；
- **第 2 期**——TUN 模式（polkit 特权助手）与 kill switch；
- **第 3 期**——打包发布（AUR + GitHub Releases）、打磨与无障碍。

### 许可证

GPL-3.0，见 [LICENSE](LICENSE)。
