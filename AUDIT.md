# ClashGo 审计与实施记录

## 范围、基线与边界

审计日期：2026-11-16。工作树基线 `7772738`（用户确认与主仓 `78df421` 内容一致），分支 `feat/phase1-mvp`。先完成本报告，再修改代码；不推送、不改写基线、不部署。

已通读 `cmd/clashgo` 全部页面、主题、辅助逻辑和测试，`internal/config`、`internal/subs`、`internal/core`、`internal/sysproxy` 全部源码，README、DECISIONS 和历史阶段报告。基线 `GOPROXY=off go test ./... -count=1` 全部通过，`GOPROXY=off go build -o /tmp/clashgo-baseline ./cmd/clashgo` 成功；core/sysproxy 尚无测试。个人配置金样测试按原约定跳过，不读取私人夹具。

对照本机 `mihomo Meta 1.19.33`（with_gvisor），以同版本源码为准；在线文档 main 是滚动版本，不能据其默认 mips 栈修改本机已验证的 gvisor。所有后续功能测试使用临时目录、独立端口、合成订阅。XDG 隔离本身不足以隔离 GNOME 代理：GUI 测试还必须使用独立 DBus/GSettings 后端。生产 master、subscriptions、settings、运行目录均不读写；无需读取控制器密钥。

## 架构与数据流

- MyGo 原生窗口 + Onevra 即时式视图，托盘关闭窗口但保留进程。`App` 集中持有配置、注册表、内核、代理管理器和页面状态；网络/文件操作多由 goroutine 执行。
- `Store` 管理 XDG 配置、profiles、快照、运行目录；`Render` 把用户 master 与订阅池合成临时配置；`mihomo -t` 验证后重命名，再通过控制器加载。
- `Registry.Fetch/ImportLocal` 下载/导入；`SplitFullConfig` 丢弃策略组，把叶子组拍平成节点，必要 dialer 写入单独文件；`Sources` 交给渲染层。
- `Supervisor` 启动 sidecar，PDEATHSIG 随父进程退出；`Client` 调用 REST API；`sysproxy.Manager` 管理 GNOME 代理的前态、接管、恢复。
- 2 秒轮询连接，30 秒刷新节点/网络/服务；DNS 页面解析 `/logs` 文本，不是完整 DNS 抓包器。页面包括概览、节点、配置（规则/模块/解析）、请求（连接/DNS）、设置。

产品边界保持：master 属于用户；订阅只提供节点，绝不带入策略组卡；只保留自有「节点选择」模型；TUN mtu=1500、缺省 ipv6=false、split=file、订阅域名 DIRECT、TUN DNS listener、settings 强制端口/控制器/secret。UU 不做 fwmark 绕路；如未来自动化，只能检测会话、带防抖热切换 TUN。

## 问题清单（按严重度）

### P0：数据安全与错误状态

1. **订阅输入验证不闭合**（`internal/subs/subs.go`、`split.go`）：按行猜测 provider，拆分错误可回落保存原始内容；空池、notice-only、畸形载荷可替换有效缓存；32 MiB LimitReader 不区分超限与完整读完。元信息先变更，provider 与 plumbing 分两次安装且忽略后者错误。更新失败不能保证缓存/注册表仍一致。全配置里的组必须在数据层无条件丢弃，不能只依赖核心忽略字段。
2. **配置更改不是事务**（`cmd/clashgo/app.go`）：规则/策略/恢复先写 master 再校验；切换 profile 先存 active，Save 错误后仍可能继续；运行配置先 rename 再 reload，reload 失败没有文件回滚。文档所称“任何失败保留旧配置”不成立。
3. **并发状态没有一致边界**（`app.go`、`services.go`、`view_services.go`、`store.go`）：busy 只是 UI 字段，不能串行化操作；settings/Registry/Client 指针与页面字段跨线程读写；服务结果 map 在解锁后迭代，同时后台写入，存在 fatal concurrent map read/write 风险。固定 `.tmp` 文件会互相覆盖。不能把普通测试全绿当作 race-free。

### P1：产品约束、生命周期、接口误用

4. **渲染按文本行修改 YAML**（`internal/config/render.go`）：flow-style、inline 注释、空 map、重复字段会绕过或破坏强制覆盖；secret 没有 YAML quoting。已有 `mtu: 1400/9000` 不改写，违反 1500 定版；原测试甚至钉住 1400，应先改钉子测试并说明原因。
5. **DIRECT 不一定优先**：看到任何 `DOMAIN,订阅域名,` 就不插入（可能实际 REJECT）；没有 rules/nameserver-policy 节时不补；已有冲突 DNS policy 不覆盖。订阅解析/更新可能递归依赖节点。TUN 遇到空 listen 或 dns.enable=false 仍无法工作；默认 0.0.0.0:1053 对 LAN 暴露 DNS。
6. **本地订阅 provider 错型**：`ProviderSource` 无 Local 字段，普通本地池生成 `type: http` 且 URL 空；split 正确为 file。普通远程 provider 由核心二次抓取，会绕过应用校验/元数据处理。
7. **provider 过滤类型大小写错误**：`exclude-type: Direct|Reject` 使用 API 展示名；官方要求配置 type（`direct|reject`），不是正则。应同时在入库时过滤，不能仅依赖渲染排除。
8. **重启与退出不完整**（`internal/core/core.go`、`app.go`）：Stop 只发 TERM 不 wait，重启最多轮询 4 秒但仍可能带旧子进程 Start；Running 与 cmd.Wait 读写 ProcessState 竞争。Reload 失败直接 startCore 会得到 `core already running`；WaitReady 超时没有停止新子进程；隐藏窗口时停掉健康/代理巡检，coreOK 可长期失真。
9. **provider 节点测速接口错误**（`internal/core/client.go`）：`/proxies/{name}/delay` 只查 tunnel.Proxies，不含 provider-only 节点；应使用 `/providers/proxies/{provider}/{name}/healthcheck`。每个订阅的测速按钮却测试整个「节点选择」。跨订阅重名合并不确定。
10. **控制器请求缺少可靠边界**：do 忽略 JSON marshal 错误、无响应大小上限，WaitReady 的 15 秒单请求可越过总预算；Proxies 遇到 null map 可 panic；只返回 HTTP code 不利诊断。Connection.Target 没有 IPv6 方括号。
11. **实例/代理所有权**：无单实例锁，同目录多进程能互相写配置/接管；Armed 只检查 HTTPPort，不核对 mode/host；Reassert 与恢复边界过于宽。隔离测试不可调用真实 gsettings。
12. **profile 路径与 builtin 指向**：持久化 profile 名未统一验证；`profileContent("")` 等用了活动 MasterPath，切换后查看/导出内置 master 实际读取其他 profile。

### P2：功能真实性、性能与可维护性

13. 服务全不选会被当作默认全选；地图快照问题见 P0。DNS pending 查询无期限，日志流缺乏退出上下文；根据解析器 IP 猜 DIRECT/代理不能当成真实路由证据。
14. 离线规则测试跳过不支持的 GEOSITE/RULE-SET 后显示 MATCH，造成确定性错觉；小火箭翻译器畸形已知规则可能越界，hosts 值直接拼 YAML，须校验/转义。
15. 视图每帧触发 master 文件读取、provider stat、/proc 内存读取；`px.Armed()` 可产生多次 gsettings 子进程。AnimationFrame 持续请求重绘。2 秒轮询重复请求 `/connections`，节点刷新重复请求 providers；临时 HTTP transport 的空闲连接未及时关闭。需把 I/O 移出渲染热路径，不先声称有内存收益。
16. 设置端口没有互异验证；保存 corePath 不立即应用且无效路径静默回退；自启路径不遵循 XDG，disable --now 可能终止当前实例。错误消息单行裁切，失败难追踪。
17. README/早期报告仍描述已撤销的多组模型与未实现的崩溃重启/事务保证；本报告以代码为准，不执行历史报告中的生产演练。

## mihomo 配置键核对

以下覆盖项目生成、覆盖、翻译和主动读取的键；订阅节点协议参数原样透传，不能声称已覆盖核心支持的每种协议。用户任意扩展键交由 `mihomo -t` 校验。

| 键/字段族 | 官方语义与结论 |
|---|---|
| mixed-port, external-controller, secret, allow-lan, mode, ipv6, log-level | 均有效；mixed/controller/secret 应以 settings 覆盖，controller loopback；ipv6 缺省在核心为 true，应用必须主动 false；当前每次 render 强制 mode=rule 会重置临时全局选择，保留现状并记录 |
| geodata-mode, geodata-loader | false 选 MMDB；memconservative 有效；seed 不等于所有规则资源都已离线可用 |
| tun.enable/stack/auto-route/auto-detect-interface/dns-hijack/mtu | 均有效，gvisor 本机已验证；启用值跟 settings，mtu 固定1500；不使用文档默认9000；不新增路由标记 |
| dns.enable/listen/ipv6/enhanced-mode | 有效，redir-host 合法；listen 支持 TCP/UDP；TUN 必须启用解析及 listener；缺省 loopback 更安全，尊重明确非空地址 |
| dns.default-nameserver/nameserver/proxy-server-nameserver/nameserver-policy | 有效；default 应为 IP（可加密）；节点 DNS 单独直连避免循环；`#节点选择`、`#DIRECT` 是合法 DNS 出站参数；policy 比 nameserver 优先，订阅域名必须覆盖冲突项 |
| hosts, dns.use-hosts/use-system-hosts | hosts 是独立映射，不是 nameserver-policy；界面当前名称混淆 |
| sniffer.enable/sniff.HTTP/TLS/QUIC.ports/override-destination | 类型与格式有效；不把 sniff 等同 TLS 解密 |
| proxy-providers.*.type/url/path/interval | http/file/inline；http 必须 URL；file 以本地 path 为准；path 限制 HomeDir；interval 秒；split 与 Local 必须 file |
| proxy-providers.*.exclude-filter/exclude-type/health-check.enable/url/interval | 关键词过滤支持正则；exclude-type 按配置 type 的 `|` 列表；健康检查间隔秒，timeout 毫秒（当前未指定 timeout/lazy/expected-status，核心默认） |
| proxy-groups.*.name/type/proxies/use/use-all-providers | select、DIRECT、provider use 合法；组 proxies 只引用全局节点，不能替代 use；仅应用自有「节点选择」，订阅组不可透传 |
| proxies.*.name/type/udp/dialer-proxy | name 应唯一；dialer 引用必须可解析，必要 plumbing 不能丢；现有全配置拍平强制 UDP 是已确认产品行为，保留，不拓展成协议猜测 |
| rule-providers.*.type/behavior/format/url/path/interval | type=http/file/inline；behavior=domain/ipcidr/classical；format=yaml/text/mrs，mrs 不支持 classical；当前 YAML/text 用法有效，路径需按真实格式读取 |
| rules 的 DOMAIN/DOMAIN-SUFFIX/DOMAIN-KEYWORD/IP-CIDR/IP-CIDR6/GEOIP/GEOSITE/RULE-SET/AND/NETWORK/DST-PORT/MATCH 与 no-resolve | 首条命中；MATCH 是兜底；IP 系列 no-resolve 合法；AND 组合 UDP443/853 合法；离线简化匹配器不能假装覆盖这些类型 |
| profile.store-selected/store-fake-ip | 可用未用：持久化节点选择；当前 redir-host 不需要 fake-ip 持久化。应默认启用 store-selected，尊重显式配置 |

## 控制器接口逐项核对

统一使用 Bearer 请求头，不输出 secret。地址只能来自隔离设置；不以生产 API 做实验。

| 当前接口 | 结论 |
|---|---|
| GET /version | 合法；只证明该端口有核心，仍须结合自己子进程存活/端口独占判定 ready |
| GET /proxies + GET /providers/proxies | 合法；provider-only 不在前者，当前合并有必要；不可按名字覆盖后丢失 provider 归属 |
| PUT /proxies/{group}, JSON name | 合法，组必须可选；保存选择还需 profile.store-selected；不会自动断开旧连接 |
| GET /group/{group}/delay?url&timeout | 合法，群组整体测试；不是单订阅测试 |
| GET /proxies/{name}/delay?url&timeout | 仅全局 proxy；provider 节点换用 GET /providers/proxies/{provider}/{name}/healthcheck，同参数 |
| PUT /providers/proxies/{provider} | 合法；file 刷新本地，http 会再次下载，必须避免绕过应用清洗 |
| GET /configs; PATCH /configs {mode} | 合法；PATCH 也支持 tun.enable 热切换，可用于未来 UU 会话防抖，不等于 API 已验证切换成功 |
| PUT /configs?force=true {path} | 合法；path 必须绝对且在安全目录；先解析后 ApplyConfig，parse 失败返回400；force 不提供磁盘回滚 |
| GET /connections; DELETE /connections; DELETE /connections/{id} | 合法；一次快照同时有 totals 和 connections，可消除重复请求；DELETE 不存在 ID 也204 |
| GET /dns/query?name&type | 合法，默认 A；返回 Status/Question/Answer，Answer 项含 name/type/TTL/data；DNS disabled 返回500 |
| GET /logs?level=debug | 支持 HTTP JSON 行流，也支持 WebSocket；是日志而不是稳定结构化 DNS 事件协议，应标注局限和限制缓冲 |

可用但未用：`/providers/rules` 查询/更新规则集、`/cache/dns/flush`、`/traffic` 流、`/memory`、日志 structured 格式、健康检查 expected-status、provider 下载大小限制/ETag、TUN PATCH。优先稳定已有功能，而不是把所有接口变成按钮。external-controller-unix/pipe 和 external-doh-server 有免 secret 路径，不能为了便利擅自开启。

## 同类功能面与 UI 差距

参考 FlClash、Clash Verge Rev、mihomo Party 官方 README 的功能列表/界面说明。Shadowrocket 官方 App Store 页面在当前网络重定向到商店首页；搜索结果混有冒名站点，不把它们当官方证据。其“用户规则配置 + 节点池”模型以本项目产品定版为准，不虚构本轮实机对比。

- FlClash：适配式界面、实时请求与日志、全局快捷键、本地/WebDAV 备份；ClashGo 缺少可审阅的完整日志页、备份恢复验证、快捷键、节点搜索/排序和批量更新。
- Clash Verge Rev：系统代理守护、配置编辑/增强、可视化规则、备份和主题；ClashGo 有代理守护雏形但隐藏窗口停巡检，编辑失败回滚不完整，规则模块状态/更新缺失。
- mihomo Party：常用设置面板、WebDAV、SubStore、覆写、核心版本管理；ClashGo 不能迁入订阅拥有策略的模型。脚本覆写/在线转换会扩大信任边界，本轮不做。
- 小火箭式体验的核心差距是：导入可靠、节点点击/测速可信、规则编辑不会破坏配置、失败有可见原因；先补这些，不增加策略组表面。

UI 明确问题：
1. 概览上排 2:1:1 和下排四列采用自然宽度 Grow，非同一列网格；卡片没有共同确定行高；主值靠顶而不是居中，footer 漂移。需固定共享四列（首卡跨两列）、等高卡、页眉/主值/元信息三段。
2. 表格样式不统一，规则表缺表头/斑马纹，部分请求表头未给操作列留宽，右缘错位。
3. “整包配置”徽标混淆已拍平节点池；状态解释性文案重复，改徽标/数值；保留左栏纯文本 ClashGo。
4. 节点无搜索，长订阅列表难定位；本地与远程来源不清晰；每订阅测速作用域不符文案。
5. Hosts 页面名与内容不符；错误单行截断；主题高对比/字号缩放并非所有控件一致。需分步修，不用全面换 UI 框架。

## 实施顺序与验证门槛

1. 订阅入库失败关闭、空/notice/超限/畸形拒绝、元数据与缓存保留、本地 provider 正确；测试覆盖数据层绝不保存订阅组。
2. YAML 结构化渲染和定版钉子（先更新 mtu 旧测试、大小写过滤测试的行为依据）；不回写 master；完善 DNS/DIRECT/secret 和选择持久化。
3. sidecar 生命周期、控制器接口/超时/IPv6；修复事务失败路径与 profile 错指，加入无生产依赖测试。
4. 修复可确定的服务 map/空选择、翻译畸形输入、轮询/视图热路径；改善共享网格、表格、节点搜索及测速。
5. 全套测试、必要 race 测试、隔离 mihomo 集成及 GUI 可行性验证；构建 `/tmp` 交付件；在本文件补实施总结和诚实遗留。

每项独立中文提交；每次提交前 gofmt 无差异且 `GOPROXY=off go test ./... -count=1` 全绿。不能以删测试方式过关。对超出本轮可可靠验证的改动（全应用并发模型重写、自动更新/备份、UU 检测）保留明确遗留，不把缺乏证据的实验推入生产。

## 资料与证据

- [mihomo v1.19.33 配置结构/解析](https://github.com/MetaCubeX/mihomo/blob/v1.19.33/config/config.go)
- [provider parser](https://github.com/MetaCubeX/mihomo/blob/v1.19.33/adapter/provider/parser.go)
- [proxies API](https://github.com/MetaCubeX/mihomo/blob/v1.19.33/hub/route/proxies.go)、[provider API](https://github.com/MetaCubeX/mihomo/blob/v1.19.33/hub/route/provider.go)、[group API](https://github.com/MetaCubeX/mihomo/blob/v1.19.33/hub/route/groups.go)
- [configs API](https://github.com/MetaCubeX/mihomo/blob/v1.19.33/hub/route/configs.go)、[connections API](https://github.com/MetaCubeX/mihomo/blob/v1.19.33/hub/route/connections.go)、[DNS API](https://github.com/MetaCubeX/mihomo/blob/v1.19.33/hub/route/dns.go)、[认证/日志/流式接口](https://github.com/MetaCubeX/mihomo/blob/v1.19.33/hub/route/server.go)
- [官方全局配置](https://wiki.metacubex.one/config/general/)、[DNS](https://wiki.metacubex.one/config/dns/)、[TUN](https://wiki.metacubex.one/config/inbound/tun/)、[代理集合](https://wiki.metacubex.one/config/proxy-providers/)、[规则集合](https://wiki.metacubex.one/config/rule-providers/)，对应 Meta-Docs 源文件已读取。
- [FlClash](https://github.com/chen08209/FlClash/blob/main/README.md)、[Clash Verge Rev](https://github.com/clash-verge-rev/clash-verge-rev/blob/main/README.md)、[mihomo Party](https://github.com/mihomo-party-org/mihomo-party/blob/main/README.md)

## 实施总结

### 已落地

| 提交 | 结果 |
|---|---|
| `ef482da` | 订阅严格单文档解析、32 MiB 超限拒绝、空池/公告池/缺失或循环 dialer 拒绝；只持久化清洗后的节点；普通 I/O 失败回滚缓存，元信息成功后更新 |
| `a8f8794` | Shadowrocket 导入合并重复域名策略、转义值并拒绝残缺规则；严格 YAML 检查揭露并修复原先重复 apple.com 键 |
| `b5ea7db` | 结构化渲染，支持 flow/merge，安全序列化 secret；MTU1500、缺省 IPv6=false、DNS enable/listen、DIRECT 优先钉子；所有应用管理订阅使用 file，避免核心二次下载绕过清洗；选择默认持久化 |
| `cea3c7b` | Supervisor 生命周期互斥、TERM 限时升级 KILL 并回收；无效显式核心路径报错；日志0600 |
| `c76dd14` | provider 专用测速路由；控制器响应上限16 MiB、整体 ready 截止时间、空 map 防护和 IPv6 地址格式 |
| `d94fd02` | 运行配置验证/安装/激活事务，热重载失败恢复文件并尝试恢复旧核心配置；新核心未就绪则停止 |
| `d20a584` | 查看、编辑、导出内置 master 不再误指当前活动 profile |
| `dbd850e` | 服务全关与默认全选区分并持久化，视图复制结果 map，关闭探测空闲连接 |
| `668f1b6` | 概览统一四列网格，首卡跨两列，184 DIP 等高卡，主值与底部信息分区；660/980/1400 宽度几何断言 |
| `d298c6d` | 节点名称/协议搜索、每订阅独立测速（并发上限4）、节点表头斑马纹、本地/已拍平徽标、失败测速状态 |
| `121a77f` | 规则表头斑马纹与固定列宽、连接表头操作列对齐 |
| `f54a177` | 概览 /proc、master 与 gsettings 查询移至2秒采样，连接和总流量合并成一次 /connections；隐藏时仍检查自有子进程存活 |
| `4a0c287` | 可重复运行的真实 mihomo 隔离集成测试 |

原测试没有删除：MTU1400 改为1500、默认 DNS wildcard 改 loopback、provider 过滤改配置类型小写，原因已写入渲染提交说明；导入 hosts 断言由裸字符串改为 YAML 语义，额外检查重复键。普通远程订阅也改 file 是有意的安全边界调整：更新由应用负责，核心不再按 HTTP provider.interval 自动更新。**尚未补应用级周期订阅更新**，目前需要手动刷新。

### 验证与交付

- 每次提交前 `gofmt` 检查干净，`GOPROXY=off go test ./... -count=1` 通过。
- 最终 `CLASHGO_TEST_MIHOMO=/usr/bin/mihomo GOPROXY=off go test -race ./... -count=1` 全部通过；`GOPROXY=off go vet ./...` 通过。race 只覆盖已执行测试，不证明全部真实 UI 事件无竞争。
- 集成测试 `internal/core/integration_test.go` 使用临时 XDG、临时 runtime、随机 loopback 端口与本地合成 HTTP CONNECT 服务；验证真实 1.19.33 的校验、启动、file provider、健康检查、选择、连接快照、重载和失败前文件保留；退出回收 sidecar，不启用 TUN、不调用 gsettings。最初合成代理未实现 CONNECT 导致503，补齐夹具后通过，不是生产网络实验。
- MyGo headless 测试证明概览不依赖 Store/Supervisor/代理管理器即可渲染；浅/深色预览分别输出 `/tmp/clashgo-delivery/overview-light.png`、`/tmp/clashgo-delivery/overview-dark.png`，已检查共享网格。不是生产界面截图，也不是完整原生窗口交互验收。
- 构建：`GOPROXY=off go build -o /tmp/clashgo-delivery/clashgo ./cmd/clashgo`。Linux x86-64 原生 ELF，ldd 无缺失依赖；保留现有启动入口和配置目录规则，可由 Muse 停止旧实例后替换二进制验收。未改生产任何文件、未读取生产 secret、未访问生产控制器、未部署、未 push。
- SHA256 与提交元信息放在 `/tmp/clashgo-delivery/` 的交付清单，避免本报告提交导致二进制 VCS 元数据变化后自引用哈希。

### 第一轮交付时的遗留（历史记录；第二轮进度见下）

1. **全应用并发与数据事务仍不完整**：settings/Registry/UI 回调等跨线程状态未整体串行化；master 编辑先写后验证、重启启动失败的全流程回滚、注册表 Save 失败一致性尚待处理。已修的是运行配置热重载事务，不是全部用户源配置事务。订阅双文件回滚也不是掉电/crash 原子性。
2. **单实例锁与代理所有权未解决**；生产启动验收须先停止旧应用，避免同 XDG 并发。系统代理 watchdog 仍受窗口显示条件影响，host/mode 检查和恢复所有权仍待完善。未运行会修改桌面代理或内核路由的验收。
3. **订阅规模骤减、跨订阅同名节点/同名 plumbing** 尚无完整冲突处理；应用级周期订阅更新、批量更新、排序、删除确认、失败详情页未实现。
4. **DNS 日志解析器** pending 上限、停止上下文、路由归因真实性仍待处理；离线规则测试跳过不支持规则后的确定性输出仍需改；Hosts 页含义、规则集缓存真实路径仍待修。
5. **UI/性能仍有边界**：完整桌面窗口、托盘、缩放/高对比、长名称和大订阅交互未实机覆盖；配置页/订阅页仍有文件 I/O，AnimationFrame 持续刷新、providers 重复请求未全部消除。没有宣称定量 CPU/内存收益；深色次级文字对比度仍可优化。
6. **UU 自动共存未实现**，未添加 fwmark 路由，未更换 gvisor。未来只能用会话检测加防抖热切换 TUN，须专门验证会话状态与恢复条件。
7. 备份/WebDAV、在线脚本/覆写、核心升级、日志完整页等竞品功能不在本轮实现；不引入订阅策略组。Shadowrocket 实机界面证据仍缺，不能把第三方冒名站点当验收依据。

以上为第一轮交付时的记录，不代表下列第二轮修复之后的状态。

## 第二轮：六项可靠性补强

用户随后授权继续实现六项遗留，并在隔离验证后自主实机安装。仍禁止直接修改生产 master.yaml / subscriptions.json、不 push、不在生产做功能实验。

| 提交 | 结果与边界 |
|---|---|
| `ddabdc2` | 加载任何配置前按 config 目录 flock 单实例；独立 XDG 不互斥，锁文件不删除，拒绝符号链接 |
| `f7bb9f9` | 后台任务准入、统一取消与回收；异步退出等待工作完成，主线程继续处理派发避免退出死锁 |
| `48a47a8` | 注册表深拷贝快照、版本提交、暂存下载；删除/重建期间的旧下载拒绝发布；唯一临时文件原子写入 |
| `a8d3a57` | 设置快照和持久化后发布；核心操作串行、忙时跳过轮询；界面字段只在主线程修改，受托管文件选择与扫码任务 |
| `3c71c60` | mode/三组 host/port 完整代理指纹；第三方修改后放弃所有权，不夺回、不覆盖；隐藏窗口也检查；所有权日志写入失败时拒绝接管 |
| `970b1ea` | 源配置/设置/运行配置先验证再发布；启动/重载失败恢复旧文件并尝试恢复核心；内置草稿编辑器与保存冲突检查；设置事务期间 UI 读取不阻塞 |
| `8057d28` | DNS pending 1024条/30秒过期；缓存命中清理；旧 PID/控制器日志流取消；只显示解析器，不凭 IP 猜直连/代理 |
| `a31d5ae` | 远程订阅每24小时更新，首次循环检查过期；一分钟检查节拍、失败1分钟指数退避至1小时；手动/自动合并，本地跳过，窗口隐藏仍运行；成功后重新渲染 file provider 与 plumbing |

### 第二轮验证

- 所有提交之前完整离线测试、gofmt 检查通过；最终 `CLASHGO_TEST_MIHOMO=/usr/bin/mihomo GOPROXY=off go test -race ./... -count=1`、`GOPROXY=off go vet ./...`、离线构建通过。
- 单实例子进程测试、设置并发增量、过期注册表结果、代理 mock 接管/第三方覆盖/失败、源配置拒绝不发布与激活失败回滚、DNS 旧流取消、订阅退避及并发合并均有测试。测试不是所有可能 GUI 调用交错的形式化证明。
- 额外运行原生 MyGo 窗口：临时 XDG `/tmp/clashgo-native-9waifvu6`、独立 D-Bus、`GSETTINGS_BACKEND=memory`、TUN关闭、随机端口48567/53481、合成主配置。Hyprland确认窗口映射，独立核心 `/version` 返回200；同目录第二实例退出1并报告 instance owns directory。不是生产配置副本。
- 原生隔离日志出现 document portal FUSE 权限告警、libayatana deprecation、GTK scale-factor assertion；窗口映射和核心就绪成功，不将这些环境/原生库告警隐藏或宣称零告警。完整托盘菜单与每个交互路径未穷举。

### 第二轮仍保留的边界

- 多文件事务提供普通错误回滚，**不提供掉电/crash 的跨文件原子性**；外部编辑器不受应用锁约束，草稿冲突检查仍非跨进程 CAS。回滚核心自身失败会返回组合错误。
- 订阅成功下载但后续核心验证/激活失败时，新清洗缓存仍在磁盘，旧运行配置恢复并报告错误；不是订阅缓存、注册表与核心的跨层完整事务。跨订阅重名、骤减保护仍未实现。
- 调度退避为会话内状态，重启后重新根据持久化 UpdatedAt 计算；固定24小时暂未提供间隔设置。核心操作串行意味着慢下载期间其他核心操作排队，轮询跳过；不是实时抢占调度。
- gsettings 多键写入本身不是原子操作，部分失败保留恢复日志；指纹不符时宁可不恢复，避免覆盖第三方。无桌面代理配置工具时会显式报错，不伪装成功。
- DNS debug 日志不是稳定事件 API，未知格式不推断；流最多2分钟重新连接、pending在下一条记录到来时清过期，始终有1024条硬上限。
- UU自动切换、备份/WebDAV、规则离线评估完整性、Hosts展示语义、大订阅长时间性能与完整主题可访问性仍不在本轮已完成声明内。

### 实机安装记录

- 2026-11-16：旧进程<pid>收到SIGTERM并正常退出后，原子替换 `/home/user/.local/bin/clashgo`，启动新版PID<pid>；自有 `/usr/bin/mihomo` PID<pid>、PPID<pid>，Hyprland窗口映射正常，7897/9097监听正常。
- 控制器无凭据 `/version` 返回401，验证鉴权边界与HTTP服务在线；没有读取生产密钥，没有进行带鉴权的生产功能测试。独立测试实例退出后两个端口全部释放。
- 安装的二进制与 `/tmp/clashgo-delivery/clashgo` 一致，构建源码提交 `a31d5ae`；后续提交仅补本文档。SHA256：`0e52af510f81cdeb2e26848c1786d5fb2d3835062798b8f1e9718f2a1f6746e2`。
- 旧二进制：`/home/user/.local/state/binary-backups/clashgo-before-20261116-0455`。需要回滚时先正常退出新版，再原子替换旧二进制并启动；不要让两个版本同时管理同一配置目录。
- 未直接修改生产 master.yaml / subscriptions.json。安装前后 master 的大小/mtime未变；注册表的大小/mtime在应用运行后变化，应用正常启动/周期更新可能保存自身注册表，不宣称生产注册表字节不变。未修改用户systemd unit；安装前该unit为failed且旧应用为独立进程，本次仍直接启动应用，未顺带变更自启动策略。
- 首次自动健康探针误用主线程的 `/proc/PID/task/PID/children`，遗漏Go工作线程派生的核心；随后重复查询被工具guardrail拒绝。改用全进程PPID关系一次检查确认实际核心归属、监听与鉴权响应；这不是核心启动失败。未靠反复启动应用绕过该问题。

## 第三轮：猫猫图标、浅色主题与短暂提示

- `c1e06b9`：采用用户选择的A款薄荷猫，左右各两根弧形胡须；原创SVG替换借用占位图，PNG嵌入二进制，无需运行时SVG依赖；侧栏仍为纯文字ClashGo。
- `ffc1622`：普通操作结果显示5秒后消失；任务进行提示不超时，结束清除旧进度；新结果独立计时，无旧定时器误删新消息；隐藏后重开不再显示过期结果。测试覆盖过期边界、替换、busy结束与显式清空。
- `37d4c85`：用户选择C档主色 `#CAD1FA`，同步图标/侧栏wash/启动按钮并为猫轮廓增加柔和阴影；普通按钮保留 `#5B67D8`，对白字对比度约4.80:1。
- `42488db`（判定语义已由下节修正）：托盘使用同款猫猫，当时彩色表示本地接管生效，灰度表示未生效或无法确认。要求自有核心健康、控制器响应且端口匹配；系统代理模式还须完整所有权指纹匹配，TUN模式还须运行配置启用且对应网卡UP。按2秒轮询采样，隐藏窗口仍更新；旧核心PID或旧接管模式的采样不能用于新版状态，图像缓存且只在状态变化时更新托盘。串行核心操作繁忙时采样会延后，不承诺严格2秒上限；彩色不等于所有远端节点/网站可达。
- `9dd2dad`：增加真实mihomo隔离测试确认 `/configs` 的 mixed-port/tun 字段；灰度测试逐像素确认RGB相等、alpha与尺寸不变及缓存复用。完整离线测试、全包race（包含真实内核）、vet通过；主题headless渲染通过，原生隔离实例启动并正常退出。没有用生产TUN开关做实验。
- 2026-11-16更新安装：应用PID<pid>、自有核心PID<pid>、控制器无凭据401；只读D-Bus确认托盘导出灰度猫及“代理未生效”提示，与当时系统代理mode=none且无TUN网卡一致。另一个D-Bus连接的树查询10秒超时，实际StatusNotifierItem端点的图标/提示读取成功。
- 此轮二进制：`/tmp/clashgo-delivery/clashgo-cat`，源码提交 `9dd2dad`，已安装至 `/home/user/.local/bin/clashgo`。SHA256 `dbc6df3104049ec1ee32be1f0b9e25fda4cd45d64b8b7150fec6606fb9eb0d8e`。hicolor及WhiteSur的ClashGo专用SVG均更新，图标缓存刷新退出0。
- 更新前应用和两份图标备份：`/home/user/.local/state/binary-backups/before-cat-20261116-0530/`。未直接编辑生产主配置、注册表或密钥文件，未push。

### 托盘误灰初判被实机故障证据推翻

用户先反馈服务正常而托盘仍灰。仅凭7897/9097监听、控制器HTTP401和gsettings mode=none，曾误判为手工代理场景，提交 `c466353` 放宽颜色条件。该版本**没有安装**。随后用户指出UI处于虚拟网卡模式，Chrome/Muse均无法访问Google，必须以TUN失败处理，不能靠改颜色掩盖故障。

### TUN权限故障恢复与就绪验收

- 直接原因：生产核心日志 `Start TUN listening error: configure tun interface: operation not permitted`。虽然 `/usr/bin/mihomo` 已有 `cap_net_admin,cap_net_raw=ep`，但此前从代理执行环境直接启动应用，继承 `NoNewPrivs: 1`，应用/核心的 `CapEff` 均为0，文件能力被屏蔽。核心控制器和混合端口仍启动，所以旧版 `/version` 验收误报健康；UI又直接按设置显示“虚拟网卡接管中”。
- 恢复方式：正常退出受限实例，经现有用户systemd manager启动瞬态 `clashgo-recovery.service`，没有更改执行环境的安全限制、持久化unit或生产配置。恢复后应用<pid>/core<pid>的 `NoNewPrivs: 0`，核心 `CapEff: 0x3000`，Meta网卡UP且MTU1500。Google经TUN和显式7897分别返回204，GitHub经TUN返回200。某私有上游的502/ETIMEDOUT是否同因仍未证实；没有读取其凭据。
- 代码修复：启动前识别会屏蔽网络文件能力的NoNewPrivs环境并明确报错，不自动提权或规避限制；启动、配置激活、回滚及运行配置重载均在宣称成功前核验真实TUN网卡。控制器enable标志不是成功证据，必须有命名网卡、UP、非回环、MTU1500；检查受取消/超时约束，失败不继续接管。
- 撤销 `c466353` 对TUN门槛的错误放宽。保留手工代理模式的端口检查；选中TUN时额外检查实际网卡，恢复采样模式及核心PID隔离。概览不再凭设置显示接管中，失败显示“虚拟网卡未就绪”，托盘灰色并标注“TUN 未就绪”。
- 钉子测试先改：明确“/version正常、代理端口开放、TUN enable=true、但网卡不存在”必须失败；覆盖NoNewPrivs及能力组合、设备缺失/未UP/MTU错误/回环、取消与超时。保留系统代理所有权与既有测试基线。全量离线测试、race、vet通过；不在生产制造TUN故障进行测试。后续部署必须由正常桌面或用户服务启动，不能直接继承本执行环境。
- 修复提交 `a8a675e`；`CLASHGO_TEST_MIHOMO=/usr/bin/mihomo GOPROXY=off go test -race ./... -count=1`、完整普通测试、vet、六个修改文件的主动LSP检查通过。安装二进制 `/tmp/clashgo-delivery/clashgo-tun-fix` 与 `/home/user/.local/bin/clashgo` 字节一致，SHA256 `f5b49ab45d79c9fa0e0db82718245119db5c29c7fcdb705347858ce1e6bf86b2`。
- 安装后应用PID<pid>由瞬态用户服务管理，Meta UP/MTU1500、控制器无凭据401；TUN访问Google204、GitHub200。D-Bus导出的托盘图像与仓库彩色猫PNG哈希一致，提示“代理服务可用”。未读取生产密钥或直接编辑主配置/注册表；旧二进制备份 `/home/user/.local/state/binary-backups/clashgo-before-tun-fix-20261116-0555`。

### 托盘描边猫与启动关键路径优化

- 托盘新增独立 `assets/tray.svg` / `assets/tray.png`。最初按确认稿安装透明背景黑色线稿，用户实机观察仍嫌线细，遂恢复原薄荷渐变填充猫：去背景、放大35%、深青描边、保留原五官与胡须。未就绪时按亮度转灰，保留五官对比与透明轮廓。窗口、桌面及启动器仍使用原彩色 `assets/icon.svg` / `assets/icon.png`，没有替换应用图标。
- 启动验收完成立即发布接管状态，不再等待两秒轮询及出口信息。出口查询改为托管后台单飞，更新请求取消旧请求、总预算8秒、组织查询最多2秒；请求代际、客户端和核心PID均匹配才发布，不在外部网络请求期间占用核心操作锁，保留同IP的组织信息缓存。
- 节点刷新共用一个provider响应，`/proxies` 加 `/providers/proxies` 从3次降为2次，保留provider失败时的已有界面与全局节点优先级。就绪初次重试改为20/40/80/160/250ms退避，不再固定等待250ms。
- 真核心race检查发现 `/version` 可先于初始配置与监听器生效返回成功（`mixed-port: 0`）。新增独立监听就绪验收，启动、激活、回滚及重载必须验证期望混合端口、实际TCP监听；TUN额外验证真实网卡UP/非回环/MTU1500。集成钉子测试明确等待这个阶段，而非依赖旧250ms延时掩盖时序问题，未删除既有断言。
- 新增只记录固定阶段名与耗时的启动日志：状态加载、代理会话、UI、节点缓存、配置验证、进程启动、控制器、接管检查、仪表盘。不记录URL、配置内容或凭据。
- 仍保留完整配置验证；研究中最小独立mihomo验证中位数仅14.5ms，未引入缓存或跳过验证。缺失订阅首次下载的有界并行属于后续可选优化：当前仍串行，避免在本轮速度调整中改变失败节点池与启动配置的语义。已有缓存路径无需下载。未改mihomo健康检查调度或代理所有权策略。
- 测试：完整离线测试、真实隔离mihomo全量race及vet通过。新增出口查询不阻塞核心操作、取消与过期结果拒绝、组织缓存、provider请求计数、短间隔重试、版本先就绪但配置/监听未就绪的回归覆盖。主动LSP检查9个改动文件无编译诊断；辅助Semgrep仍报告既有终端脱离逻辑的动态exec，参数来自 `os.Executable()` 与本进程参数，无shell拼接，本轮未更改该逻辑。
- 隔离对比：独立XDG目录、私有D-Bus、内存gsettings、随机端口、无TUN/远端订阅的最小配置，新旧各3次交替启动。未清操作系统页缓存，不代表生产冷机启动。控制器加监听就绪中位数314.3→316.0ms（基本不变）；托盘正常图标文件生成2588.1→579.3ms（减少77.6%）。20ms采样，指标为后端图标文件生成而非桌面绘制完成。结果 `/tmp/clashgo-startup-compare-result.json`。隔离门户仍有FUSE权限/GTK scale-factor警告，不影响验证，但未宣称原生库警告已修复。
- 首次实机安装优化版后，日志显示UI149ms、配置验证403ms、控制器708ms、接管检查902ms、仪表盘910ms；Meta UP/MTU1500，Google经TUN204、GitHub200。之后按新反馈替换托盘美术资源，启动逻辑不变。第二次实机启动接管检查为2893ms，说明受机器负载/桌面初始化影响，不能保证生产每次低于1秒。
- 最终按用户反馈将填充猫外轮廓从5加粗到9（+80%），不改五官、胡须或软件图标。代码提交 `6c7dec0`，安装产物 `/tmp/clashgo-delivery/clashgo-startup` 与 `/home/user/.local/bin/clashgo` 一致，SHA256 `5860c259eedacdc0cedee7833bfe1cb99dcd2a6d702200e298c2d0320acdc430`。当前应用PID<pid>由原用户瞬态服务启动，NoNewPrivs=0；Meta UP/MTU1500、Google经TUN204，D-Bus导出托盘PNG与新版资源哈希一致。最近备份 `/home/user/.local/state/binary-backups/clashgo-before-startup-20261116-063529`，本轮前的TUN修复版备份为 `/home/user/.local/state/binary-backups/clashgo-before-startup-20261116-062950`。
- 最终轮廓调整后完整普通测试与gofmt通过；前一填充猫版本全量真实mihomo race及vet通过，最后一提交仅改变SVG描边宽度和对应PNG。未直接编辑生产主配置或订阅注册表，未读取生产控制器密钥，未推送远端。保留首次缺失订阅串行下载、完整验证以及既有审计中说明的其他限制。
