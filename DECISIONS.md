# Decisions

Lightweight decision log: date, decision, reason, what it supersedes.

- **2026-11-11 — Name: ClashGo.** Short, zero-explanation ("Clash client,
  written in Go"); the `carrotciallo/clashgo` repo slot was verified free.
  A handful of ≤2-star same-name repos exist (incl. one tiny mihomo
  client); accepted knowingly — the name is won by whoever ships.
- **2026-11-11 — Product model: master config owned by the user,
  subscriptions demoted to proxy-providers node pools.** Reason: in the
  bundled-profile model, user rules bound to a profile silently die when
  switching subscriptions (observed in production use). Rules and policy
  groups live in the master config, decoupled from any subscription.
- **2026-11-11 — Stack: Go + MyGo (native mode) + Onevra, mihomo as a
  sidecar process.** Reason: single-language stack (UI, logic and core are
  all Go), no cgo/webview, small binary; Onevra on MyGo was already
  proven in a previous project on the target machine. Supersedes the
  standing "GPUI Kit first for new GUI projects" default for this project.
- **2026-11-11 — License: GPL-3.0.** Matches the ecosystem (mihomo and
  mainstream clients are GPL-3.0); the client ships the mihomo binary in
  release bundles, so one license keeps obligations simple.
- **2026-11-11 — Distribution: bundle a pinned mihomo in releases
  (path overridable to a system install); ship via AUR + GitHub
  Releases.** Reason: out-of-the-box usability and a known-good core
  version, without locking users in.
- **2026-11-11 — State as files, no database.** All state lives as
  inspectable files under XDG directories; the master config is the
  single source of truth and can be backed up/migrated with git.
  Reason: multi-store state (db + prefs + generated config) proved
  impossible to reconcile when troubleshooting the previous client.
- **2026-11-11 — Local-first: develop in a local repo, publish later.**
  The GitHub repo goes public when the MVP is ready, not at day one.
- **2026-11-11 — Phase 1 config mechanics: marker blocks, no YAML
  library.** The master config stays a plain user-editable YAML file.
  ClashGo owns only two marked regions: the `proxy-providers` block and
  any group `use:` line preceded by a `# clashgo:use-all` marker.
  Everything else — including the rules list — is edited as lines, so
  user comments and formatting survive round-trips byte-for-byte, and
  the project takes on no YAML dependency. Rendering forces the runtime
  keys (ports, controller, secret) into the *runtime copy only*; the
  master file is never rewritten by rendering. Structured YAML editing
  can replace this later if a real need appears.
- **2026-11-11 — Phase 1 ports: mixed 7897, controller 127.0.0.1:9097.**
  Deliberately different from FlClash (7890) so both clients can coexist
  during the trial period; the system proxy is armed only after the core
  answers `/version` (Phase 0 incident: never point the desktop at an
  unverified core).
- **2026-11-11 — Core lifecycle: the GUI owns the core.** The mihomo
  child is spawned with PDEATHSIG so it cannot outlive the GUI (Phase 0
  left orphans), the core home is validated and seeded with geodata
  before spawn (a missing `geoip.metadb` makes mihomo hang on a network
  download instead of erroring), and startup failure surfaces the core
  log in the UI instead of failing silently.
- **2026-11-11 — Providers exclude pseudo outbounds, and DNS gets a
  dedicated proxy-server resolver.** Both found by live failure, see
  `docs/phase1-mvp-20261111.md`: rendered providers carry
  `exclude-type: "Direct|Reject"` so airport notice entries cannot
  capture url-test groups, and the template DNS includes
  `proxy-server-nameserver` (domestic DoH via DIRECT) — without it, a
  proxy-bound main nameserver deadlocks resolution of the nodes' own
  server domains. Validation is time-boxed at 30 s and subscriptions
  are fetched before the first validation, keeping validation local.
- **2026-11-11 — TUN capture, mutually exclusive with the system
  proxy.** Exactly one capture mode owns the machine's traffic at any
  time: TUN mode neutralizes the GNOME proxy to `none` (parked, with
  the previous settings kept for restore), proxy mode disarms TUN by
  not rendering it. The overview offers a single segmented choice and
  the tray a single toggle item, so the two can never be on together.
  The injected `tun.stack` is **gvisor**: measured live on this
  machine, the mixed/system stacks blackhole every TCP connection
  through the Meta interface while gVisor passes real traffic.
  Enabling TUN is a full core restart (a hot reload cannot apply it),
  with rollback to the previous mode when the core does not come up.
- **2026-11-11 — Providers also exclude notice nodes by name.** The
  Direct/Reject type filter does not catch airports' notice entries
  disguised as real protocols (one provider ships ss nodes named like
  "订阅链接可能已泄露" pointing at 8.8.8.8:12345); an url-test group
  that selects one blackholes all its traffic — measured live when it
  took down the first TUN trial. Rendered providers therefore also
  carry an `exclude-filter` built from a notice keyword list that the
  fragment importer shares.
- **2026-11-11 — Full-config subscriptions are split, and their
  proxies are inlined.** Some subscriptions answer with a whole client
  config whose user-facing nodes are proxy-groups (fallbacks over
  dialer proxies), not proxies. Fetch detects that shape, splits it
  (proxies to the provider file, groups into the registry as
  fragments), and rendering merges the fragments back: collision-safe
  renames, reachability pruning, entry groups surfaced in the picker.
  The proxies are spliced into the main `proxies:` section rather
  than loaded as a file provider — measured live, a group's
  `proxies:` entries never resolve against provider members (only
  `use:` draws from providers), so the provider shape cannot express
  these groups at all. This is also the one place the project takes a
  YAML dependency (gopkg.in/yaml.v3): node-level splitting must
  preserve every proxy field verbatim, which line editing cannot do.
  It supersedes the Phase 1 "no YAML library" note for this path
  only; the master config itself is still edited as lines.
- **2026-11-11 — Capture flags are claims, not truth: Arm and
  Neutralize verify against the live settings.** A capture switch
  raced a quit and landed after Disarm had restored the desktop,
  leaving the state file claiming `parked` while GNOME pointed at a
  dead port; the next start trusted the flag and no-opped. Both
  entry points now compare the record with the live GNOME values
  and re-assert (keeping the recorded prev) when they disagree, and
  startCore refuses to apply a capture switch once a quit is in
  flight. Reproduced by planting the stale state, verified live.
- **2026-11-11 — UI language: grouped lists, one card per group.**
  Borrowed from the reference clients' layouts (Shadowrocket's
  grouped inset lists, FlClash's card dashboard), not just their
  colors: section headers with counts, one rounded card per group,
  hairline-separated rows. The nodes page is collapsible group
  sections (the picker group starts open); rules render as
  structured rows in file order; subscriptions share the list.
- **2026-11-11 — The nodes page is organized by subscription, not
  by the core's groups.** The user thinks in subscriptions: each
  subscription is a section listing its own nodes, one tap makes a
  node the egress (through an intermediate group when the choice
  lives one hop down, as split subscriptions arrange it). Strategy
  groups stay available in a collapsed area — demoted, not deleted.
  Subscription management lives on the same page; there is no
  separate subscriptions page.
- **2026-11-11 — Configs are switchable profiles.** The master
  config is one of several files (built-in master.yaml plus
  profiles/<name>.yaml), switched from the 配置 page in
  Shadowrocket's shape: tap to switch, orange dot = default,
  check = active. A switch validates before it lands and reverts
  whole on failure; MasterPath resolves through the active
  profile, so rendering, snapshots and rule edits follow the
  switch with no other changes.
- **2026-11-11 — Strategy groups are not a user-facing feature
  (reverses the "demoted, not deleted" half of the nodes-page
  decision).** The product's model is subscriptions and nodes; the
  core's groups are routing plumbing inside the master config.
  The nodes page carries no group UI at all. The dashboard shows
  decision groups (service groups, top-level routers) as read-only
  status — what FlClash's panel does — and that is the only place
  group names surface.
- **2026-11-11 — Add forms live in dialogs.** Pages stay lists;
  adding a subscription, a profile or a rule starts from a + entry
  and opens a modal form (Onevra Dialog). No form is permanently
  expanded on a page.
- **2026-11-11 — Subscription groups are filtered at import; the
  group-status module is removed (reverses the dashboard round's
  "read-only decision groups" compromise).** A subscription is a
  node pool, full stop. When a payload is a whole client config,
  its proxy-groups are discarded at fetch: leaf groups (members
  all proxies) flatten into plain provider nodes — a renamed copy
  of the first member — and routing groups (service groups,
  hubs, url-test trees) never enter the system. The dialer
  plumbing the flattened nodes chain through is spliced into the
  main config's proxies: section, the only place it registers
  without joining a group; it must not live in the provider file,
  whose exclude-filter would unregister it and break every chain.
  The core now contains only the master template's own groups.
  Cost, accepted: intra-group entry-relay failover is lost (a
  flattened node keeps its preferred entry only).
- **2026-11-11 — Capture is asserted, not just applied: launch
  grab plus a running watchdog.** Starting ClashGo takes the
  system capture over whatever currently holds it (in TUN mode
  the desktop is parked to "none", in proxy mode it is armed to
  our port; the displaced settings become the recorded prev only
  when we did not already own the capture). While ClashGo runs,
  the poll loop re-asserts ownership every cycle
  (sysproxy.Reassert): if another client — FlClash arms GNOME on
  its own launch — rewrites the settings, ours are written back
  and the thief's values never replace the recorded prev.
- **2026-11-12 — The master template's rules are the user's
  Shadowrocket rules, ported.** The default master now carries
  the phone config's (v10) rule design in mihomo form: its local
  custom blocks translated line for line in the same order
  (QUIC/DoQ block, Apple TV via proxy, Apple Music direct,
  Private Relay via proxy, ad sets as REJECT), and the same
  maintained sets as rule-providers — blackmatrix7's Clash
  packaging of the identical sets, plus AWAvenue — with
  Shadowrocket's PROXY mapped to 节点选择 and FINAL to MATCH.
  Two honest losses: USER-AGENT rules have no mihomo equivalent
  (the TV domain rules and set cover the intent on a desktop),
  and REJECT-NO-DROP becomes plain REJECT. The [Host] apple
  entries become nameserver-policy entries pointing at the
  domestic DoH pair. Rule-providers live in the master file like
  everything else user-owned; ClashGo manages none of it.

## 2026-11-12 — Format adaptation: Shadowrocket import

The config page imports a Shadowrocket .conf (paste or URL) and
turns it into a ClashGo profile mechanically
(internal/config/translate.go): local rules translate in order,
RULE-SET/DOMAIN-SET become rule-providers pointing at the
ORIGINAL .list URLs — probed live, mihomo reads Surge text lists
natively in both behaviors (China_Domain 3689-3691 entries,
AWAvenue's Surge list yields its full 976 rules with exact-match
semantics the YAML domain packaging widens). Nothing is dropped
silently: USER-AGENT lines and every approximation
(REJECT-NO-DROP, unknown policies, [General]) are listed in the
import report dialog. A desktop private-range head is prepended
(the phone leans on skip-proxy, which has no equivalent here).
Golden check against the user's v10: the converter reproduces
the 2026-11-12 hand-synced rules exactly (sole delta: the
generated awavenue-ads provider name).

Found on the way, fixed: a split subscription with EMPTY dialer
plumbing spliced a bare "[]" under proxies:, which is invalid
YAML and killed a fresh install's first render outright.
ProviderItems now returns "" for an empty sequence and the
renderer skips "[]" lines; regression tests pin both.
Observed, not yet guarded: an airport can answer a fetch with a
degraded payload (one notice node instead of the full list) and
Fetch accepts it over a healthy cache; a shrink guard belongs in
Fetch if it ever bites outside a sandbox.

## 2026-11-12 — Tray click, segmented capture switch, silent autostart

Tray: the icon now registers Tray.OnClick -> openWindow. Without
a listener the SNI item is not activatable at all, which was the
root of "clicking the tray does nothing" all along; verified by
sending Activate over DBus to the running item — the window
appears.

Capture: the dashboard's two independent switches became a
segmented 系统代理 | 虚拟网卡 control over the same setCapture —
the modes are mutually exclusive by the user's ruling and now
look that way.

Silent autostart (default ON): autostart entries (Hyprland
exec_cmd, the app's systemd unit) launch with --autostart; such
a launch starts tray-only when settings.silentAutostart is on.
The setting defaults true even for settings files written
before it existed, until the user turns it off in Settings.

## 2026-11-12 — Terminal launch detaches into its own session

Launching the app from an interactive terminal made it a child of
that terminal session: closing the terminal SIGHUPed the app and
the core with it (hit in real use). main() now first calls
detachFromTerminal: if the process has a controlling terminal, it
re-executes itself with Setsid and the parent exits, so the app
survives the terminal. Launches that already have no controlling
terminal (systemd unit, compositor autostart, desktop entry) are
untouched. Env registry: CLASHGO_DETACHED=1 marks the re-executed
child (loop guard); CLASHGO_NO_DETACH=1 opts out and keeps the
foreground behavior for debugging.

## 2026-11-12 — Node rows wear flagicons circle flags

Node-row flags come from xiyaowong/flagicons (lipis/flag-icons,
MIT) as circle-cut SVG images, replacing emoji flags: emoji render
at the whim of the local font stack, the library's vectors do not.
Region codes derive from the name — a baked-in flag emoji if the
airport wrote one, else region keywords — which also gives the
split pool's flattened group names ("H01香港 主力") the flags they
never carried. Parsed flags are cached; unknown regions fall back
to a globe badge. The egress strips show the chain without the
root picker's name (节点选择 is plumbing, not a hop).

## 2026-11-12 — Dashboard round 2: requests page, service checks, quota, no snapshots

Per the user's direction (benchmark: FlClash's dashboard and tools
pages): a 请求 page joins the nav (live connection table with
per-connection close, recently-closed requests tracked client-side
from poll diffs, and a DNS lookup through the core); the dashboard
gains per-service reachability checks (Google/GitHub/YouTube/…,
probed through the mixed port so they measure the real routed path)
with a full page and a manage mode persisted in settings; the
config card shows the first quota-reporting subscription's traffic
usage and days left (subscription-userinfo header, captured at
fetch). Adding a subscription diversifies: link / file / paste /
QR code (zbarimg over a chosen image or the clipboard image), and
file/paste subscriptions are stored as local content with refresh
as a no-op. Header actions go icon-first (lucide), per the same
direction. The config-snapshot/rollback mechanism is removed
entirely — it was never used, and renders are validated before
install, so a bad config still never lands.

## 2026-11-12 — Plumbing groups preprocessed out, not hidden

User directive, stated repeatedly and finally without room for
interpretation: 自动选择/故障转移-style scaffolding groups must
not exist in the software at all — preprocessing is the answer,
not UI hiding. Subscription payloads already had their
proxy-groups discarded wholesale at ingest (internal/subs). The
remaining source was our own master template: it now defines
exactly one group, 节点选择, a plain selector hanging every
provider node directly (plus DIRECT). The live production master
was migrated the same way (backup master.yaml.bak-20261112-groups);
the running core verifies groups = [GLOBAL, 节点选择] with the
user's pick (J06日本 主力） untouched. GLOBAL is the core's
built-in and cannot be configured away; it appears in no file and
no screen. Failover/url-test behaviour is gone by design — the
user picks nodes, Shadowrocket-style.

## 2026-11-12 — Config page rebuilt in Shadowrocket's shape; Onevra stays

The config page is now the reference client's page, not an
approximation of it: an actions card （恢复默认配置 / 导入小火箭
配置 / 模块 / 测试规则）, the 本地文件 list (orange dot =
default, check = in use, tap switches through the validated
path, ⓘ opens the detail page), and a per-file detail page —
section rows with counts （通用 / 规则 / Hosts / 规则集 URL
x/y) over read-only section lists, plus 编辑纯文本 (hands the
file to the desktop editor), 导出配置， 复制， 替换策略 (bulk
policy rename across the file's rules), 测试规则 (offline
first-match evaluation) and 设为默认 / 删除. The one legend
line under the file list is the reference client's own text,
kept as part of its design. Rules stay editable only on the
active profile; other files' rules are listed read-only.

Component library, settled the same day by the user: Onevra
stays. The Lobe MyGo port (shell + dashboard spike) is rejected
and deleted from the field — no migration.
