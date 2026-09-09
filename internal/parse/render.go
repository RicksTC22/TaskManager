package parse

import (
	"regexp"
	"strings"
)

// LinkResolver maps a [[wiki-link]] target to an href and whether the target
// already exists (so unresolved links can be styled differently).
type LinkResolver func(target string) (href string, known bool)

var (
	rHeading   = regexp.MustCompile(`^(#{1,4})\s+(.*)$`)
	rBullet    = regexp.MustCompile(`^\s*[-*+]\s+(.*)$`)
	rNumbered  = regexp.MustCompile(`^\s*\d+[.)]\s+(.*)$`)
	rCheckbox  = regexp.MustCompile(`^\s*[-*+]\s*\[( |x|X)\]\s+(.*)$`)
	rInlineTag = regexp.MustCompile(`(^|[\s(>])#([a-zA-Z][\w-]*)`)
	rWiki      = regexp.MustCompile(`\[\[([^\]]{1,120}?)\]\]`)
	rMdLink    = regexp.MustCompile(`\[([^\]]{1,200}?)\]\(([^)\s]{1,500}?)\)`)
	rAutoLink  = regexp.MustCompile(`(^|[\s(])(https?://[^\s<)]+)`)
	rBold      = regexp.MustCompile(`\*\*([^*\n]{1,200}?)\*\*`)
	rItalic    = regexp.MustCompile(`(^|[^*\w])\*([^*\n]{1,200}?)\*`)
	rCode      = regexp.MustCompile("`([^`\n]{1,300}?)`")
)

// RenderHTML turns Cairn's light markdown into a safe HTML fragment. All input
// is escaped before any markup is added.
func RenderHTML(md string, link LinkResolver) string {
	md = strings.ReplaceAll(md, "\r\n", "\n")
	lines := strings.Split(md, "\n")

	var b strings.Builder
	var list string // "ul" | "ol" | ""
	inCode := false
	para := []string{}

	flushPara := func() {
		if len(para) == 0 {
			return
		}
		b.WriteString("<p>")
		b.WriteString(inline(strings.Join(para, "<br>"), link))
		b.WriteString("</p>\n")
		para = para[:0]
	}
	closeList := func() {
		if list != "" {
			b.WriteString("</" + list + ">\n")
			list = ""
		}
	}

	for _, ln := range lines {
		if strings.HasPrefix(strings.TrimSpace(ln), "```") {
			flushPara()
			closeList()
			if inCode {
				b.WriteString("</code></pre>\n")
			} else {
				b.WriteString("<pre><code>")
			}
			inCode = !inCode
			continue
		}
		if inCode {
			b.WriteString(escape(ln))
			b.WriteString("\n")
			continue
		}
		if strings.TrimSpace(ln) == "" {
			flushPara()
			closeList()
			continue
		}
		if m := rHeading.FindStringSubmatch(ln); m != nil {
			flushPara()
			closeList()
			h := len(m[1])
			b.WriteString("<h" + itoa(h+1) + ">")
			b.WriteString(inline(escape(m[2]), link))
			b.WriteString("</h" + itoa(h+1) + ">\n")
			continue
		}
		if m := rCheckbox.FindStringSubmatch(ln); m != nil {
			flushPara()
			if list != "ul" {
				closeList()
				b.WriteString("<ul class=\"tasklist\">\n")
				list = "ul"
			}
			checked := ""
			if strings.EqualFold(m[1], "x") {
				checked = " checked"
			}
			b.WriteString("<li><input type=\"checkbox\" disabled" + checked + "> ")
			b.WriteString(inline(escape(m[2]), link))
			b.WriteString("</li>\n")
			continue
		}
		if m := rBullet.FindStringSubmatch(ln); m != nil {
			flushPara()
			if list != "ul" {
				closeList()
				b.WriteString("<ul>\n")
				list = "ul"
			}
			b.WriteString("<li>" + inline(escape(m[1]), link) + "</li>\n")
			continue
		}
		if m := rNumbered.FindStringSubmatch(ln); m != nil {
			flushPara()
			if list != "ol" {
				closeList()
				b.WriteString("<ol>\n")
				list = "ol"
			}
			b.WriteString("<li>" + inline(escape(m[1]), link) + "</li>\n")
			continue
		}
		if strings.HasPrefix(ln, "> ") {
			flushPara()
			closeList()
			b.WriteString("<blockquote>" + inline(escape(strings.TrimPrefix(ln, "> ")), link) + "</blockquote>\n")
			continue
		}
		para = append(para, escape(ln))
	}
	flushPara()
	closeList()
	if inCode {
		b.WriteString("</code></pre>\n")
	}
	return b.String()
}

// inline runs on already-escaped text.
func inline(s string, link LinkResolver) string {
	s = rCode.ReplaceAllString(s, "<code>$1</code>")

	s = rWiki.ReplaceAllStringFunc(s, func(m string) string {
		target := strings.TrimSpace(rWiki.FindStringSubmatch(m)[1])
		href, known := "#", false
		if link != nil {
			href, known = link(target)
		}
		cls := "wikilink"
		if !known {
			cls = "wikilink new"
		}
		return `<a class="` + cls + `" href="` + attr(href) + `">` + target + `</a>`
	})

	s = rMdLink.ReplaceAllStringFunc(s, func(m string) string {
		sm := rMdLink.FindStringSubmatch(m)
		text, href := sm[1], sm[2]
		if !safeURL(href) {
			return m
		}
		return `<a href="` + attr(href) + `" rel="noopener noreferrer">` + text + `</a>`
	})

	s = rAutoLink.ReplaceAllString(s, `$1<a href="$2" rel="noopener noreferrer">$2</a>`)

	s = rBold.ReplaceAllString(s, "<strong>$1</strong>")
	s = rItalic.ReplaceAllString(s, "$1<em>$2</em>")

	s = rInlineTag.ReplaceAllString(s, `$1<a class="tag" href="/tags/$2">#$2</a>`)
	return s
}

func escape(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	return s
}

func attr(s string) string {
	s = strings.ReplaceAll(s, `"`, "%22")
	s = strings.ReplaceAll(s, "<", "%3C")
	s = strings.ReplaceAll(s, ">", "%3E")
	return s
}

func safeURL(s string) bool {
	low := strings.ToLower(strings.TrimSpace(s))
	return strings.HasPrefix(low, "http://") ||
		strings.HasPrefix(low, "https://") ||
		strings.HasPrefix(low, "mailto:") ||
		strings.HasPrefix(low, "/") ||
		strings.HasPrefix(low, "#")
}

func itoa(n int) string { return string(rune('0' + n)) }
