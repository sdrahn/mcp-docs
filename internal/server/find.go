package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/sdrahn/mcp-docs/internal/catalog"
	"github.com/sdrahn/mcp-docs/internal/rank"
)

// find_sections ranks sections for a question in words (§5.6): for the
// broad question where search finds too much or nothing and the right
// document is not obvious. It returns ids, paths, sizes and the
// best-matching line, not text: the agent chooses what to read.

const (
	defaultFindResults = 5
	maxFindResults     = 50
)

// indexes are the rank indexes of the collections, by name, built when
// first asked and again when a document changes.
type indexes struct {
	mu sync.Mutex
	m  map[string]*rank.Index
}

func (s *Server) index(ctx context.Context, k *catalog.Collection) (*rank.Index, error) {
	s.ranked.mu.Lock()
	defer s.ranked.mu.Unlock()
	if s.ranked.m == nil {
		s.ranked.m = map[string]*rank.Index{}
	}
	if ix := s.ranked.m[k.Name]; ix != nil && ix.Current(ctx, s.Cat, k) {
		return ix, nil
	}
	ix, err := rank.Build(ctx, s.Cat, k, s.MaxFile)
	if err != nil {
		return nil, err
	}
	s.ranked.m[k.Name] = ix
	return ix, nil
}

type found struct {
	Doc       string   `json:"doc"`
	Section   string   `json:"section,omitempty"`
	Path      []string `json:"path"`
	Line      int      `json:"line"`
	Lines     int      `json:"lines"`
	Bytes     int64    `json:"bytes"`
	Score     float64  `json:"score"`
	MatchLine int      `json:"matchLine,omitempty"`
	MatchText string   `json:"matchText,omitempty"`
	Version   string   `json:"version"`
}

func findSectionsTool() tool {
	return tool{name: "find_sections", title: "Find sections for a question",
		description: "The sections that best match a question or topic in words, ranked (BM25 over each " +
			"section's text, its title counting most), across the documents of all collections or one: for a " +
			"broad question where the document is not obvious or search finds too much or nothing. Gives each " +
			"section's document, id, heading path, size and best-matching line, not its text: read the one that " +
			"answers with read_section. For a precise term (a setting, an error message), search is better.",
		input: obj(map[string]any{
			"query":      str("the question or topic, in words (\"back up the gateway's state\")"),
			"collection": str("only this collection"),
			"maxResults": integer(1, maxFindResults, fmt.Sprintf("sections at most (default %d)", defaultFindResults)),
		}, "query"),
		output: obj(map[string]any{
			"sections": map[string]any{"type": "array", "items": obj(map[string]any{
				"doc": strType, "section": strType, "path": map[string]any{"type": "array", "items": strType},
				"line": intType, "lines": intType, "bytes": intType, "score": map[string]any{"type": "number"},
				"matchLine": intType, "matchText": strType, "version": strType},
				"doc", "path", "line", "lines", "bytes", "score", "version")},
		}, "sections"),
		run: findSections}
}

func findSections(s *Server, ctx context.Context, args json.RawMessage) (*result, error) {
	var a struct {
		Query      string `json:"query"`
		Collection string `json:"collection"`
		MaxResults int    `json:"maxResults"`
	}
	if err := decode(args, &a); err != nil {
		return nil, err
	}
	if strings.TrimSpace(a.Query) == "" {
		return nil, errors.New("query: give the question or topic")
	}
	n := a.MaxResults
	if n == 0 {
		n = defaultFindResults
	}
	if n < 1 || n > maxFindResults {
		return nil, fmt.Errorf("maxResults: from 1 to %d", maxFindResults)
	}
	colls := s.Cat.Collections
	if a.Collection != "" {
		k := s.Cat.Collection(a.Collection)
		if k == nil {
			return nil, fmt.Errorf("collection %q: no such collection (%s)", a.Collection, strings.Join(s.collNames(), ", "))
		}
		colls = []*catalog.Collection{k}
	}
	var hits []rank.Hit
	for _, k := range colls {
		ix, err := s.index(ctx, k)
		if err != nil {
			return nil, err
		}
		h, err := ix.Search(a.Query, n)
		if err != nil {
			return nil, err
		}
		hits = append(hits, h...)
	}
	// Scores of different collections are of different indexes; within
	// one, they are comparable. Merged by score all the same: the best of
	// each tends to the top.
	sortHits(hits)
	if len(hits) > n {
		hits = hits[:n]
	}

	terms := map[string]bool{}
	for _, t := range rank.Tokens(a.Query) {
		terms[t] = true
	}
	out := []found{}
	var text strings.Builder
	fmt.Fprintf(&text, "%d section(s) for %q, best first:\n", len(hits), a.Query)
	for i, h := range hits {
		o := h.Entry.Outline
		f := found{Doc: h.Doc.Address(), Score: round(h.Score), Version: h.Entry.Version}
		if h.Heading >= 0 {
			hd := o.Headings[h.Heading]
			f.Section, f.Path, f.Line, f.Lines, f.Bytes = hd.ID, shortPath(o, h.Heading), hd.Line, hd.Lines, hd.Bytes
		} else {
			first, last := rank.OwnLines(o, -1)
			f.Path, f.Line, f.Lines = []string{"(before the first heading)"}, first, last-first+1
			f.Bytes = -1
		}
		first, last := rank.OwnLines(o, h.Heading)
		if ls, err := s.Cat.Lines(h.Doc, h.Entry, first, last); err == nil {
			f.MatchLine, f.MatchText = bestLine(ls, first, terms)
		}
		if f.Bytes < 0 {
			f.Bytes = 0
			if ls, err := s.Cat.Lines(h.Doc, h.Entry, first, last); err == nil {
				for _, l := range ls {
					f.Bytes += int64(len(l))
				}
			}
		}
		out = append(out, f)
		// One line an agent can act on (read_section takes doc#id), then
		// the words that matched, in their context (§15).
		addr := f.Doc
		if f.Section != "" {
			addr += "#" + f.Section
		}
		fmt.Fprintf(&text, "%d. %s  %s  (%s)\n", i+1, addr, strings.Join(f.Path, " > "), size(f.Bytes))
		if f.MatchLine > 0 {
			fmt.Fprintf(&text, "   %d: %s\n", f.MatchLine, excerpt(strings.TrimSpace(f.MatchText), terms))
		}
	}
	if len(hits) == 0 {
		text.Reset()
		fmt.Fprintf(&text, "no sections for %q: try other words, or search for a precise term", a.Query)
	} else {
		text.WriteString("(read one: read_section doc#id)")
	}
	return &result{text: text.String(), structured: map[string]any{"sections": out}}, nil
}

// excerptWidth is about how much of a matching line find_sections
// shows: the words around the first of the query's.
const excerptWidth = 160

// excerpt is the part of a line around its first word of the query,
// about excerptWidth bytes, cut at spaces and marked "…" where cut.
func excerpt(l string, terms map[string]bool) string {
	if len(l) <= excerptWidth {
		return l
	}
	at := 0
	for i := 0; i < len(l); {
		j := i
		for j < len(l) && l[j] != ' ' {
			j++
		}
		if ts := rank.Tokens(l[i:j]); len(ts) > 0 && terms[ts[0]] {
			at = i
			break
		}
		i = j + 1
	}
	start := max(0, at-excerptWidth/3)
	if start > 0 {
		if k := strings.IndexByte(l[start:], ' '); k >= 0 && start+k < at+1 {
			start += k + 1
		}
	}
	end := min(len(l), start+excerptWidth)
	if end < len(l) {
		if k := strings.LastIndexByte(l[start:end], ' '); k > 0 {
			end = start + k
		}
	}
	for start > 0 && !utf8.RuneStart(l[start]) {
		start++
	}
	for end < len(l) && !utf8.RuneStart(l[end]) {
		end--
	}
	out := l[start:end]
	if start > 0 {
		out = "…" + out
	}
	if end < len(l) {
		out += "…"
	}
	return out
}

// bestLine is the line of a section with the most of the query's words
// (the first of those), skipping the heading when another line has any.
func bestLine(lines []string, first int, terms map[string]bool) (int, string) {
	best, bestN := 0, 0
	for i, l := range lines {
		n := 0
		seen := map[string]bool{}
		for _, t := range rank.Tokens(l) {
			if terms[t] && !seen[t] {
				seen[t] = true
				n++
			}
		}
		if i == 0 && len(lines) > 1 {
			n = min(n, 1) // a heading that names the topic is not the best line
		}
		if n > bestN {
			best, bestN = i, n
		}
	}
	if bestN == 0 {
		return 0, ""
	}
	return first + best, strings.TrimRight(lines[best], "\r\n")
}

func sortHits(h []rank.Hit) {
	for i := 1; i < len(h); i++ {
		for j := i; j > 0 && h[j].Score > h[j-1].Score; j-- {
			h[j], h[j-1] = h[j-1], h[j]
		}
	}
}

func round(f float64) float64 { return float64(int(f*100+0.5)) / 100 }
