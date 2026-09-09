// Package parse holds Cairn's pure text logic: extracting tags and wiki-links
// from an entry body, and turning a free-form "idea dump" into structured items.
// It has no dependencies on the database or HTTP layers.
package parse

import (
	"regexp"
	"sort"
	"strings"
)

var (
	tagRe  = regexp.MustCompile(`(^|[\s(])#([a-zA-Z][\w-]*)`)
	linkRe = regexp.MustCompile(`\[\[([^\]]{1,120}?)\]\]`)
)

// Tags returns the distinct #hashtags in s, lower-cased, in first-seen order.
// A leading "## " markdown heading is not a tag (needs a letter right after #).
func Tags(s string) []string {
	var out []string
	seen := map[string]bool{}
	for _, m := range tagRe.FindAllStringSubmatch(s, -1) {
		t := strings.ToLower(m[2])
		if !seen[t] {
			seen[t] = true
			out = append(out, t)
		}
	}
	return out
}

// Links returns the distinct [[wiki-link]] targets in s, trimmed, in
// first-seen order. Case is preserved; resolution is the caller's job.
func Links(s string) []string {
	var out []string
	seen := map[string]bool{}
	for _, m := range linkRe.FindAllStringSubmatch(s, -1) {
		t := strings.TrimSpace(m[1])
		if t == "" {
			continue
		}
		key := strings.ToLower(t)
		if !seen[key] {
			seen[key] = true
			out = append(out, t)
		}
	}
	return out
}

// NormalizeTag lower-cases and strips a leading '#'.
func NormalizeTag(s string) string {
	return strings.ToLower(strings.TrimPrefix(strings.TrimSpace(s), "#"))
}

// SortedUnique returns the distinct, lower-cased, sorted members of in.
func SortedUnique(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		s = strings.ToLower(strings.TrimSpace(s))
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}
