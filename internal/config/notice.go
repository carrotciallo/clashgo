package config

import "strings"

// noticeNameParts mark airport notice entries disguised as nodes or
// groups ("订阅链接可能已泄露" and friends). They are excluded as
// provider nodes (exclude-filter) and dropped at import when a
// full-config subscription is flattened.
var noticeNameParts = []string{"拉取于", "订阅链接", "请注意", "盗用", "剩余流量", "到期时间", "官网", "公告"}

// IsNoticeName reports whether a node or group name is one of the
// airport's notice entries rather than a real node.
func IsNoticeName(name string) bool {
	for _, p := range noticeNameParts {
		if strings.Contains(name, p) {
			return true
		}
	}
	return false
}

// noticeFilter is the provider exclude-filter regex built from the
// same keyword list, so both layers agree on what a notice is.
func noticeFilter() string { return strings.Join(noticeNameParts, "|") }
