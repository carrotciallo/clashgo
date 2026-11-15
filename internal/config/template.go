package config

// DefaultMaster is the first-run master config: a mainland-friendly
// ruleset with the managed regions ClashGo maintains. Users own this
// file outright after creation — ClashGo only ever rewrites the marked
// blocks and the rules list (on explicit rule edits in the UI).
const DefaultMaster = `# ClashGo master config — this file is yours.
# ClashGo manages only the marked blocks (proxy-providers and group
# "use:" lines flagged with clashgo:use-all); everything else, rules
# included, is edited as-is and passed to the core untouched.

geodata-mode: false
geodata-loader: memconservative

dns:
  enable: true
  # redir-host, not fake-ip: measured live (2026-11-11) — with fake-ip,
  # session creation for domain-addressed nodes dies with "dns resolve
  # failed" in this setup, while redir-host (the mode the reference
  # client config on this machine uses) works end to end.
  enhanced-mode: redir-host
  # Bootstrap only (proxy server names, DoH endpoints): plain domestic
  # DNS is fine for that. Everything else goes by policy below —
  # measured on a real mainland network (2026-11-11): domestic DoH
  # returns poisoned answers for foreign domains and foreign DoH is
  # unreachable directly, so foreign resolution must ride the nodes.
  default-nameserver:
    - 223.5.5.5
    - 119.29.29.29
  nameserver:
    - "https://1.1.1.1/dns-query#节点选择"
    - "https://8.8.8.8/dns-query#节点选择"
  # Proxy server domains must resolve WITHOUT the proxy — the main
  # nameserver rides the proxy, so without this dedicated resolver the
  # first connection deadlocks (measured live 2026-11-11; the working
  # reference config on this machine carries the same key).
  proxy-server-nameserver:
    - "https://dns.alidns.com/dns-query#DIRECT"
    - "https://doh.pub/dns-query#DIRECT"
  nameserver-policy:
    "geosite:cn,private":
      - https://dns.alidns.com/dns-query
      - https://doh.pub/dns-query
    "+.apple.com":
      - https://dns.alidns.com/dns-query
      - https://doh.pub/dns-query
    "+.icloud.com":
      - https://dns.alidns.com/dns-query
      - https://doh.pub/dns-query
    "+.mzstatic.com":
      - https://dns.alidns.com/dns-query
      - https://doh.pub/dns-query
    "+.aaplimg.com":
      - https://dns.alidns.com/dns-query
      - https://doh.pub/dns-query
sniffer:
  enable: true
  sniff:
    HTTP:
      ports: [80, 8080-8880]
      override-destination: true
    TLS:
      ports: [443, 8443]
    QUIC:
      ports: [443, 8443]

# >>> clashgo:providers (managed — edit subscriptions in ClashGo, not here)
proxy-providers: {}
# <<< clashgo:providers

proxy-groups:
  # The single selection point — Shadowrocket's model has no
  # url-test/fallback scaffolding groups, so neither does this
  # config: 节点选择 hangs every provider node directly and the
  # user picks. Subscription payloads' own groups are discarded
  # at ingest (internal/subs), never reaching this file.
  - name: 节点选择
    type: select
    # clashgo:use-all
    use: []
    proxies:
      - DIRECT

# Rule providers: the same maintained sets the user's Shadowrocket
# config references (blackmatrix7 ios_rule_script, Clash packaging of
# the identical sets, plus AWAvenue). Synced from Shadowrocket v10 on
# 2026-11-12; policies live in the rules list below, providers are
# policy-free payloads.
rule-providers:
  appletv:
    type: http
    behavior: classical
    format: yaml
    url: "https://cdn.jsdelivr.net/gh/blackmatrix7/ios_rule_script@master/rule/Clash/AppleTV/AppleTV.yaml"
    path: ./rules/appletv.yaml
    interval: 86400
  awavenue:
    type: http
    behavior: domain
    format: yaml
    url: "https://cdn.jsdelivr.net/gh/TG-Twilight/AWAvenue-Ads-Rule@main/Filters/AWAvenue-Ads-Rule-Clash.yaml"
    path: ./rules/awavenue.yaml
    interval: 86400
  advertising:
    type: http
    behavior: classical
    format: yaml
    url: "https://cdn.jsdelivr.net/gh/blackmatrix7/ios_rule_script@master/rule/Clash/Advertising/Advertising.yaml"
    path: ./rules/advertising.yaml
    interval: 86400
  advertising-domain:
    type: http
    behavior: domain
    format: yaml
    url: "https://cdn.jsdelivr.net/gh/blackmatrix7/ios_rule_script@master/rule/Clash/Advertising/Advertising_Domain.yaml"
    path: ./rules/advertising-domain.yaml
    interval: 86400
  lan:
    type: http
    behavior: classical
    format: yaml
    url: "https://cdn.jsdelivr.net/gh/blackmatrix7/ios_rule_script@master/rule/Clash/Lan/Lan.yaml"
    path: ./rules/lan.yaml
    interval: 86400
  proxy:
    type: http
    behavior: classical
    format: yaml
    url: "https://cdn.jsdelivr.net/gh/blackmatrix7/ios_rule_script@master/rule/Clash/Proxy/Proxy.yaml"
    path: ./rules/proxy.yaml
    interval: 86400
  proxy-domain:
    type: http
    behavior: domain
    format: yaml
    url: "https://cdn.jsdelivr.net/gh/blackmatrix7/ios_rule_script@master/rule/Clash/Proxy/Proxy_Domain.yaml"
    path: ./rules/proxy-domain.yaml
    interval: 86400
  openai:
    type: http
    behavior: classical
    format: yaml
    url: "https://cdn.jsdelivr.net/gh/blackmatrix7/ios_rule_script@master/rule/Clash/OpenAI/OpenAI.yaml"
    path: ./rules/openai.yaml
    interval: 86400
  google:
    type: http
    behavior: classical
    format: yaml
    url: "https://cdn.jsdelivr.net/gh/blackmatrix7/ios_rule_script@master/rule/Clash/Google/Google.yaml"
    path: ./rules/google.yaml
    interval: 86400
  telegram:
    type: http
    behavior: classical
    format: yaml
    url: "https://cdn.jsdelivr.net/gh/blackmatrix7/ios_rule_script@master/rule/Clash/Telegram/Telegram.yaml"
    path: ./rules/telegram.yaml
    interval: 86400
  discord:
    type: http
    behavior: classical
    format: yaml
    url: "https://cdn.jsdelivr.net/gh/blackmatrix7/ios_rule_script@master/rule/Clash/Discord/Discord.yaml"
    path: ./rules/discord.yaml
    interval: 86400
  github:
    type: http
    behavior: classical
    format: yaml
    url: "https://cdn.jsdelivr.net/gh/blackmatrix7/ios_rule_script@master/rule/Clash/GitHub/GitHub.yaml"
    path: ./rules/github.yaml
    interval: 86400
  youtube:
    type: http
    behavior: classical
    format: yaml
    url: "https://cdn.jsdelivr.net/gh/blackmatrix7/ios_rule_script@master/rule/Clash/YouTube/YouTube.yaml"
    path: ./rules/youtube.yaml
    interval: 86400
  netflix:
    type: http
    behavior: classical
    format: yaml
    url: "https://cdn.jsdelivr.net/gh/blackmatrix7/ios_rule_script@master/rule/Clash/Netflix/Netflix.yaml"
    path: ./rules/netflix.yaml
    interval: 86400
  spotify:
    type: http
    behavior: classical
    format: yaml
    url: "https://cdn.jsdelivr.net/gh/blackmatrix7/ios_rule_script@master/rule/Clash/Spotify/Spotify.yaml"
    path: ./rules/spotify.yaml
    interval: 86400
  steam:
    type: http
    behavior: classical
    format: yaml
    url: "https://cdn.jsdelivr.net/gh/blackmatrix7/ios_rule_script@master/rule/Clash/Steam/Steam.yaml"
    path: ./rules/steam.yaml
    interval: 86400
  apple:
    type: http
    behavior: classical
    format: yaml
    url: "https://cdn.jsdelivr.net/gh/blackmatrix7/ios_rule_script@master/rule/Clash/Apple/Apple.yaml"
    path: ./rules/apple.yaml
    interval: 86400
  apple-domain:
    type: http
    behavior: domain
    format: yaml
    url: "https://cdn.jsdelivr.net/gh/blackmatrix7/ios_rule_script@master/rule/Clash/Apple/Apple_Domain.yaml"
    path: ./rules/apple-domain.yaml
    interval: 86400
  china-domain:
    type: http
    behavior: domain
    format: yaml
    url: "https://cdn.jsdelivr.net/gh/blackmatrix7/ios_rule_script@master/rule/Clash/China/China_Domain.yaml"
    path: ./rules/china-domain.yaml
    interval: 86400
  china:
    type: http
    behavior: classical
    format: yaml
    url: "https://cdn.jsdelivr.net/gh/blackmatrix7/ios_rule_script@master/rule/Clash/China/China.yaml"
    path: ./rules/china.yaml
    interval: 86400
  globalmedia:
    type: http
    behavior: classical
    format: yaml
    url: "https://cdn.jsdelivr.net/gh/blackmatrix7/ios_rule_script@master/rule/Clash/GlobalMedia/GlobalMedia.yaml"
    path: ./rules/globalmedia.yaml
    interval: 86400
  globalmedia-domain:
    type: http
    behavior: domain
    format: yaml
    url: "https://cdn.jsdelivr.net/gh/blackmatrix7/ios_rule_script@master/rule/Clash/GlobalMedia/GlobalMedia_Domain.yaml"
    path: ./rules/globalmedia-domain.yaml
    interval: 86400
  global:
    type: http
    behavior: classical
    format: yaml
    url: "https://cdn.jsdelivr.net/gh/blackmatrix7/ios_rule_script@master/rule/Clash/Global/Global.yaml"
    path: ./rules/global.yaml
    interval: 86400
  global-domain:
    type: http
    behavior: domain
    format: yaml
    url: "https://cdn.jsdelivr.net/gh/blackmatrix7/ios_rule_script@master/rule/Clash/Global/Global_Domain.yaml"
    path: ./rules/global-domain.yaml
    interval: 86400

rules:
  # Private / LAN first (the old skeleton head; the Lan set below agrees)
  - DOMAIN-SUFFIX,local,DIRECT
  - IP-CIDR,127.0.0.0/8,DIRECT,no-resolve
  - IP-CIDR,10.0.0.0/8,DIRECT,no-resolve
  - IP-CIDR,172.16.0.0/12,DIRECT,no-resolve
  - IP-CIDR,192.168.0.0/16,DIRECT,no-resolve
  - IP-CIDR,100.64.0.0/10,DIRECT,no-resolve
  # === Synced from Shadowrocket v10 (2026-11-12), order preserved ===
  # QUIC + DoQ blocked (forces TCP fallback); v10's REJECT-NO-DROP has
  # no mihomo twin, plain REJECT is the closest.
  - AND,((NETWORK,UDP),(DST-PORT,443)),REJECT
  - AND,((NETWORK,UDP),(DST-PORT,853)),REJECT
  # Apple TV / TV+ via proxy (the two USER-AGENT lines have no mihomo
  # equivalent and are intentionally not carried over)
  - DOMAIN-SUFFIX,tv.apple.com,节点选择
  - DOMAIN-KEYWORD,buy.itunes.apple.com,节点选择
  - DOMAIN-SUFFIX,vod.itunes.apple.com,节点选择
  - DOMAIN-SUFFIX,play-edge.itunes.apple.com,节点选择
  - DOMAIN-SUFFIX,play.itunes.apple.com,节点选择
  - DOMAIN,hls-amt.itunes.apple.com,节点选择
  - DOMAIN,hls.itunes.apple.com,节点选择
  - DOMAIN,np-edge.itunes.apple.com,节点选择
  - DOMAIN,uts-api.itunes.apple.com,节点选择
  - DOMAIN,tv.applemusic.com,节点选择
  - RULE-SET,appletv,节点选择
  # Apple Music explicit DIRECT (top priority in v10)
  - DOMAIN-SUFFIX,music.apple.com,DIRECT
  - DOMAIN-SUFFIX,applemusic.com,DIRECT
  - DOMAIN-SUFFIX,itunes.apple.com,DIRECT
  - DOMAIN-SUFFIX,itunes.com,DIRECT
  - DOMAIN-SUFFIX,mzstatic.com,DIRECT
  - DOMAIN-SUFFIX,aaplimg.com,DIRECT
  - DOMAIN-SUFFIX,icloud-content.com,DIRECT
  - DOMAIN-SUFFIX,me.com,DIRECT
  # Mainland DIRECT supplement
  - DOMAIN-SUFFIX,douyin.com,DIRECT
  - DOMAIN-SUFFIX,douyincdn.com,DIRECT
  - DOMAIN-SUFFIX,douyinpic.com,DIRECT
  - DOMAIN-SUFFIX,bytedance.com,DIRECT
  - DOMAIN-SUFFIX,byteimg.com,DIRECT
  - DOMAIN-SUFFIX,toutiaocdn.com,DIRECT
  - DOMAIN-SUFFIX,pinduoduo.com,DIRECT
  - DOMAIN-SUFFIX,kuaishou.com,DIRECT
  - DOMAIN-SUFFIX,bilivideo.com,DIRECT
  - DOMAIN-SUFFIX,aliyun.com,DIRECT
  - DOMAIN-SUFFIX,aliyuncs.com,DIRECT
  - DOMAIN-SUFFIX,1688.com,DIRECT
  - DOMAIN-SUFFIX,goofish.com,DIRECT
  - DOMAIN-SUFFIX,huawei.com,DIRECT
  - DOMAIN-SUFFIX,mi.com,DIRECT
  - DOMAIN-SUFFIX,unionpay.com,DIRECT
  # Apple Private Relay / iCloud Mask / Siri via proxy
  - DOMAIN,guzzoni.apple.com,节点选择
  - DOMAIN,mask-api.fe.apple-dns.net,节点选择
  - DOMAIN,mask-api.icloud.com,节点选择
  - DOMAIN,mask-t.apple-dns.net,节点选择
  - DOMAIN,mask.apple-dns.net,节点选择
  - DOMAIN-SUFFIX,mask.icloud.com,节点选择
  - DOMAIN-SUFFIX,mask-h2.icloud.com,节点选择
  - DOMAIN-SUFFIX,gateway.icloud.com,节点选择
  - DOMAIN-SUFFIX,apps.mastic.com,节点选择
  - DOMAIN-KEYWORD,siri,节点选择
  - DOMAIN-SUFFIX,apple-relay.apple.com,节点选择
  - DOMAIN-SUFFIX,apple-relay.cloudflare.com,节点选择
  - DOMAIN-SUFFIX,apple-relay.akamaiized.net,节点选择
  - DOMAIN-SUFFIX,apple-relay.fastly-edge.com,节点选择
  - DOMAIN-SUFFIX,apple-relay.tasty-edge.com,节点选择
  - DOMAIN-SUFFIX,apple-relay.mask.apple-dns.net,节点选择
  - DOMAIN-SUFFIX,smoot.apple.com,节点选择
  - DOMAIN-SUFFIX,gspe1-ssl.ls.apple.com,节点选择
  - DOMAIN-SUFFIX,ls.apple.com,节点选择
  - DOMAIN-SUFFIX,cp4.cloudflare.com,节点选择
  # Modern PROXY supplement
  - DOMAIN-SUFFIX,chatgpt.com,节点选择
  - DOMAIN-SUFFIX,sora.com,节点选择
  - DOMAIN-SUFFIX,anthropic.com,节点选择
  - DOMAIN-SUFFIX,claude.ai,节点选择
  - DOMAIN-SUFFIX,perplexity.ai,节点选择
  - DOMAIN-SUFFIX,x.ai,节点选择
  - DOMAIN-SUFFIX,googleapis.com,节点选择
  - DOMAIN-SUFFIX,discord.com,节点选择
  - DOMAIN-SUFFIX,discordapp.com,节点选择
  - DOMAIN-SUFFIX,discord.gg,节点选择
  - DOMAIN-SUFFIX,reddit.com,节点选择
  - DOMAIN-SUFFIX,redd.it,节点选择
  - DOMAIN-SUFFIX,redditstatic.com,节点选择
  - DOMAIN-SUFFIX,spotify.com,节点选择
  - DOMAIN-SUFFIX,spotifycdn.com,节点选择
  - DOMAIN-SUFFIX,x.com,节点选择
  - DOMAIN-SUFFIX,threads.net,节点选择
  - DOMAIN-SUFFIX,threads.com,节点选择
  - DOMAIN-SUFFIX,huggingface.co,节点选择
  - DOMAIN-SUFFIX,netflix.com,节点选择
  - DOMAIN-SUFFIX,disneyplus.com,节点选择
  - DOMAIN-SUFFIX,tiktok.com,节点选择
  - DOMAIN-SUFFIX,tiktokv.com,节点选择
  - DOMAIN-SUFFIX,notion.so,节点选择
  - DOMAIN-SUFFIX,figma.com,节点选择
  - DOMAIN-SUFFIX,steamcommunity.com,节点选择
  # Muse / Meta AI explicit
  - DOMAIN-SUFFIX,muse.ai,节点选择
  - DOMAIN-SUFFIX,meta.ai,节点选择
  - DOMAIN-SUFFIX,metaaiusercontent.com,节点选择
  # Microsoft DIRECT (v10 restores it against the Proxy set), Steam
  # content DIRECT, Akamai via proxy
  - DOMAIN-SUFFIX,microsoft.com,DIRECT
  - DOMAIN,steamcdn-a.akamaihd.net,DIRECT
  - DOMAIN-SUFFIX,steamcontent.com,DIRECT
  - DOMAIN-SUFFIX,steamusercontent.com,DIRECT
  - DOMAIN-SUFFIX,akamai.net,节点选择
  - DOMAIN-SUFFIX,akamaihd.net,节点选择
  # Apple services by IP (17/8 is Apple's own range)
  - IP-CIDR,17.0.0.0/8,DIRECT,no-resolve
  # Ad blocking: AWAvenue curated first, blackmatrix7 Advertising net
  - RULE-SET,awavenue,REJECT
  - RULE-SET,advertising,REJECT
  - RULE-SET,advertising-domain,REJECT
  # Remote maintained sets (blackmatrix7, same sets as the phone's)
  - RULE-SET,lan,DIRECT
  - RULE-SET,proxy,节点选择
  - RULE-SET,proxy-domain,节点选择
  - RULE-SET,openai,节点选择
  - RULE-SET,google,节点选择
  - RULE-SET,telegram,节点选择
  - RULE-SET,discord,节点选择
  - RULE-SET,github,节点选择
  - RULE-SET,youtube,节点选择
  - RULE-SET,netflix,节点选择
  - RULE-SET,spotify,节点选择
  - RULE-SET,steam,DIRECT
  # Apple + China DIRECT sets sit before the Global sets (first match)
  - RULE-SET,apple,DIRECT
  - RULE-SET,apple-domain,DIRECT
  - RULE-SET,china-domain,DIRECT
  - RULE-SET,china,DIRECT
  - RULE-SET,globalmedia,节点选择
  - RULE-SET,globalmedia-domain,节点选择
  - RULE-SET,global,节点选择
  - RULE-SET,global-domain,节点选择
  # China by IP, then final via proxy (v10's FINAL,PROXY)
  - GEOIP,CN,DIRECT
  - MATCH,节点选择`
