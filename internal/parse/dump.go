package parse

import (
	"regexp"
	"strings"
	"time"
)

// Item is one parsed result from an idea dump, ready to be reviewed and saved
// as an entry. Sub holds its subtasks (from dash-prefixed lines beneath it).
type Item struct {
	Title    string
	Body     string
	IsTask   bool
	Done     bool
	Priority int      // 0 none, 1 low, 2 medium, 3 high
	Due      string   // ISO date or ""
	Project  string   // project name or ""
	Tags     []string // lower-cased, from #hashtags
	Links    []string // [[wiki-link]] targets
	Sub      []Item   // subtasks
}

var actionVerbs = map[string]bool{
	"call": true, "email": true, "text": true, "message": true, "ask": true,
	"tell": true, "follow": true, "followup": true, "send": true, "reply": true,
	"write": true, "draft": true, "review": true, "read": true, "watch": true,
	"finish": true, "complete": true, "fix": true, "build": true, "make": true,
	"create": true, "add": true, "remove": true, "delete": true, "update": true,
	"refactor": true, "test": true, "deploy": true, "ship": true, "release": true,
	"buy": true, "order": true, "pay": true, "book": true, "schedule": true,
	"plan": true, "prep": true, "prepare": true, "clean": true, "organize": true,
	"sort": true, "file": true, "print": true, "sign": true, "submit": true,
	"renew": true, "cancel": true, "confirm": true, "check": true, "verify": true,
	"research": true, "find": true, "get": true, "pick": true, "grab": true,
	"move": true, "install": true, "setup": true, "configure": true, "migrate": true,
	"backup": true, "publish": true, "meet": true, "join": true,
}

var (
	bulletRe   = regexp.MustCompile(`^\s*(?:[-*+•]|\d+[.)])\s+`)
	checkboxRe = regexp.MustCompile(`^\s*(?:[-*+]\s*)?\[( |x|X)\]\s*`)
	todoRe     = regexp.MustCompile(`(?i)^\s*todo:?\s+`)
	priBangRe  = regexp.MustCompile(`(^|\s)!(high|hi|med|medium|mid|low|lo|1|2|3|p1|p2|p3)\b`)
	projectRe  = regexp.MustCompile(`(^|\s)[@+]([a-zA-Z][\w-]{0,40})`)
	dueRe      = regexp.MustCompile(`(?i)(^|\s)(?:due|by|on|~):?\s*([a-z0-9/]+(?:\s+\d{1,2}(?:st|nd|rd|th)?)?)`)
	blankLine  = regexp.MustCompile(`\r?\n[ \t]*\r?\n+`)
)

// Dump parses free-form text into items. Blocks are separated by a blank line.
// Within a block the first line is the item; a following line that starts with a
// dash (or other bullet marker) becomes one of its subtasks, and any other line
// is folded into the item's body. A block that opens with a bullet has no head,
// so each of its bullets is a top-level item instead.
func Dump(text string, now time.Time) []Item {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	var items []Item
	for _, block := range blankLine.Split(text, -1) {
		if strings.TrimSpace(block) == "" {
			continue
		}
		items = append(items, parseBlock(block, now)...)
	}
	return items
}

func isBulletLine(s string) bool {
	return bulletRe.MatchString(s) || checkboxRe.MatchString(s)
}

func parseBlock(block string, now time.Time) []Item {
	lines := nonEmptyLines(block)
	if len(lines) == 0 {
		return nil
	}

	// No head line: a bare bullet list — each bullet is its own top-level item,
	// with plain continuation lines folded into the bullet above.
	if isBulletLine(lines[0]) {
		var items []Item
		for i := 0; i < len(lines); {
			head := lines[i]
			i++
			var cont []string
			for i < len(lines) && !isBulletLine(lines[i]) {
				cont = append(cont, lines[i])
				i++
			}
			if it, ok := parseItem(head, strings.Join(cont, "\n"), now); ok {
				items = append(items, it)
			}
		}
		return items
	}

	// Head line + subtasks (dashed lines) + body (everything else).
	var body, subs []string
	for _, ln := range lines[1:] {
		if isBulletLine(ln) {
			subs = append(subs, ln)
		} else {
			body = append(body, ln)
		}
	}
	main, ok := parseItem(lines[0], strings.Join(body, "\n"), now)
	if !ok {
		return nil
	}
	for _, sl := range subs {
		if sub, ok := parseItem(sl, "", now); ok {
			main.Sub = append(main.Sub, sub)
		}
	}
	if len(main.Sub) > 0 && !main.IsTask {
		main.IsTask = true // a thing with subtasks is a task
	}
	return []Item{main}
}

func parseItem(head, body string, now time.Time) (Item, bool) {
	it := Item{}

	forcedTask := false
	if m := checkboxRe.FindStringSubmatch(head); m != nil {
		forcedTask = true
		if strings.EqualFold(m[1], "x") {
			it.Done = true
		}
		head = checkboxRe.ReplaceAllString(head, "")
	}
	if todoRe.MatchString(head) {
		forcedTask = true
		head = todoRe.ReplaceAllString(head, "")
	}
	head = bulletRe.ReplaceAllString(head, "")

	combined := head
	if body != "" {
		combined += "\n" + body
	}

	// Priority: !high / !2 / !p3 ...
	if m := priBangRe.FindStringSubmatch(combined); m != nil {
		it.Priority = priorityValue(m[2])
		combined = priBangRe.ReplaceAllString(combined, "$1")
	}

	// Project: @name or +name
	if m := projectRe.FindStringSubmatch(combined); m != nil {
		it.Project = m[2]
		combined = strings.Replace(combined, m[0], m[1], 1)
	}

	// Explicit due: "due tomorrow", "by 9/10", "on fri", "~sep 10"
	if m := dueRe.FindStringSubmatch(combined); m != nil {
		if iso, ok := ParseDate(m[2], now); ok {
			it.Due = iso
			combined = strings.Replace(combined, m[0], m[1], 1)
		}
	}
	// Bare date words anywhere in the head line.
	if it.Due == "" {
		if iso, rest, ok := scanBareDate(firstLine(combined), now); ok {
			it.Due = iso
			combined = rest + tail(combined)
		}
	}

	combined = collapseSpaces(combined)
	it.Tags = Tags(combined)
	it.Links = Links(combined)

	parts := strings.SplitN(combined, "\n", 2)
	it.Title = strings.TrimSpace(parts[0])
	if len(parts) == 2 {
		it.Body = strings.TrimSpace(parts[1])
	}
	if it.Title == "" && it.Body != "" {
		p := strings.SplitN(it.Body, "\n", 2)
		it.Title, it.Body = strings.TrimSpace(p[0]), ""
		if len(p) == 2 {
			it.Body = strings.TrimSpace(p[1])
		}
	}
	if it.Title == "" {
		return it, false
	}

	it.IsTask = forcedTask || it.Done || looksLikeTask(it, now)
	return it, true
}

func looksLikeTask(it Item, now time.Time) bool {
	fields := strings.Fields(strings.TrimLeft(it.Title, "#[] "))
	if len(fields) > 0 {
		first := strings.TrimRight(strings.ToLower(fields[0]), ":,.")
		if actionVerbs[first] {
			return true
		}
	}
	if it.Due != "" && wordCount(it.Title) <= 12 {
		return true
	}
	return false
}

func priorityValue(s string) int {
	switch strings.ToLower(s) {
	case "high", "hi", "3", "p3":
		return 3
	case "med", "medium", "mid", "2", "p2":
		return 2
	case "low", "lo", "1", "p1":
		return 1
	}
	return 0
}

// scanBareDate looks for a 1- or 2-word date at the start or end of a single
// line and, if found, returns the line with that fragment removed.
func scanBareDate(line string, now time.Time) (iso, rest string, ok bool) {
	toks := strings.Fields(line)
	if len(toks) == 0 {
		return "", line, false
	}
	try := func(cand string, drop int, fromEnd bool) (string, string, bool) {
		if iso, ok := ParseDate(cand, now); ok {
			if fromEnd {
				return iso, strings.Join(toks[:len(toks)-drop], " "), true
			}
			return iso, strings.Join(toks[drop:], " "), true
		}
		return "", "", false
	}
	if len(toks) >= 2 {
		if iso, r, ok := try(toks[len(toks)-2]+" "+toks[len(toks)-1], 2, true); ok {
			return iso, r, true
		}
	}
	if iso, r, ok := try(toks[len(toks)-1], 1, true); ok {
		return iso, r, true
	}
	if len(toks) >= 2 {
		if iso, r, ok := try(toks[0]+" "+toks[1], 2, false); ok {
			return iso, r, true
		}
	}
	return "", line, false
}

func nonEmptyLines(block string) []string {
	var out []string
	for _, ln := range strings.Split(block, "\n") {
		if strings.TrimSpace(ln) != "" {
			out = append(out, strings.TrimRight(ln, " \t"))
		}
	}
	return out
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

func tail(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[i:]
	}
	return ""
}

func collapseSpaces(s string) string {
	lines := strings.Split(s, "\n")
	for i, ln := range lines {
		lines[i] = strings.TrimSpace(regexp.MustCompile(`[ \t]{2,}`).ReplaceAllString(ln, " "))
	}
	return strings.Join(lines, "\n")
}

func wordCount(s string) int { return len(strings.Fields(s)) }
