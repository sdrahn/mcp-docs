package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/sdrahn/mcp-docs/internal/catalog"
	"github.com/sdrahn/mcp-docs/internal/markdown"
)

// search finds lines as mcp-gateway's search_text does, and places each
// match in its section, so that the agent goes from a match to
// read_section without an outline between (§5.3, D5).

const (
	defaultContext    = 2
	maxContext        = 10
	defaultMaxResults = 50
	maxMaxResults     = 500
	// maxLineShown cuts long lines in the output.
	maxLineShown = 400
)

type match struct {
	Doc          string   `json:"doc"`
	Line         int      `json:"line"`
	Text         string   `json:"text"`
	Version      string   `json:"version,omitempty"`
	Section      string   `json:"section,omitempty"`
	Path         []string `json:"path,omitempty"`
	SectionLine  int      `json:"sectionLine,omitempty"`
	SectionLines int      `json:"sectionLines,omitempty"`
	SectionBytes int64    `json:"sectionBytes,omitempty"`
}

var queryArgs = map[string]any{
	"query":         str("text, or RE2 with regexp"),
	"regexp":        boolean("query is a regular expression"),
	"caseSensitive": boolean("match case"),
	"context":       integer(0, maxContext, fmt.Sprintf("lines around each match (default %d)", defaultContext)),
	"maxResults":    integer(1, maxMaxResults, fmt.Sprintf("matching lines at most (default %d)", defaultMaxResults)),
}

func searchTool() tool {
	in := map[string]any{
		"collection": str("only this collection"),
		"doc":        str("only this document or directory (collection/path)"),
	}
	for k, v := range queryArgs {
		in[k] = v
	}
	return tool{name: "search", title: "Search the documents",
		description: "Lines containing a text (or an RE2 expression), each with its section (doc#id): for a " +
			"precise term, a setting or an error message. Read the section with read_section.",
		input: obj(in, "query"),
		output: obj(map[string]any{
			"matches": map[string]any{"type": "array", "items": obj(map[string]any{
				"doc": strType, "line": intType, "text": strType, "version": strType, "section": strType,
				"path": map[string]any{"type": "array", "items": strType}, "sectionLine": intType,
				"sectionLines": intType, "sectionBytes": intType}, "doc", "line", "text")},
			"truncated": map[string]any{"type": "boolean"}}, "matches", "truncated"),
		run: search}
}

type queryOpts struct {
	Query         string `json:"query"`
	Regexp        bool   `json:"regexp"`
	CaseSensitive bool   `json:"caseSensitive"`
	Context       *int   `json:"context"`
	MaxResults    int    `json:"maxResults"`
}

// textSearch collects matches and their context, up to limit matching
// lines and the read limit of output.
type textSearch struct {
	s     *Server
	match func(string) bool
	// inText, if set, is false for a document none of whose lines
	// match.
	inText    func(string) bool
	around    int
	limit     int
	compat    bool // mcp-server-fs's output: file paths, no sections
	matches   []match
	out       strings.Builder
	truncated bool
	skipped   []string // documents too large to search
	// marks[k] is the length of out after match k's lines.
	marks []int
	// more are the collections with matches the result leaves out, when
	// several were searched.
	more []string
}

func (s *Server) newSearch(q queryOpts, compat bool) (*textSearch, error) {
	if q.Query == "" {
		return nil, errors.New("query: give the text to find")
	}
	around := defaultContext
	if q.Context != nil {
		around = *q.Context
	}
	if around < 0 || around > maxContext {
		return nil, fmt.Errorf("context: from 0 to %d lines", maxContext)
	}
	limit := q.MaxResults
	if limit == 0 {
		limit = defaultMaxResults
	}
	if limit < 0 || limit > maxMaxResults {
		return nil, fmt.Errorf("maxResults: from 1 to %d", maxMaxResults)
	}
	m, err := matcher(q.Query, q.Regexp, q.CaseSensitive)
	if err != nil {
		return nil, err
	}
	// A text matches a line as it matches the document; an expression
	// is matched a line at a time (^ and $ are a line's).
	var inText func(string) bool
	if !q.Regexp {
		inText = m
	}
	return &textSearch{s: s, match: m, inText: inText, around: around, limit: limit, compat: compat, matches: []match{}}, nil
}

func search(s *Server, ctx context.Context, args json.RawMessage) (*result, error) {
	var a struct {
		queryOpts
		Collection string `json:"collection"`
		Doc        string `json:"doc"`
	}
	if err := decode(args, &a); err != nil {
		return nil, err
	}
	sr, err := s.newSearch(a.queryOpts, false)
	if err != nil {
		return nil, err
	}
	var starts []catalog.Doc
	switch {
	case a.Doc != "" && a.Collection != "":
		return nil, errors.New("give collection or doc, not both")
	case a.Doc != "":
		d, _, err := s.Cat.Resolve(a.Doc, false)
		if err != nil {
			return nil, err
		}
		starts = []catalog.Doc{d}
	case a.Collection != "":
		k := s.Cat.Collection(a.Collection)
		if k == nil {
			return nil, fmt.Errorf("collection %q: no such collection (%s)", a.Collection, strings.Join(s.collNames(), ", "))
		}
		starts = []catalog.Doc{{Coll: k, Rel: "."}}
	default:
		for _, k := range s.Cat.Collections {
			starts = append(starts, catalog.Doc{Coll: k, Rel: "."})
		}
	}
	if len(starts) == 1 {
		if err := sr.below(ctx, starts[0], nil); err != nil {
			return nil, err
		}
		return sr.result(), nil
	}
	// Several collections: each is searched on its own, and they share
	// what one call returns, so that the first one's matches do not
	// keep the others' out.
	subs := make([]*textSearch, len(starts))
	for i, d := range starts {
		if subs[i], err = s.newSearch(a.queryOpts, false); err != nil {
			return nil, err
		}
		if err := subs[i].below(ctx, d, nil); err != nil {
			return nil, err
		}
	}
	for i, n := range shares(subs, sr.limit, s.MaxRead-512) {
		sub := subs[i]
		if n > 0 {
			sr.out.WriteString(strings.TrimRight(sub.out.String()[:sub.marks[n-1]], "\n") + "\n\n")
			sr.matches = append(sr.matches, sub.matches[:n]...)
		}
		if n < len(sub.matches) || sub.truncated {
			sr.truncated = true
			sr.more = append(sr.more, starts[i].Coll.Name)
		}
		sr.skipped = append(sr.skipped, sub.skipped...)
	}
	return sr.result(), nil
}

// shares divides limit matching lines and budget bytes of output among
// searches: a line to each in turn while it has more, then, while the
// output is too large, a line less of the one with the most output.
func shares(subs []*textSearch, limit int, budget int64) []int {
	n := make([]int, len(subs))
	for given := true; given && limit > 0; {
		given = false
		for i, sub := range subs {
			if limit > 0 && n[i] < len(sub.matches) {
				n[i]++
				limit--
				given = true
			}
		}
	}
	size := func(i int) int64 {
		if n[i] == 0 {
			return 0
		}
		return int64(subs[i].marks[n[i]-1])
	}
	for {
		var total int64
		most := 0
		for i := range subs {
			total += size(i)
			if size(i) > size(most) {
				most = i
			}
		}
		shown := 0
		for i := range subs {
			if n[i] > 0 {
				shown++
			}
		}
		if total <= budget || (n[most] <= 1 && shown <= 1) {
			return n
		}
		n[most]--
	}
}

// below searches d, a document or the documents below a directory,
// leaving out those exclude matches.
func (sr *textSearch) below(ctx context.Context, d catalog.Doc, exclude func(catalog.Doc) bool) error {
	fi, err := sr.s.Cat.Stat(d)
	if err != nil {
		return err
	}
	if !fi.IsDir() {
		sr.doc(ctx, d)
		return nil
	}
	return sr.s.Cat.Walk(ctx, d, exclude, func(doc catalog.Doc, e fs.DirEntry) bool {
		if e.IsDir() {
			return true
		}
		return sr.doc(ctx, doc)
	})
}

func (sr *textSearch) result() *result {
	body := strings.TrimRight(sr.out.String(), "\n")
	switch {
	case len(sr.matches) == 0 && !sr.truncated:
		body = "no matches"
	case sr.truncated && len(sr.more) > 0:
		body += fmt.Sprintf("\n(stopped after %d matching lines; more in %s: narrow the query, or search one collection or document)",
			len(sr.matches), strings.Join(sr.more, ", "))
	case sr.truncated:
		body += fmt.Sprintf("\n(stopped after %d matching lines: narrow the query, or search one collection or document)", len(sr.matches))
	case !sr.compat:
		body += "\n(read a match's section: read_section with doc and its id; or the lines: read_lines)"
	}
	if len(sr.skipped) > 0 {
		body += fmt.Sprintf("\n(not searched, larger than %s: %s)", size(sr.s.MaxFile), strings.Join(sr.skipped, ", "))
	}
	return &result{text: body, structured: map[string]any{"matches": sr.matches, "truncated": sr.truncated}}
}

// doc searches one document; it returns false when the search is to
// stop.
func (sr *textSearch) doc(ctx context.Context, d catalog.Doc) bool {
	if ctx.Err() != nil {
		return false
	}
	e, text, err := sr.s.Cat.LoadText(ctx, d, sr.s.MaxFile)
	if errors.Is(err, catalog.ErrTooLarge) {
		sr.skipped = append(sr.skipped, d.Address())
		return true
	}
	if err != nil {
		return ctx.Err() == nil // unreadable, or not text: skipped
	}
	// A text is in a line only if it is in the document: most documents
	// are done with one look, without their lines.
	if sr.inText != nil && !sr.inText(text) {
		return true
	}
	lines := strings.Split(strings.TrimSuffix(text, "\n"), "\n")
	var hits []int
	for i, l := range lines {
		lines[i] = strings.TrimSuffix(l, "\r")
		if sr.match(lines[i]) {
			hits = append(hits, i)
		}
	}
	if len(hits) == 0 {
		return true
	}
	if len(sr.matches) >= sr.limit || int64(sr.out.Len()) > sr.s.MaxRead-512 {
		sr.truncated = true
		return false
	}
	name := d.Address()
	if sr.compat {
		name = d.Full()
	}
	o := e.Outline
	if sr.compat {
		fmt.Fprintf(&sr.out, "%s\n", name)
	} else {
		fmt.Fprintf(&sr.out, "%s (version %s)\n", name, e.Version)
	}
	shownTo := -1 // last line index written
	section := -2 // the section whose header was written
	for _, h := range hits {
		if len(sr.matches) >= sr.limit || int64(sr.out.Len()) > sr.s.MaxRead-512 {
			sr.truncated = true
			return false
		}
		from, to := max(h-sr.around, 0), min(h+sr.around, len(lines)-1)
		m := match{Doc: name, Line: h + 1, Text: cut(lines[h])}
		sec := o.At(h + 1)
		if !sr.compat {
			// Context stays in the match's section's own text (up to the
			// next heading), so that every line is shown under its own.
			first, last := 1, len(lines)
			if sec >= 0 {
				first = o.Headings[sec].Line
			}
			if sec+1 < len(o.Headings) {
				last = o.Headings[sec+1].Line - 1
			}
			from, to = max(from, first-1), min(to, last-1)
			m.Version = e.Version
			if sec >= 0 {
				sh := o.Headings[sec]
				m.Section, m.Path, m.SectionLine, m.SectionLines, m.SectionBytes = sh.ID, o.Path(sec), sh.Line, sh.Lines, sh.Bytes
			}
			if sec != section {
				if sec >= 0 {
					sh := o.Headings[sec]
					fmt.Fprintf(&sr.out, "  § %s  #%s  [%d lines, %s]\n", strings.Join(shortPath(o, sec), " > "), sh.ID,
						sh.Lines, size(sh.Bytes))
				} else {
					sr.out.WriteString("  § (before the first heading)\n")
				}
				section = sec
			} else if shownTo >= 0 && from > shownTo+1 {
				sr.out.WriteString("  --\n")
			}
		} else if shownTo >= 0 && from > shownTo+1 {
			sr.out.WriteString("--\n")
		}
		indent := ""
		if !sr.compat {
			indent = "  "
		}
		for i := max(from, shownTo+1); i <= to; i++ {
			sep := "-"
			if sr.match(lines[i]) {
				sep = ":"
			}
			fmt.Fprintf(&sr.out, "%s%d%s %s\n", indent, i+1, sep, cut(lines[i]))
		}
		shownTo = max(shownTo, to)
		sr.matches = append(sr.matches, m)
		sr.marks = append(sr.marks, sr.out.Len())
	}
	sr.out.WriteString("\n")
	return true
}

// shortPath is a heading's path without the document's title (its one
// level-1 heading), which every path of the document would repeat.
func shortPath(o *markdown.Outline, i int) []string {
	p := o.Path(i)
	ones := 0
	for _, h := range o.Headings {
		if h.Level == 1 {
			ones++
		}
	}
	if ones == 1 && len(p) > 1 && o.Headings[0].Level == 1 {
		return p[1:]
	}
	return p
}

// matcher returns whether a line matches query.
func matcher(query string, isRegexp, caseSensitive bool) (func(string) bool, error) {
	if isRegexp {
		if !caseSensitive {
			query = "(?i)" + query
		}
		re, err := regexp.Compile(query)
		if err != nil {
			return nil, fmt.Errorf("query: not a valid regular expression: %v", err)
		}
		return func(l string) bool { return re.MatchString(spaced(l)) }, nil
	}
	query = spaced(query)
	if caseSensitive {
		return func(l string) bool { return strings.Contains(spaced(l), query) }, nil
	}
	q := strings.ToLower(query)
	if isASCII(q) {
		return func(l string) bool { return containsFold(spaced(l), q) }, nil
	}
	return func(l string) bool { return strings.Contains(strings.ToLower(spaced(l)), q) }, nil
}

func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= utf8.RuneSelf {
			return false
		}
	}
	return true
}

// containsFold reports whether s holds q, which is lowercase ASCII and
// not empty, in any case: strings.Contains(strings.ToLower(s), q),
// without making the lowercase text (of every document searched).
func containsFold(s, q string) bool {
	// The two letters outside ASCII whose lowercase is in it: the long
	// way, for the same answer.
	if strings.Contains(s, "\u212a") || strings.Contains(s, "\u0130") {
		return strings.Contains(strings.ToLower(s), q)
	}
	lo, up := q[0], q[0]
	if 'a' <= lo && lo <= 'z' {
		up = lo - 'a' + 'A'
	}
	// The next place of the first letter, in either case; -1: none.
	next := func(from int, c byte) int {
		if k := strings.IndexByte(s[from:], c); k >= 0 {
			return from + k
		}
		return -1
	}
	a, b := next(0, lo), -1
	if up != lo {
		b = next(0, up)
	}
	for a >= 0 || b >= 0 {
		i := a
		if a < 0 || (b >= 0 && b < a) {
			i = b
		}
		if len(s)-i < len(q) {
			return false
		}
		k := 1
		for ; k < len(q); k++ {
			c := s[i+k]
			if 'A' <= c && c <= 'Z' {
				c += 'a' - 'A'
			}
			if c != q[k] {
				break
			}
		}
		if k == len(q) {
			return true
		}
		if i == a {
			a = next(i+1, lo)
		} else {
			b = next(i+1, up)
		}
	}
	return false
}

// noBreak turns the no-break spaces into spaces: a query typed with
// spaces finds "SLES for SAP" where the documents write it with
// no-break spaces, as SUSE's do (SLES&nbsp;for&nbsp;SAP).
var noBreak = strings.NewReplacer("\u00a0", " ", "\u2007", " ", "\u202f", " ")

// spaced is a line as search matches it: no-break spaces as spaces.
func spaced(l string) string {
	// Three searches for a string, not one for any of three characters:
	// that one looks at every character (a third of search's time).
	if !strings.Contains(l, "\u00a0") && !strings.Contains(l, "\u2007") && !strings.Contains(l, "\u202f") {
		return l
	}
	return noBreak.Replace(l)
}

// cut shortens a long line for the output.
func cut(l string) string {
	if len(l) <= maxLineShown {
		return l
	}
	i := maxLineShown
	for i > 0 && !utf8.RuneStart(l[i]) {
		i--
	}
	return l[:i] + " …"
}
