// Package markdown scans a Markdown document in one streamed pass for
// what an agent needs to read it in parts: its headings with their ids
// and the extent of their sections, and the byte offset of every
// OffsetEvery-th line so that a range of lines is read without reading
// from the start (docs/architecture.md, §5.2 and §7).
//
// Heading recognition is mcp-gateway's outline_file, unchanged: ATX
// headings (# to ######, up to 3 spaces of indent, closing #s removed,
// empty ones ignored); nothing inside a fenced code block (backticks or
// tildes, closed by a fence of the same character at least as long) is
// a heading; setext headings are not recognised, as a line of dashes is
// as often a rule. A YAML front matter block at the start of the file
// is skipped.
package markdown

import (
	"bufio"
	"context"
	"io"
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"gopkg.in/yaml.v3"
)

// OffsetEvery is the distance in lines between two stored line offsets.
const OffsetEvery = 256

// maxFrontMatter bounds the lines of a front matter block: a "---" on
// the first line that no other closes this soon is a rule.
const maxFrontMatter = 200

// MaxTitle cuts long heading titles.
const MaxTitle = 400

// Heading is a heading and the extent of its section, which runs to the
// next heading of the same or a higher level.
type Heading struct {
	Level int    `json:"level"`
	Title string `json:"title"`
	ID    string `json:"id"`
	Line  int    `json:"line"`
	Lines int    `json:"lines"`
	Bytes int64  `json:"bytes"`
	// Item marks a list item or bold-titled paragraph taken as a section
	// (Options.ListItems), not a heading of the document.
	Item bool `json:"item,omitempty"`
	// Parent is the index of the enclosing heading in Outline.Headings,
	// -1 for none.
	Parent int `json:"-"`
	start  int64
}

// Options change what a scan takes as sections.
type Options struct {
	// ListItems takes the items of a section that open with a bold title
	// as sections one level below it: top-level list items
	// ("27. **Title** …", "- **Title** …") and paragraphs that open with
	// one ("**D19 — Title.**"), after a blank line. A bold label ending
	// in a colon ("**Note:**") is not a title. For documents that keep
	// steps, decisions or entries as list items under one heading
	// (docs/architecture.md, §11 step 5).
	ListItems bool
}

// End is the section's last line.
func (h Heading) End() int { return h.Line + h.Lines - 1 }

// Outline is what a scan finds.
type Outline struct {
	Headings []Heading
	Lines    int
	Bytes    int64
	// Offsets[k] is the byte offset of line k*OffsetEvery+1.
	Offsets []int64
	// FrontMatter is the number of lines of the front matter block (0:
	// none).
	FrontMatter int
	// Meta is what the front matter says of the document.
	Meta Meta
	// Summary is the document's first paragraph, links reduced to their
	// text, cut to SummaryLength.
	Summary string
}

// Meta are the front matter fields mcp-docs reads; others are ignored.
type Meta struct {
	Title       string   `yaml:"title"`
	Description string   `yaml:"description"`
	Tags        []string `yaml:"tags"`
	// Unranked ("rank: false") keeps the document out of find_sections'
	// ranking: a license, a legal notice, a changelog. It is still
	// listed, searched and read.
	Unranked bool `yaml:"-"`
}

// SummaryLength bounds a summary.
const SummaryLength = 200

// Title is the document's title: its front matter's, else its first
// level-1 heading, else its first heading ("" without any).
func (o *Outline) Title() string {
	if o.Meta.Title != "" {
		return o.Meta.Title
	}
	for _, h := range o.Headings {
		if h.Level == 1 {
			return h.Title
		}
	}
	if len(o.Headings) > 0 {
		return o.Headings[0].Title
	}
	return ""
}

// Description is the document's description: its front matter's, else
// its first paragraph.
func (o *Outline) Description() string {
	if o.Meta.Description != "" {
		return cutWords(strings.Join(strings.Fields(o.Meta.Description), " "), SummaryLength)
	}
	return o.Summary
}

var (
	atxHeading  = regexp.MustCompile(`^ {0,3}(#{1,6})(?:[ \t]+(.*?))?[ \t]*$`)
	closingHash = regexp.MustCompile(`(?:^|[ \t]+)#+$`)
	codeFence   = regexp.MustCompile("^ {0,3}(`{3,}|~{3,})")
	explicitID  = regexp.MustCompile(`[ \t]*\{#([^\s{}]+)\}$`)
	anchorTag   = regexp.MustCompile(`(?i)<a\s+(?:[^>]*?\s)?(?:id|name)\s*=\s*["']([^"']+)["'][^>]*>\s*(?:</a>)?`)
	htmlTag     = regexp.MustCompile(`</?[A-Za-z][^>]*>`)
	link        = regexp.MustCompile(`!?\[([^\]]*)\]\([^)]*\)`)
	// A top-level list item opening with a bold title, numbered or not.
	listItem = regexp.MustCompile(`^(?:(\d+)[.)]|[-*+])[ \t]+\*\*([^*]+?)\*\*`)
	// A paragraph opening with a bold title.
	boldPara = regexp.MustCompile(`^\*\*([^*]+?)\*\*`)
)

// Scan reads a document from r and returns its outline. ctx ends a long
// scan.
func Scan(ctx context.Context, r io.Reader) (*Outline, error) {
	return ScanWith(ctx, r, Options{})
}

// ScanWith is Scan with options.
func ScanWith(ctx context.Context, r io.Reader, opts Options) (*Outline, error) {
	sc := &scanner{out: &Outline{}, ids: map[string]int{}, opts: opts, blank: true}
	br := bufio.NewReader(r)
	var pending []string // lines of a possible front matter block
	inFront := false
	for {
		line, err := br.ReadString('\n')
		if len(line) > 0 {
			if sc.n%4096 == 0 && ctx.Err() != nil {
				return nil, ctx.Err()
			}
			l := strings.TrimRight(line, "\r\n")
			switch {
			case sc.n == 0 && len(pending) == 0 && l == "---":
				inFront = true
				pending = append(pending, line)
			case inFront:
				pending = append(pending, line)
				if l == "---" || l == "..." {
					for _, p := range pending {
						sc.skip(p)
					}
					sc.out.FrontMatter = len(pending)
					sc.out.Meta = parseMeta(pending[1 : len(pending)-1])
					pending, inFront = nil, false
				} else if len(pending) > maxFrontMatter {
					sc.replay(pending)
					pending, inFront = nil, false
				}
			default:
				sc.line(line)
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
	}
	sc.replay(pending) // an unclosed front matter block is text
	sc.end(1)
	sc.endParagraph()
	sc.dropShortItems()
	sc.itemIDs()
	return sc.out, nil
}

// parseMeta reads the fields of a front matter block; a block that is
// not YAML, or fields of other types, give nothing.
func parseMeta(lines []string) Meta {
	var raw map[string]any
	if yaml.Unmarshal([]byte(strings.Join(lines, "")), &raw) != nil {
		return Meta{}
	}
	var m Meta
	if t, ok := raw["title"].(string); ok {
		m.Title = strings.TrimSpace(t)
	}
	if d, ok := raw["description"].(string); ok {
		m.Description = strings.TrimSpace(d)
	}
	if r, ok := raw["rank"].(bool); ok && !r {
		m.Unranked = true
	}
	switch t := raw["tags"].(type) {
	case []any:
		for _, x := range t {
			if s, ok := x.(string); ok {
				m.Tags = append(m.Tags, s)
			}
		}
	case string:
		for _, x := range strings.FieldsFunc(t, func(r rune) bool { return r == ',' || r == ' ' }) {
			m.Tags = append(m.Tags, x)
		}
	}
	return m
}

type scanner struct {
	out    *Outline
	open   []int  // sections not yet ended, by rising level
	fence  string // the open code fence, if any
	n      int    // lines read
	offset int64
	ids    map[string]int
	opts   Options
	// blank: the line before was blank, a heading or the start.
	blank bool
	// The first paragraph: its lines so far, and whether it ended.
	para     []string
	paraDone bool
}

// paragraph takes a line of text outside code and headings towards the
// summary: the first paragraph is the first run of text lines, ended by
// a blank line, a heading or a fence. Indented code, tables, HTML,
// images and rules do not start one.
func (sc *scanner) paragraph(l string) {
	if sc.paraDone {
		return
	}
	t := strings.TrimSpace(l)
	switch {
	case t == "":
		sc.endParagraph()
	case len(sc.para) == 0 && (strings.HasPrefix(l, "    ") || strings.HasPrefix(l, "\t") || strings.HasPrefix(t, "|") || strings.HasPrefix(t, "<") ||
		strings.HasPrefix(t, "![") || strings.HasPrefix(t, "[![") || strings.Trim(t, "-=*_ ") == ""):
	default:
		sc.para = append(sc.para, t)
	}
}

func (sc *scanner) endParagraph() {
	if sc.paraDone || len(sc.para) == 0 {
		return
	}
	sc.paraDone = true
	t := link.ReplaceAllString(strings.Join(sc.para, " "), "$1")
	sc.out.Summary = cutWords(t, SummaryLength)
}

// cutWords cuts a text to at most n bytes, at a space when there is one.
func cutWords(t string, n int) string {
	if len(t) <= n {
		return t
	}
	i := n
	for i > 0 && !utf8.RuneStart(t[i]) {
		i--
	}
	if j := strings.LastIndexByte(t[:i], ' '); j > n/2 {
		i = j
	}
	return strings.TrimRight(t[:i], " ,.;:") + " …"
}

func (sc *scanner) replay(lines []string) {
	for _, l := range lines {
		sc.line(l)
	}
}

// skip counts a line that is no content (front matter).
func (sc *scanner) skip(line string) {
	sc.mark()
	sc.n++
	sc.offset += int64(len(line))
	sc.out.Lines, sc.out.Bytes = sc.n, sc.offset
}

func (sc *scanner) mark() {
	if sc.n%OffsetEvery == 0 {
		sc.out.Offsets = append(sc.out.Offsets, sc.offset)
	}
}

func (sc *scanner) line(line string) {
	sc.mark()
	l := strings.TrimRight(line, "\r\n")
	if sc.fence != "" || codeFence.MatchString(l) {
		sc.blank = false // code is no blank line before a bold paragraph
	}
	switch m := codeFence.FindStringSubmatch(l); {
	case sc.fence != "":
		if m != nil && m[1][0] == sc.fence[0] && len(m[1]) >= len(sc.fence) &&
			strings.TrimSpace(l[len(m[0]):]) == "" {
			sc.fence = ""
		}
	case m != nil:
		sc.fence = m[1]
		sc.endParagraph()
	default:
		level, title, id := ParseHeading(l)
		if level == 0 {
			sc.paragraph(l)
			if sc.opts.ListItems {
				sc.item(l)
			}
			sc.blank = strings.TrimSpace(l) == ""
		} else {
			sc.blank = true
			sc.endParagraph()
			sc.end(level) // the sections it ends close on the line before
			h := Heading{Level: level, Title: title, Line: sc.n + 1, start: sc.offset, Parent: -1}
			if len(sc.open) > 0 {
				h.Parent = sc.open[len(sc.open)-1]
			}
			h.ID = sc.unique(id, title)
			sc.out.Headings = append(sc.out.Headings, h)
			sc.open = append(sc.open, len(sc.out.Headings)-1)
		}
	}
	sc.n++
	sc.offset += int64(len(line))
	sc.out.Lines, sc.out.Bytes = sc.n, sc.offset
}

// item opens a section for a list item or paragraph with a bold title,
// one level below the innermost heading of the document.
func (sc *scanner) item(l string) {
	var title string
	if m := listItem.FindStringSubmatch(l); m != nil {
		title = strings.TrimSpace(m[2])
		if m[1] != "" {
			title = m[1] + ". " + title
		}
	} else if m := boldPara.FindStringSubmatch(l); m != nil && sc.blank {
		title = strings.TrimSpace(m[1])
	} else {
		return
	}
	title = strings.TrimRight(title, ". ")
	if len(title) < 3 || strings.HasSuffix(title, ":") {
		return
	}
	parent := -1
	for i := len(sc.open) - 1; i >= 0; i-- {
		if !sc.out.Headings[sc.open[i]].Item {
			parent = sc.open[i]
			break
		}
	}
	if parent < 0 || sc.out.Headings[parent].Level == 6 {
		return
	}
	level := sc.out.Headings[parent].Level + 1
	sc.end(level)
	sc.out.Headings = append(sc.out.Headings, Heading{Level: level, Title: cut(title), Line: sc.n + 1,
		start: sc.offset, Parent: parent, Item: true})
	sc.open = append(sc.open, len(sc.out.Headings)-1)
}

// dropShortItems leaves items of one line in their parent: a list of
// short bold terms ("- **desktop notifications** (package);") is no
// list of entries, and a one-line entry is read with its neighbours.
// Nothing is below a one-line item, so only indexes change.
func (sc *scanner) dropShortItems() {
	hs := sc.out.Headings
	index := make([]int, len(hs)) // old index → new
	var out []Heading
	for i, h := range hs {
		if h.Item && h.Lines < 2 {
			continue
		}
		index[i] = len(out)
		out = append(out, h)
	}
	if len(out) == len(hs) {
		return
	}
	for i := range out {
		if out[i].Parent >= 0 {
			out[i].Parent = index[out[i].Parent]
		}
	}
	sc.out.Headings = out
}

// itemIDs gives the items their ids once the headings have theirs, so
// that items never change a heading's id (its anchor, as links have
// it): the slug of the title, made unique against all ids.
func (sc *scanner) itemIDs() {
	taken := map[string]bool{}
	for _, h := range sc.out.Headings {
		if !h.Item {
			taken[h.ID] = true
		}
	}
	for i := range sc.out.Headings {
		h := &sc.out.Headings[i]
		if !h.Item {
			continue
		}
		base := Slug(h.Title)
		if base == "" {
			base = "item"
		}
		id := base
		for n := 1; taken[id]; n++ {
			id = base + "-" + strconv.Itoa(n)
		}
		taken[id] = true
		h.ID = id
	}
}

// end closes the open sections of level or deeper on the current line.
func (sc *scanner) end(level int) {
	for len(sc.open) > 0 {
		h := &sc.out.Headings[sc.open[len(sc.open)-1]]
		if h.Level < level {
			return
		}
		h.Lines, h.Bytes = sc.n-h.Line+1, sc.offset-h.start
		sc.open = sc.open[:len(sc.open)-1]
	}
}

// unique returns the heading's id: the explicit one, or the slug of its
// title, made unique as GitHub does (github-slugger: a repeated id gets
// -1, -2, … in document order, skipping ids already taken).
func (sc *scanner) unique(explicit, title string) string {
	base := explicit
	if base == "" {
		base = Slug(title)
	}
	if base == "" {
		base = "section"
	}
	id := base
	for {
		if _, taken := sc.ids[id]; !taken {
			break
		}
		sc.ids[base]++
		id = base + "-" + strconv.Itoa(sc.ids[base])
	}
	sc.ids[id] = 0
	return id
}

// ParseHeading returns the level (0: not a heading), title and explicit
// id of an ATX heading line.
func ParseHeading(l string) (level int, title, id string) {
	m := atxHeading.FindStringSubmatch(l)
	if m == nil {
		return 0, "", ""
	}
	title = strings.TrimSpace(closingHash.ReplaceAllString(m[2], ""))
	if e := explicitID.FindStringSubmatch(title); e != nil {
		id, title = e[1], strings.TrimSpace(title[:len(title)-len(e[0])])
	}
	if a := anchorTag.FindStringSubmatch(title); a != nil {
		if id == "" {
			id = a[1]
		}
		title = strings.TrimSpace(anchorTag.ReplaceAllString(title, ""))
	}
	if title == "" {
		return 0, "", ""
	}
	return len(m[1]), cut(title), id
}

// Slug is the anchor GitHub gives a heading: the rendered title
// lowercased, characters other than letters, digits, marks, spaces, "-"
// and "_" removed, each space turned into "-".
func Slug(title string) string {
	t := link.ReplaceAllString(title, "$1")
	t = htmlTag.ReplaceAllString(t, "")
	var b strings.Builder
	for _, r := range strings.ToLower(t) {
		switch {
		case r == ' ':
			b.WriteByte('-')
		case r == '-' || r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.IsMark(r):
			b.WriteRune(r)
		}
	}
	return b.String()
}

// Path returns the titles from the outermost heading enclosing heading i
// down to it.
func (o *Outline) Path(i int) []string {
	var out []string
	for ; i >= 0; i = o.Headings[i].Parent {
		out = append([]string{o.Headings[i].Title}, out...)
	}
	return out
}

// At returns the index of the innermost heading whose section holds
// line, or -1 (before the first heading).
func (o *Outline) At(line int) int {
	// The last heading at or before the line: sections nest, so it is
	// the innermost one that holds it.
	lo, hi := 0, len(o.Headings)
	for lo < hi {
		mid := (lo + hi) / 2
		if o.Headings[mid].Line <= line {
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	return lo - 1
}

// Children returns the indexes of the headings directly below heading i
// (-1: the top-level headings).
func (o *Outline) Children(i int) []int {
	var out []int
	for j, h := range o.Headings {
		if h.Parent == i {
			out = append(out, j)
		}
	}
	return out
}

// Within reports whether heading j is heading i or below it.
func (o *Outline) Within(j, i int) bool {
	for ; j >= 0; j = o.Headings[j].Parent {
		if j == i {
			return true
		}
	}
	return false
}

func cut(l string) string {
	if len(l) <= MaxTitle {
		return l
	}
	i := MaxTitle
	for i > 0 && !utf8.RuneStart(l[i]) {
		i--
	}
	return l[:i] + " …"
}
