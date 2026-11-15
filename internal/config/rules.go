package config

import "strings"

// ExtractRules returns the rule lines of the top-level "rules:" list,
// trimmed, in order. Comment and blank lines inside the list are not
// returned (they are preserved in the file only until a rule edit
// rewrites the block — see ReplaceRules).
func ExtractRules(master string) []string {
	lines := strings.Split(master, "\n")
	start := -1
	for i, ln := range lines {
		if strings.HasPrefix(ln, "rules:") {
			start = i
			break
		}
	}
	if start < 0 {
		return nil
	}
	var rules []string
	for _, ln := range lines[start+1:] {
		if ln != "" && !strings.HasPrefix(ln, " ") && !strings.HasPrefix(ln, "\t") && !strings.HasPrefix(ln, "-") {
			break // next top-level key
		}
		t := strings.TrimSpace(ln)
		if strings.HasPrefix(t, "- ") {
			rules = append(rules, strings.TrimSpace(t[2:]))
		}
	}
	return rules
}

// ReplaceRules rewrites the top-level rules list with the given rules,
// one "  - <rule>" line each, leaving the rest of the file untouched.
// If the file has no rules section, one is appended.
func ReplaceRules(master string, rules []string) string {
	var block []string
	block = append(block, "rules:")
	for _, r := range rules {
		block = append(block, "  - "+r)
	}
	lines := strings.Split(master, "\n")
	start := -1
	for i, ln := range lines {
		if strings.HasPrefix(ln, "rules:") {
			start = i
			break
		}
	}
	if start < 0 {
		return strings.TrimSuffix(master, "\n") + "\n" + strings.Join(block, "\n") + "\n"
	}
	end := start + 1
	for end < len(lines) {
		ln := lines[end]
		if ln != "" && !strings.HasPrefix(ln, " ") && !strings.HasPrefix(ln, "\t") && !strings.HasPrefix(ln, "-") && !strings.HasPrefix(ln, "#") {
			break
		}
		end++
	}
	out := append([]string{}, lines[:start]...)
	out = append(out, block...)
	out = append(out, lines[end:]...)
	return strings.Join(out, "\n")
}
