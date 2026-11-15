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

审计阶段完成；实施与验证结果在后续提交补充。本节不把计划当成已完成事项。
