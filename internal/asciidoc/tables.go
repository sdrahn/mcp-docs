package asciidoc

import (
	"regexp"
	"strconv"
	"strings"
)

// cell is a table cell as written: what stands before its "|" (how
// many columns and rows it spans, how often it repeats, its style) and
// its text.
type cell struct {
	spec string
	text string
}

var (
	spanSpec  = regexp.MustCompile(`(^|[ \t])((?:\d+(?:\.\d+)?|\.\d+)[+*])([<^>]?(?:\.[<^>])?[adehlmsv]?)$`)
	styleSpec = regexp.MustCompile(`^[ \t]*([<^>]?(?:\.[<^>])?[adehlmsv]?)$`)
	specParts = regexp.MustCompile(`^(?:(\d+)?(?:\.(\d+))?([+*]))?[<^>]?(?:\.[<^>])?([adehlmsv])?$`)
)

// cells splits a table's lines at their separators; a cell goes on
// until the next.
func cells(body []string, sep byte) (out []cell, firstLine int) {
	var buf strings.Builder
	flush := func() {
		if len(out) > 0 {
			out[len(out)-1].text = buf.String()
		}
		buf.Reset()
	}
	firstLine = -1
	for _, line := range body {
		rest := line
		for {
			idx := -1
			for i := 0; i < len(rest); i++ {
				if rest[i] == sep && (i == 0 || rest[i-1] != '\\') {
					idx = i
					break
				}
			}
			if idx < 0 {
				buf.WriteString(rest + "\n")
				break
			}
			before, spec := rest[:idx], ""
			if m := spanSpec.FindStringSubmatch(before); m != nil {
				spec = m[2] + m[3]
				before = before[:len(before)-len(spec)]
			} else if m := styleSpec.FindStringSubmatch(before); m != nil && m[1] != "" {
				spec, before = m[1], ""
			}
			buf.WriteString(before)
			flush()
			out = append(out, cell{spec: spec})
			rest = rest[idx+1:]
		}
		if firstLine < 0 && len(out) > 0 && strings.TrimSpace(line) != "" {
			firstLine = len(out)
		}
	}
	flush()
	return out, firstLine
}

// table renders a table as a pipe table: a cell's text on one line, a
// cell that spans columns or rows in its first place, the others it
// covers empty.
func (r *renderer) table(open string, body []string, m *meta) []block {
	var out block
	if m.title != "" {
		out = append(out, "**"+r.in.inline(m.title)+"**", "")
	}
	var rows [][]string
	header := m.options["header"]
	switch format := m.named["format"]; {
	case open[0] == ',' || format == "csv", open[0] == ':' || format == "dsv":
		sep := ","
		if open[0] == ':' || format == "dsv" {
			sep = ":"
		}
		if s := m.named["separator"]; s != "" {
			sep = s
		}
		for _, l := range body {
			if strings.TrimSpace(l) == "" {
				continue
			}
			var row []string
			for _, c := range strings.Split(l, sep) {
				row = append(row, r.cellText(strings.Trim(strings.TrimSpace(c), `"`), ""))
			}
			rows = append(rows, row)
		}
	default:
		sep := open[0]
		cs, firstLine := cells(body, sep)
		if len(cs) == 0 {
			return nil
		}
		width := columns(m.named["cols"])
		if width == 0 {
			// As many columns as the first line has cells.
			for _, c := range cs[:max(firstLine, 1)] {
				span, _, repeat, _ := spec(c.spec)
				width += span * repeat
			}
		}
		// The first row is a header if the table says so, or if it is on
		// one line and a blank line follows.
		if !header && !m.options["noheader"] && len(body) > 1 && strings.TrimSpace(body[0]) != "" && strings.TrimSpace(body[1]) == "" {
			header = true
		}
		var row []string
		down := map[int]int{}
		place := func(text string, span, more int) {
			for down[len(row)] > 0 {
				down[len(row)]--
				row = append(row, "")
			}
			for i := range span {
				if more > 0 {
					down[len(row)] = more
				}
				if i == 0 {
					row = append(row, text)
				} else {
					row = append(row, "")
				}
			}
			if len(row) >= width {
				rows = append(rows, row)
				row = nil
			}
		}
		for _, c := range cs {
			span, more, repeat, style := spec(c.spec)
			text := r.cellText(strings.ReplaceAll(c.text, `\`+string(sep), string(sep)), style)
			for range repeat {
				place(text, span, more)
			}
		}
		if len(row) > 0 {
			rows = append(rows, row)
		}
	}
	if len(rows) == 0 {
		return nil
	}
	width := 0
	for _, row := range rows {
		width = max(width, len(row))
	}
	line := func(cells []string) string {
		for len(cells) < width {
			cells = append(cells, "")
		}
		return "| " + strings.Join(cells, " | ") + " |"
	}
	if header {
		out = append(out, line(rows[0]))
		rows = rows[1:]
	} else {
		out = append(out, line(make([]string, width)))
	}
	rule := make([]string, width)
	for i := range rule {
		rule[i] = "---"
	}
	out = append(out, line(rule))
	for _, row := range rows {
		out = append(out, line(row))
	}
	return []block{out}
}

// columns is the number of columns a cols attribute names: "1,2,3",
// "3*", "3".
func columns(cols string) int {
	cols = strings.TrimSpace(cols)
	if cols == "" {
		return 0
	}
	n := 0
	for _, c := range strings.FieldsFunc(cols, func(r rune) bool { return r == ',' || r == ';' }) {
		c = strings.TrimSpace(c)
		if i := strings.IndexByte(c, '*'); i > 0 {
			if k, err := strconv.Atoi(c[:i]); err == nil {
				n += k
				continue
			}
		}
		n++
	}
	if n == 1 {
		if k, err := strconv.Atoi(cols); err == nil {
			return k
		}
	}
	return n
}

// spec reads a cell's specifier: the columns it spans, the rows more it
// covers, how often it repeats, and its style.
func spec(s string) (span, more, repeat int, style string) {
	span, repeat = 1, 1
	m := specParts.FindStringSubmatch(strings.TrimSpace(s))
	if m == nil {
		return
	}
	style = m[4]
	a, _ := strconv.Atoi(m[1])
	b, _ := strconv.Atoi(m[2])
	switch m[3] {
	case "*":
		if a > 1 {
			repeat = min(a, 100)
		}
	case "+":
		if a > 1 {
			span = min(a, 100)
		}
		if b > 1 {
			more = min(b-1, 1000)
		}
	}
	return
}

// cellText is a cell's content on one line: its paragraphs, lists and
// code, one after another.
func (r *renderer) cellText(text, style string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	var parts []string
	switch style {
	case "l", "m":
		parts = []string{codeSpan(strings.Join(strings.Fields(text), " "))}
	case "a":
		for _, b := range r.blocks(strings.Split(text, "\n")) {
			parts = append(parts, flat(b))
		}
	default:
		// Paragraphs, and lists as their items.
		for _, para := range strings.Split(text, "\n\n") {
			var lines []string
			for _, l := range strings.Split(para, "\n") {
				if l = strings.TrimSpace(l); l != "" {
					lines = append(lines, l)
				}
			}
			if len(lines) == 0 {
				continue
			}
			if isListStart(lines[0]) {
				parts = append(parts, flat(r.listItems(parseList(lines), 0)[0]))
			} else {
				parts = append(parts, r.text(lines))
			}
		}
	}
	out := strings.Join(parts, " ")
	switch style {
	case "h", "s":
		out = "**" + out + "**"
	case "e":
		out = "*" + out + "*"
	}
	return strings.ReplaceAll(out, "|", "\\|")
}

// flat is a block on one line: a code block as a code span, the items
// of a list and the lines of a quote after one another.
func flat(b block) string {
	if len(b) >= 2 && strings.HasPrefix(b[0], "```") {
		var code []string
		for _, l := range b[1 : len(b)-1] {
			if l = strings.TrimSpace(l); l != "" {
				code = append(code, l)
			}
		}
		return codeSpan(strings.Join(code, "; "))
	}
	var parts []string
	for _, l := range b {
		if l = strings.TrimSpace(l); l != "" && l != ">" {
			parts = append(parts, l)
		}
	}
	return strings.Join(parts, " ")
}
