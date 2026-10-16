package docbook

import (
	"encoding/json"
	"fmt"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Target is where an id is in the converted collection: the document
// (relative to the collection's root), the title it carries, and the
// anchor a link to it goes to: its own for a topic or section, the
// enclosing section's for an example, list or table (mcp-docs addresses
// headings).
type Target struct {
	File, Title, Anchor string
}

// renderer turns DocBook elements into Markdown lines.
type renderer struct {
	file    string            // the document being written, relative to the root
	links   map[string]Target // every id of the collection
	unknown map[string]int    // elements it does not know, by name
	broken  map[string]bool   // link targets not in the collection
}

// Inline elements, rendered within a paragraph.
var (
	codeElements = set("command", "literal", "option", "filename", "package", "systemitem", "varname",
		"envar", "code", "constant", "function", "parameter", "tag", "userinput", "computeroutput",
		"prompt", "errorcode", "errorname", "property", "type", "classname", "methodname", "uri",
		"email", "application", "database", "token", "symbol", "returnvalue")
	textElements = set("phrase", "productname", "productnumber", "citetitle", "abbrev", "acronym",
		"trademark", "firstterm", "glossterm", "guilabel", "guimenu", "guisubmenu", "guimenuitem",
		"guibutton", "foreignphrase", "wordasword", "orgname", "personname", "firstname", "surname")
	inlineElements = set("replaceable", "emphasis", "keycap", "keycombo", "xref", "link", "footnote",
		"quote", "menuchoice", "sup", "superscript", "sub", "subscript")
	// Elements that are metadata, not content.
	skipElements = set("info", "title", "titleabbrev", "subtitle", "abstract", "meta", "revhistory",
		"indexterm", "remark", "docmanager", "annotation", "alt")
)

func set(names ...string) map[string]bool {
	m := map[string]bool{}
	for _, n := range names {
		m[n] = true
	}
	return m
}

func isInline(n *Node) bool {
	return n.Name == "" || codeElements[n.Name] || textElements[n.Name] || inlineElements[n.Name]
}

var spaces = regexp.MustCompile(`\s+`)

// inline renders inline content as one line of Markdown.
func (r *renderer) inline(nodes []*Node) string {
	var b strings.Builder
	for _, n := range nodes {
		b.WriteString(r.inlineNode(n))
	}
	return strings.TrimSpace(spaces.ReplaceAllString(b.String(), " "))
}

func (r *renderer) inlineNode(n *Node) string {
	switch {
	case n.Name == "":
		return escapeText(n.Text)
	case codeElements[n.Name]:
		return codeSpan(plain(n))
	case textElements[n.Name]:
		return r.inline(n.Children)
	}
	switch n.Name {
	case "replaceable":
		return "*" + plain(n) + "*"
	case "emphasis":
		if role := n.Attr["role"]; role == "bold" || role == "strong" {
			return "**" + r.inline(n.Children) + "**"
		}
		return "*" + r.inline(n.Children) + "*"
	case "keycap":
		return codeSpan(plain(n))
	case "keycombo", "menuchoice":
		var parts []string
		for _, c := range n.Children {
			if c.Name != "" {
				parts = append(parts, strings.TrimSpace(plain(c)))
			}
		}
		sep := "+"
		if n.Name == "menuchoice" {
			sep = " > "
		}
		return codeSpan(strings.Join(parts, sep))
	case "xref":
		return r.ref(n.Attr["linkend"], "")
	case "link":
		if href := n.Attr["href"]; href != "" {
			text := r.inline(n.Children)
			if text == "" {
				return "<" + href + ">"
			}
			return "[" + text + "](" + href + ")"
		}
		return r.ref(n.Attr["linkend"], r.inline(n.Children))
	case "footnote":
		return " (" + strings.Join(r.paragraphText(n.Children), " ") + ")"
	case "quote":
		return "“" + r.inline(n.Children) + "”"
	case "sup", "superscript":
		return "^" + r.inline(n.Children)
	case "sub", "subscript":
		return "_" + r.inline(n.Children)
	}
	r.unknown[n.Name]++
	return r.inline(n.Children)
}

// ref renders a link to an id of the collection, with its title unless
// a text is given.
func (r *renderer) ref(id, text string) string {
	t, ok := r.links[id]
	if !ok {
		r.broken[id] = true
		if text == "" {
			text = id
		}
		return text
	}
	if text == "" {
		text = t.Title
	}
	rel := relPath(path.Dir(r.file), t.File)
	if t.File == r.file {
		rel = ""
	}
	return "[" + text + "](" + rel + "#" + t.Anchor + ")"
}

func relPath(fromDir, to string) string {
	from := strings.Split(path.Clean(fromDir), "/")
	if fromDir == "." || fromDir == "" {
		from = nil
	}
	parts := strings.Split(to, "/")
	i := 0
	for i < len(from) && i < len(parts)-1 && from[i] == parts[i] {
		i++
	}
	up := strings.Repeat("../", len(from)-i)
	return up + strings.Join(parts[i:], "/")
}

// plain is the text of an element, markup dropped.
func plain(n *Node) string {
	if n.Name == "" {
		return n.Text
	}
	var b strings.Builder
	for _, c := range n.Children {
		b.WriteString(plain(c))
	}
	return b.String()
}

func codeSpan(s string) string {
	s = strings.TrimSpace(spaces.ReplaceAllString(s, " "))
	if s == "" {
		return ""
	}
	fence := "`"
	for strings.Contains(s, fence) {
		fence += "`"
	}
	if strings.HasPrefix(s, "`") || strings.HasSuffix(s, "`") {
		return fence + " " + s + " " + fence
	}
	return fence + s + fence
}

// escapeText keeps text from being read as Markdown markup: agents read
// the Markdown as it is, so only what would change meaning is escaped
// (emphasis stars, backticks, backslashes); a "#" or list marker never
// starts a line (text is joined into paragraph lines), and an
// underscore within a word is no emphasis.
func escapeText(s string) string {
	r := strings.NewReplacer("\\", "\\\\", "*", "\\*", "`", "\\`")
	return r.Replace(s)
}

// block is a run of Markdown lines; blocks are separated by blank lines.
type block []string

// blocks renders the children of an element as blocks: runs of inline
// content become paragraphs.
func (r *renderer) blocks(nodes []*Node, level int) []block {
	var out []block
	var run []*Node
	flush := func() {
		if text := r.inline(run); text != "" {
			out = append(out, block{text})
		}
		run = nil
	}
	for _, n := range nodes {
		if isInline(n) {
			run = append(run, n)
			continue
		}
		flush()
		out = append(out, r.block(n, level)...)
	}
	flush()
	return out
}

// paragraphText is the text of blocks, each block one string.
func (r *renderer) paragraphText(nodes []*Node) []string {
	var out []string
	for _, b := range r.blocks(nodes, 6) {
		out = append(out, strings.Join(b, " "))
	}
	return out
}

func (r *renderer) title(n *Node) string {
	if t := n.Child("title"); t != nil {
		return r.inline(t.Children)
	}
	if info := n.Child("info"); info != nil {
		if t := info.Child("title"); t != nil {
			return r.inline(t.Children)
		}
	}
	return ""
}

func (r *renderer) block(n *Node, level int) []block {
	switch {
	case skipElements[n.Name]:
		return nil
	}
	switch n.Name {
	case "para", "simpara":
		return r.blocks(n.Children, level)
	case "formalpara":
		out := []block{}
		if t := r.title(n); t != "" {
			out = append(out, block{"**" + t + "**"})
		}
		return append(out, r.blocks(n.Children, level)...)
	case "section", "topic", "sect1", "sect2", "sect3", "simplesect", "chapter", "appendix", "article", "legalnotice":
		return r.section(n, level+1)
	case "itemizedlist", "stepalternatives":
		return r.list(n, "listitem", func(int) string { return "- " }, level)
	case "orderedlist":
		return r.list(n, "listitem", func(i int) string { return strconv.Itoa(i+1) + ". " }, level)
	case "procedure", "substeps":
		out := []block{}
		if t := r.title(n); t != "" {
			out = append(out, block{"**" + t + "**"})
		}
		return append(out, r.list(n, "step", func(i int) string { return strconv.Itoa(i+1) + ". " }, level)...)
	case "variablelist":
		var out []block
		if t := r.title(n); t != "" {
			out = append(out, block{"**" + t + "**"})
		}
		for _, e := range n.Elements("varlistentry") {
			var terms []string
			for _, t := range e.Elements("term") {
				terms = append(terms, r.inline(t.Children))
			}
			body := []block{{"**" + strings.Join(terms, ", ") + "**"}}
			if li := e.Child("listitem"); li != nil {
				body = append(body, r.blocks(li.Children, level)...)
			}
			out = append(out, item("- ", body))
		}
		return out
	case "screen", "programlisting", "synopsis", "literallayout", "cmdsynopsis":
		return []block{fenced(plain(n), n.Attr["language"])}
	case "note", "tip", "important", "warning", "caution":
		label := strings.ToUpper(n.Name[:1]) + n.Name[1:]
		if t := r.title(n); t != "" {
			label += ": " + t
		}
		body := append([]block{{"**" + label + "**"}}, r.blocks(n.Children, level)...)
		return []block{quote(body)}
	case "example", "informalexample":
		out := []block{}
		if t := r.title(n); t != "" {
			out = append(out, block{"**Example: " + t + "**"})
		}
		return append(out, r.blocks(n.Children, level)...)
	case "figure", "informalfigure", "mediaobject", "inlinemediaobject":
		var alt string
		n.Walk(func(c *Node) {
			if alt == "" && (c.Name == "textobject" || c.Name == "alt") {
				alt = strings.TrimSpace(spaces.ReplaceAllString(plain(c), " "))
			}
		})
		t := r.title(n)
		switch {
		case t != "" && alt != "":
			return []block{{"*Figure: " + t + ". " + alt + "*"}}
		case t != "":
			return []block{{"*Figure: " + t + "*"}}
		case alt != "":
			return []block{{"*Figure: " + alt + "*"}}
		}
		return nil
	case "table", "informaltable":
		return r.table(n)
	case "blockquote":
		return []block{quote(r.blocks(n.Children, level))}
	case "bridgehead":
		return []block{{"**" + r.inline(n.Children) + "**"}}
	case "listitem", "step", "textobject":
		return r.blocks(n.Children, level)
	}
	r.unknown[n.Name]++
	return r.blocks(n.Children, level)
}

// section renders a heading with its id as an explicit anchor, then
// the section's abstract and content.
func (r *renderer) section(n *Node, level int) []block {
	h := strings.Repeat("#", min(level, 6)) + " " + r.title(n)
	if id := n.Attr["id"]; id != "" {
		h += " {#" + id + "}"
	}
	out := []block{{h}}
	if info := n.Child("info"); info != nil {
		if a := info.Child("abstract"); a != nil {
			out = append(out, r.blocks(a.Children, level)...)
		}
	}
	return append(out, r.blocks(n.Children, level)...)
}

func (r *renderer) list(n *Node, itemName string, marker func(int) string, level int) []block {
	var out []block
	i := 0
	for _, c := range n.Children {
		if c.Name != itemName {
			continue
		}
		out = append(out, item(marker(i), r.blocks(c.Children, level)))
		i++
	}
	return out
}

// item renders a list item: the marker on its first line, everything
// else indented under it.
func item(marker string, body []block) block {
	indent := strings.Repeat(" ", len(marker))
	var out block
	for bi, b := range body {
		if bi > 0 {
			out = append(out, "")
		}
		for li, l := range b {
			switch {
			case bi == 0 && li == 0:
				out = append(out, marker+l)
			case l == "":
				out = append(out, "")
			default:
				out = append(out, indent+l)
			}
		}
	}
	if len(out) == 0 {
		out = block{strings.TrimRight(marker, " ")}
	}
	return out
}

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

func fenced(text, lang string) block {
	lines := strings.Split(text, "\n")
	for i := range lines {
		lines[i] = strings.TrimRight(lines[i], " \t")
	}
	// Blank lines at either end are layout of the source, not content.
	for len(lines) > 0 && lines[0] == "" {
		lines = lines[1:]
	}
	for len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	fence := "```"
	for strings.Contains(text, fence) {
		fence += "`"
	}
	out := block{fence + lang}
	out = append(out, lines...)
	return append(out, fence)
}

// table renders a table as a pipe table, a cell's blocks joined.
func (r *renderer) table(n *Node) []block {
	var rows [][]string
	head := 0
	n.Walk(func(c *Node) {
		if c.Name != "row" && c.Name != "tr" {
			return
		}
		var cells []string
		for _, e := range c.Children {
			if e.Name == "entry" || e.Name == "td" || e.Name == "th" {
				cell := strings.Join(r.paragraphText(e.Children), " ")
				cells = append(cells, strings.ReplaceAll(cell, "|", "\\|"))
			}
		}
		rows = append(rows, cells)
	})
	if thead := findFirst(n, "thead"); thead != nil {
		thead.Walk(func(c *Node) {
			if c.Name == "row" || c.Name == "tr" {
				head++
			}
		})
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
	var out block
	if t := r.title(n); t != "" {
		out = append(out, "**"+t+"**", "")
	}
	if head == 0 {
		out = append(out, line(make([]string, width)))
	} else {
		out = append(out, line(rows[0]))
		rows = rows[1:]
	}
	sep := make([]string, width)
	for i := range sep {
		sep[i] = "---"
	}
	out = append(out, line(sep))
	for _, row := range rows {
		out = append(out, line(row))
	}
	return []block{out}
}

func findFirst(n *Node, name string) *Node {
	var found *Node
	n.Walk(func(c *Node) {
		if found == nil && c.Name == name {
			found = c
		}
	})
	return found
}

func join(blocks []block) string {
	var parts []string
	for _, b := range blocks {
		parts = append(parts, strings.Join(b, "\n"))
	}
	return strings.Join(parts, "\n\n") + "\n"
}

// frontMatter renders YAML front matter; values are JSON strings and
// arrays, which YAML reads as they are.
func frontMatter(fields [][2]any) string {
	var b strings.Builder
	b.WriteString("---\n")
	for _, f := range fields {
		v, _ := json.Marshal(f[1])
		fmt.Fprintf(&b, "%s: %s\n", f[0], v)
	}
	b.WriteString("---\n\n")
	return b.String()
}

func sortedKeys(m map[string]int) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
