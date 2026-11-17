package asciidoc

import (
	"regexp"
	"strconv"
	"strings"
)

// block is a run of Markdown lines; blocks are separated by blank lines.
type block []string

func join(blocks []block) string {
	var parts []string
	for _, b := range blocks {
		if len(b) > 0 {
			parts = append(parts, strings.Join(b, "\n"))
		}
	}
	return strings.Join(parts, "\n\n") + "\n"
}

var (
	attrListLine = regexp.MustCompile(`^\[(.*)\]\s*$`)
	anchorLine   = regexp.MustCompile(`^\[\[([^\],]+)(?:,[^\]]*)?\]\]\s*$`)
	titleLine    = regexp.MustCompile(`^\.([^\s.].*)$`)
	admonPara    = regexp.MustCompile(`^(NOTE|TIP|IMPORTANT|WARNING|CAUTION):[ \t]+(.*)$`)
	blockMacro   = regexp.MustCompile(`^(image|video|audio|toc|include)::([^\[]*)\[(.*)\]\s*$`)
	listMarker   = regexp.MustCompile(`^[ \t]*(\*{1,6}|-|\.{1,6}|\d+\.|<(?:\d+|\.)>)[ \t]+(\S.*)$`)
	dlistItem    = regexp.MustCompile(`^[ \t]*(\S.*?)(:{2,4}|;;)(?:[ \t]+(.*))?$`)
	mdStart      = regexp.MustCompile(`^(#{1,6}[ \t]|#{1,6}$|>|[-+][ \t]|\d{1,9}[.)][ \t])`)
	// As Asciidoctor makes a section's id of its title: what is no
	// letter, digit, "_", space, "." or "-" is left out, then runs of
	// spaces, "." and "-" are one "_".
	idDrop = regexp.MustCompile(`[^\pL\pN_ .\-]+`)
	idSep  = regexp.MustCompile(`[ .\-]+`)
)

var admonitions = map[string]string{"NOTE": "Note", "TIP": "Tip", "IMPORTANT": "Important", "WARNING": "Warning", "CAUTION": "Caution"}

// meta is what precedes a block: its attribute lists, its anchor, its
// title.
type meta struct {
	id      string
	title   string
	pos     []string          // positional attributes; the first is the style
	named   map[string]string // name=value
	options map[string]bool   // %option, options="a,b"
}

func (m *meta) style() string {
	if len(m.pos) > 0 {
		return m.pos[0]
	}
	return ""
}

// attrs reads an attribute list: "style#id.role%option, second, key=value".
func (m *meta) attrs(list string) {
	if m.named == nil {
		m.named, m.options = map[string]string{}, map[string]bool{}
	}
	first := true
	for _, a := range splitAttrs(list) {
		a = strings.TrimSpace(a)
		if k, v, ok := strings.Cut(a, "="); ok && !strings.ContainsAny(k, " \"") {
			v = unquote(strings.TrimSpace(v))
			k = strings.TrimSpace(k)
			m.named[k] = v
			switch k {
			case "id":
				m.id = v
			case "options", "opts":
				for _, o := range strings.Split(v, ",") {
					m.options[strings.TrimSpace(o)] = true
				}
			}
			first = false
			continue
		}
		if first {
			// The first: a style with shorthand #id .role %option.
			style := a
			if i := strings.IndexAny(a, "#.%"); i >= 0 {
				style = a[:i]
				for _, part := range shorthand.FindAllString(a[i:], -1) {
					switch part[0] {
					case '#':
						m.id = part[1:]
					case '%':
						m.options[part[1:]] = true
					}
				}
			}
			// A list that only names an id or an option keeps the style
			// of the list before it ("[source,yaml]", then "[#id]").
			switch style = unquote(style); {
			case len(m.pos) == 0:
				m.pos = []string{style}
			case style != "":
				m.pos[0] = style
			}
			first = false
			continue
		}
		// The second positional: a source block's language.
		if len(m.pos) == 1 {
			m.pos = append(m.pos, unquote(a))
		} else if len(m.pos) > 1 && a != "" {
			m.pos = append(m.pos, unquote(a))
		}
	}
}

var shorthand = regexp.MustCompile(`[#.%][^#.%]+`)

// splitAttrs splits an attribute list at its commas, not at those in
// quotes.
func splitAttrs(list string) []string {
	var out []string
	var cur strings.Builder
	quote := byte(0)
	for i := 0; i < len(list); i++ {
		c := list[i]
		switch {
		case quote != 0:
			if c == quote {
				quote = 0
			}
			cur.WriteByte(c)
		case c == '"' || c == '\'':
			quote = c
			cur.WriteByte(c)
		case c == ',':
			out = append(out, cur.String())
			cur.Reset()
		default:
			cur.WriteByte(c)
		}
	}
	return append(out, cur.String())
}

// heading is a section title of a page.
type heading struct {
	level     int
	id, title string
	// auto is the id Asciidoctor makes of the title, where the section
	// has another: links are written to either.
	auto string
}

// renderer turns a page's preprocessed lines into Markdown blocks.
type renderer struct {
	in *inliner
	// ids are the section ids given, to keep them apart.
	ids      map[string]int
	headings []heading
	// unknown counts what the renderer met and does not know, by what it
	// is.
	unknown map[string]int
	// anchors are the ids of blocks that are no sections, each with the
	// id of the section it is in; current is that section while
	// rendering.
	anchors map[string]string
	current string
}

// delimiter reports whether a line opens a delimited block, and which.
func delimiter(l string) (kind string, ok bool) {
	t := strings.TrimRight(l, " \t")
	switch {
	case strings.HasPrefix(t, "```"):
		return "```", true
	case t == "--":
		return "open", true
	case len(t) >= 4 && (t[0] == '|' || t[0] == '!' || t[0] == ',' || t[0] == ':') && strings.Trim(t[1:], "=") == "":
		return "table", true
	case len(t) < 4:
		return "", false
	}
	if strings.Trim(t, string(t[0])) != "" {
		return "", false
	}
	switch t[0] {
	case '-':
		return "listing", true
	case '.':
		return "literal", true
	case '=':
		return "example", true
	case '*':
		return "sidebar", true
	case '_':
		return "quote", true
	case '+':
		return "pass", true
	}
	return "", false
}

// closing returns the index of the line that closes the block opened at
// lines[i], or len(lines).
func closing(lines []string, i int) int {
	open := strings.TrimRight(lines[i], " \t")
	if strings.HasPrefix(open, "```") {
		for j := i + 1; j < len(lines); j++ {
			if strings.TrimRight(lines[j], " \t") == "```" {
				return j
			}
		}
		return len(lines)
	}
	for j := i + 1; j < len(lines); j++ {
		if strings.TrimRight(lines[j], " \t") == open {
			return j
		}
	}
	return len(lines)
}

// blocks renders lines as blocks; level is that of the page's title in
// the Markdown (1).
func (r *renderer) blocks(lines []string) []block {
	var out []block
	var m meta
	for i := 0; i < len(lines); {
		l := lines[i]
		if strings.TrimSpace(l) == "" {
			i++
			continue
		}
		// What precedes a block.
		if a := anchorLine.FindStringSubmatch(l); a != nil {
			m.id = a[1]
			i++
			continue
		}
		if a := attrListLine.FindStringSubmatch(l); a != nil && !strings.HasPrefix(l, "[[") {
			m.attrs(a[1])
			i++
			continue
		}
		if t := titleLine.FindStringSubmatch(l); t != nil && i+1 < len(lines) && strings.TrimSpace(lines[i+1]) != "" {
			m.title = t[1]
			i++
			continue
		}
		var b []block
		switch kind, isDelim := delimiter(l); {
		case headingLine.MatchString(l) && m.style() != "discrete" && m.style() != "float":
			h := headingLine.FindStringSubmatch(l)
			b = []block{r.heading(len(h[1]), h[2], m.id)}
			i++
		case headingLine.MatchString(l):
			h := headingLine.FindStringSubmatch(l)
			b = []block{{"**" + r.in.inline(h[2]) + "**"}}
			i++
		case isDelim:
			end := closing(lines, i)
			b = r.delimited(kind, l, lines[i+1:min(end, len(lines))], &m)
			i = end + 1
		case blockMacro.MatchString(l):
			b = r.macro(blockMacro.FindStringSubmatch(l), &m)
			i++
		case l == "'''" || l == "<<<" || l == "---" || l == "***" || continuation(l):
			i++ // a rule, a page break, or a "+" that attaches to nothing
		case isListStart(l):
			end := listEnd(lines, i)
			b = r.list(lines[i:end], &m)
			i = end
		default:
			end := i + 1
			for end < len(lines) && strings.TrimSpace(lines[end]) != "" {
				if _, d := delimiter(lines[end]); d {
					break
				}
				end++
			}
			b = r.paragraph(lines[i:end], &m)
			i = end
		}
		if m.id != "" && !headingLine.MatchString(l) && r.anchors != nil {
			r.anchors[m.id] = r.current
		}
		out = append(out, b...)
		m = meta{}
	}
	return out
}

// heading renders a section title with its id: the one given, or the
// one Asciidoctor makes of the title ("_" and the title's words).
func (r *renderer) heading(level int, title, id string) block {
	auto := strings.TrimRight(idSep.ReplaceAllString("_"+idDrop.ReplaceAllString(strings.ToLower(plainText(title)), ""), "_"), "_")
	if id == "" {
		if id = auto; id == "" {
			id = "_"
		}
	}
	if n := r.ids[id]; n > 0 {
		r.ids[id]++
		id += "_" + strconv.Itoa(n+1)
	} else {
		r.ids[id] = 1
	}
	text := r.in.inline(title)
	r.headings = append(r.headings, heading{level: level, id: id, title: plainText(title), auto: auto})
	if level == 1 {
		r.current = ""
		return block{"# " + text} // the page's title: the document is its section
	}
	r.current = id
	return block{strings.Repeat("#", level) + " " + text + " {#" + id + "}"}
}

// delimited renders a delimited block by its kind and style.
func (r *renderer) delimited(kind, open string, body []string, m *meta) []block {
	style := m.style()
	var title []block
	if m.title != "" {
		title = []block{{"**" + r.in.inline(m.title) + "**"}}
	}
	switch kind {
	case "```":
		return append(title, fenced(body, strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(open), "```"))))
	case "listing", "literal":
		lang := ""
		if (style == "source" || style == "") && len(m.pos) > 1 {
			lang = m.pos[1]
		}
		if admonitions[style] != "" {
			return r.admonition(style, m.title, []block{fenced(body, "")})
		}
		return append(title, fenced(body, lang))
	case "pass":
		return nil // HTML for a browser (an embedded viewer): nothing to read
	case "table":
		return r.table(open, body, m)
	case "quote":
		return []block{quote(append(title, r.blocks(body)...))}
	}
	// An example, a sidebar or an open block: by its style.
	switch {
	case style == "tabs":
		return r.tabs(body)
	case admonitions[style] != "":
		return r.admonition(style, m.title, r.blocks(body))
	case style == "source" || style == "listing" || style == "literal":
		lang := ""
		if len(m.pos) > 1 {
			lang = m.pos[1]
		}
		return append(title, fenced(body, lang))
	case style == "quote" || style == "verse":
		return []block{quote(append(title, r.blocks(body)...))}
	case kind == "example" && !m.options["collapsible"] && m.title != "":
		return append([]block{{"**Example: " + r.in.inline(m.title) + "**"}}, r.blocks(body)...)
	}
	return append(title, r.blocks(body)...)
}

func (r *renderer) admonition(style, title string, body []block) []block {
	label := admonitions[style]
	if title != "" {
		label += ": " + r.in.inline(title)
	}
	return []block{quote(append([]block{{"**" + label + "**"}}, body...))}
}

// quote is blocks as a Markdown quote.
func quote(body []block) block {
	var out block
	for bi, b := range body {
		if bi > 0 {
			out = append(out, ">")
		}
		for _, l := range b {
			if l == "" {
				out = append(out, ">")
			} else {
				out = append(out, "> "+l)
			}
		}
	}
	return out
}

// fenced is a code block, its fence longer than any in the text.
func fenced(lines []string, lang string) block {
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	fence := "```"
	for _, l := range lines {
		for strings.Contains(l, fence) {
			fence += "`"
		}
	}
	if strings.ContainsAny(lang, " \t`") {
		lang = ""
	}
	out := block{fence + lang}
	out = append(out, lines...)
	return append(out, fence)
}

// tabs renders a tab set (@asciidoctor/tabs): a description list whose
// terms are the tabs. Markdown has no tabs: each is its title and its
// content, one after another.
func (r *renderer) tabs(body []string) []block {
	var out []block
	// The tabs are the list the block starts with; what follows it in
	// the block is after the tabs.
	start := 0
	for start < len(body) && strings.TrimSpace(body[start]) == "" {
		start++
	}
	end := listEnd(body, start)
	rest := body[end:]
	for _, it := range parseList(body[start:end]) {
		if it.term == "" {
			out = append(out, r.listItems([]*item{it}, 0)...)
			continue
		}
		out = append(out, block{"**" + r.in.inline(it.term) + "**"})
		if text := strings.TrimSpace(strings.Join(it.text, " ")); text != "" {
			out = append(out, block{r.in.inline(text)})
		}
		for _, a := range it.attached {
			out = append(out, r.blocks(a)...)
		}
		out = append(out, r.listItems(it.children, 0)...)
	}
	return append(out, r.blocks(rest)...)
}

func (r *renderer) macro(m []string, mt *meta) []block {
	switch m[1] {
	case "image":
		alt := firstAttr(m[3])
		title := ""
		if mt.title != "" {
			title = r.in.inline(mt.title)
		}
		switch {
		case title != "" && alt != "":
			return []block{{"*Figure: " + title + ". " + escapeText(alt) + "*"}}
		case title != "":
			return []block{{"*Figure: " + title + "*"}}
		case alt != "":
			return []block{{"*Figure: " + escapeText(alt) + "*"}}
		}
		return nil
	case "include":
		r.unknown["include (not read)"]++
	}
	return nil // a video, an audio, a table of contents: nothing to read
}

// paragraph renders a run of text lines: a paragraph, an admonition, or
// by its style a code block or a quote.
func (r *renderer) paragraph(lines []string, m *meta) []block {
	style := m.style()
	var title []block
	if m.title != "" {
		title = []block{{"**" + r.in.inline(m.title) + "**"}}
	}
	if style == "source" || style == "listing" || style == "literal" || (style == "" && len(m.pos) > 1 && m.pos[1] != "") {
		lang := ""
		if len(m.pos) > 1 {
			lang = m.pos[1]
		}
		return append(title, fenced(lines, lang))
	}
	// Indented from its first line on: literal.
	if lines[0] != strings.TrimLeft(lines[0], " \t") {
		return append(title, fenced(dedent(lines), ""))
	}
	text := r.text(lines)
	if a := admonPara.FindStringSubmatch(lines[0]); a != nil {
		rest := append([]string{a[2]}, lines[1:]...)
		return r.admonition(a[1], m.title, []block{{escapeStart(r.text(rest))}})
	}
	if admonitions[style] != "" {
		return r.admonition(style, m.title, []block{{escapeStart(text)}})
	}
	if text == "" {
		return title
	}
	if style == "quote" || style == "verse" {
		return []block{quote(append(title, block{text}))}
	}
	return append(title, block{escapeStart(text)})
}

// text is a paragraph's lines as one line of Markdown; a line that ends
// in " +" breaks there.
func (r *renderer) text(lines []string) string {
	var b strings.Builder
	for i, l := range lines {
		l = strings.TrimSpace(l)
		brk := strings.HasSuffix(l, " +")
		if brk {
			l = strings.TrimSuffix(l, " +")
		}
		b.WriteString(l)
		if i < len(lines)-1 {
			if brk {
				b.WriteString("\x04")
			} else {
				b.WriteString(" ")
			}
		}
	}
	return strings.ReplaceAll(strings.TrimSpace(r.in.inline(b.String())), "\x04", "<br>")
}

func dedent(lines []string) []string {
	n := -1
	for _, l := range lines {
		if strings.TrimSpace(l) == "" {
			continue
		}
		if ind := len(l) - len(strings.TrimLeft(l, " \t")); n < 0 || ind < n {
			n = ind
		}
	}
	out := make([]string, len(lines))
	for i, l := range lines {
		if len(l) >= n && n > 0 {
			out[i] = l[n:]
		} else {
			out[i] = strings.TrimLeft(l, " \t")
		}
	}
	return out
}

// escapeStart keeps a paragraph from being read as a heading, a quote
// or a list item where it starts like one.
func escapeStart(text string) string {
	m := mdStart.FindString(text)
	switch {
	case m == "":
		return text
	case text[0] >= '0' && text[0] <= '9':
		i := strings.IndexAny(text, ".)")
		return text[:i] + "\\" + text[i:]
	}
	return "\\" + text
}
