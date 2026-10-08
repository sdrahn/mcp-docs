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
}

var queryArgs = map[string]any{
	"query":         str("the text to find, or a regular expression (RE2 syntax) with regexp: true"),
	"regexp":        boolean("query is a regular expression"),
	"caseSensitive": boolean("match case (default: ignore it)"),
	"context":       integer(0, maxContext, fmt.Sprintf("lines shown before and after each match (default %d)", defaultContext)),
	"maxResults":    integer(1, maxMaxResults, fmt.Sprintf("matching lines at most (default %d)", defaultMaxResults)),
}

func searchTool() tool {
	in := map[string]any{
		"collection": str("search only this collection"),
		"doc":        str("search only this document, or the documents below this directory (collection/path)"),
	}
	for k, v := range queryArgs {
		in[k] = v
	}
	return tool{name: "search", title: "Search the documents",
		description: "Find the lines of the documents that contain a text (or match a regular expression with " +
			"regexp: true), case-insensitive unless caseSensitive. Each match comes with its document, line, " +
			"context lines and the section it is in (id, heading path, size): read that section with " +
			"read_section, or the lines around the match with read_lines.",
		input: obj(in, "query"),
		output: obj(map[string]any{
			"matches": map[string]any{"type": "array", "items": obj(map[string]any{
				"doc": strType, "line": intType, "text": strType, "version": strType, "section": strType,
				"path": map[string]any{"type": "array", "items": strType}, "sectionLine": intType,
				"sectionLines": intType}, "doc", "line", "text")},
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
	s         *Server
	match     func(string) bool
	around    int
	limit     int
	compat    bool // mcp-server-fs's output: file paths, no sections
	matches   []match
	out       strings.Builder
	truncated bool
	skipped   []string // documents too large to search
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
	return &textSearch{s: s, match: m, around: around, limit: limit, compat: compat, matches: []match{}}, nil
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
	for _, d := range starts {
		if err := sr.below(ctx, d, nil); err != nil {
			return nil, err
		}
		if sr.truncated {
			break
		}
	}
	return sr.result(), nil
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
	e, err := sr.s.Cat.Load(ctx, d)
	if err != nil {
		return ctx.Err() == nil // unreadable, or not text: skipped
	}
	b, err := sr.s.Cat.ReadAll(d, sr.s.MaxFile)
	if errors.Is(err, catalog.ErrTooLarge) {
		sr.skipped = append(sr.skipped, d.Address())
		return true
	}
	if err != nil {
		return true
	}
	text := string(b)
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
				m.Section, m.Path, m.SectionLine, m.SectionLines = sh.ID, o.Path(sec), sh.Line, sh.Lines
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
		return re.MatchString, nil
	}
	if caseSensitive {
		return func(l string) bool { return strings.Contains(l, query) }, nil
	}
	q := strings.ToLower(query)
	return func(l string) bool { return strings.Contains(strings.ToLower(l), q) }, nil
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
