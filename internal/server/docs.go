package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"regexp"
	"strconv"
	"strings"

	"github.com/sdrahn/mcp-docs/internal/catalog"
	"github.com/sdrahn/mcp-docs/internal/markdown"
)

var docArg = str("the document: collection/path (as list_docs names it), or a path relative to the first collection")

// load resolves a document address and returns its outline.
func (s *Server) load(ctx context.Context, addr string) (catalog.Doc, string, *catalog.Entry, error) {
	d, section, err := s.Cat.Resolve(addr, false)
	if err != nil {
		return d, "", nil, err
	}
	e, err := s.Cat.Load(ctx, d)
	return d, section, e, err
}

// title is a document's title: its first level-1 heading, else its
// first heading.
func title(o *markdown.Outline) string {
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

// --- list_docs ---------------------------------------------------------------

func listDocsTool() tool {
	doc := obj(map[string]any{"doc": strType, "title": strType, "lines": intType, "bytes": intType,
		"index": map[string]any{"type": "boolean"}}, "doc", "title", "lines", "bytes")
	coll := obj(map[string]any{"name": strType, "description": strType, "path": strType, "index": strType,
		"documents": intType, "bytes": intType}, "name", "path", "documents", "bytes")
	return tool{name: "list_docs", title: "List documents",
		description: "The collections of documents (without arguments, when there are several), or the documents " +
			"of a collection or below a path, with their titles and sizes; the collection's index first.",
		input: obj(map[string]any{"collection": str("a collection's name"),
			"path": str("a directory: collection/path")}),
		output: obj(map[string]any{
			"collections": map[string]any{"type": "array", "items": coll},
			"documents":   map[string]any{"type": "array", "items": doc},
			"truncated":   map[string]any{"type": "boolean"}}, "truncated"),
		run: listDocs}
}

type docInfo struct {
	Doc   string `json:"doc"`
	Title string `json:"title"`
	Lines int    `json:"lines"`
	Bytes int64  `json:"bytes"`
	Index bool   `json:"index,omitempty"`
}

type collInfo struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Path        string `json:"path"`
	Index       string `json:"index,omitempty"`
	Documents   int    `json:"documents"`
	Bytes       int64  `json:"bytes"`
}

func listDocs(s *Server, ctx context.Context, args json.RawMessage) (*result, error) {
	var a struct {
		Collection string `json:"collection"`
		Path       string `json:"path"`
	}
	if err := decode(args, &a); err != nil {
		return nil, err
	}
	switch {
	case a.Collection != "" && a.Path != "":
		return nil, errors.New("give collection or path, not both")
	case a.Path != "":
		d, _, err := s.Cat.Resolve(a.Path, false)
		if err != nil {
			return nil, err
		}
		return s.listDocuments(ctx, d)
	case a.Collection != "":
		k := s.Cat.Collection(a.Collection)
		if k == nil {
			return nil, fmt.Errorf("collection %q: no such collection (%s)", a.Collection, strings.Join(s.collNames(), ", "))
		}
		return s.listDocuments(ctx, catalog.Doc{Coll: k, Rel: "."})
	case len(s.Cat.Collections) == 1:
		return s.listDocuments(ctx, catalog.Doc{Coll: s.Cat.Collections[0], Rel: "."})
	}
	var out strings.Builder
	colls := []collInfo{}
	for _, k := range s.Cat.Collections {
		ci := collInfo{Name: k.Name, Description: k.Description, Path: k.Path}
		if d, ok := s.Cat.Index(k); ok {
			ci.Index = d.Address()
		}
		err := s.Cat.Walk(ctx, catalog.Doc{Coll: k, Rel: "."}, nil, func(d catalog.Doc, e fs.DirEntry) bool {
			if !e.IsDir() {
				ci.Documents++
				if fi, err := e.Info(); err == nil {
					ci.Bytes += fi.Size()
				}
			}
			return ci.Documents < s.Cat.MaxEntries
		})
		if err != nil {
			if ctx.Err() != nil {
				return nil, err
			}
			fmt.Fprintf(&out, "%s: %v\n", k.Name, err)
		}
		colls = append(colls, ci)
		fmt.Fprintf(&out, "%s", k.Name)
		if k.Description != "" {
			fmt.Fprintf(&out, ": %s", k.Description)
		}
		fmt.Fprintf(&out, "\n  %d documents, %s (%s) in %s", ci.Documents, size(ci.Bytes), tokens(ci.Bytes), k.Path)
		if ci.Index != "" {
			fmt.Fprintf(&out, "; index %s", ci.Index)
		}
		out.WriteString("\n")
	}
	out.WriteString("(list a collection's documents: list_docs with collection)")
	return &result{text: out.String(), structured: map[string]any{"collections": colls, "truncated": false}}, nil
}

func (s *Server) collNames() []string {
	var out []string
	for _, k := range s.Cat.Collections {
		out = append(out, k.Name)
	}
	return out
}

func (s *Server) listDocuments(ctx context.Context, dir catalog.Doc) (*result, error) {
	fi, err := s.Cat.Stat(dir)
	if err != nil {
		return nil, err
	}
	if !fi.IsDir() {
		return nil, fmt.Errorf("%s: a document, not a directory (outline shows its sections)", dir.Address())
	}
	var docs []docInfo
	truncated := false
	err = s.Cat.Walk(ctx, dir, nil, func(d catalog.Doc, e fs.DirEntry) bool {
		if e.IsDir() {
			return true
		}
		if len(docs) >= s.Cat.MaxEntries {
			truncated = true
			return false
		}
		di := docInfo{Doc: d.Address()}
		if en, err := s.Cat.Load(ctx, d); err == nil {
			di.Title, di.Lines, di.Bytes = title(en.Outline), en.Outline.Lines, en.Outline.Bytes
		} else if ctx.Err() != nil {
			return false
		}
		docs = append(docs, di)
		return true
	})
	if err != nil {
		return nil, err
	}
	if idx, ok := s.Cat.Index(dir.Coll); ok {
		for i := range docs {
			if docs[i].Doc == idx.Address() {
				docs[i].Index = true
				docs = append([]docInfo{docs[i]}, append(docs[:i:i], docs[i+1:]...)...)
				break
			}
		}
	}
	var total int64
	for _, d := range docs {
		total += d.Bytes
	}
	var out strings.Builder
	fmt.Fprintf(&out, "%s: %d documents, %s (%s)\n", dir.Address(), len(docs), size(total), tokens(total))
	shown := 0
	for _, d := range docs {
		line := "  " + d.Doc
		if d.Title != "" {
			line += " — " + d.Title
		}
		if d.Index {
			line += " (index: read it first)"
		}
		line += fmt.Sprintf("  [%d lines, %s]\n", d.Lines, size(d.Bytes))
		if int64(out.Len()+len(line)) > s.MaxRead-256 {
			truncated = true
			break
		}
		out.WriteString(line)
		shown++
	}
	docs = docs[:shown]
	if len(docs) == 0 {
		out.WriteString("  no documents\n")
	}
	if truncated {
		out.WriteString("(more documents: list a directory with path, or search)\n")
	}
	out.WriteString("(the sections of a document: outline)")
	if docs == nil {
		docs = []docInfo{}
	}
	return &result{text: out.String(), structured: map[string]any{"documents": docs, "truncated": truncated}}, nil
}

// --- outline -----------------------------------------------------------------

func outlineTool() tool {
	return tool{name: "outline", title: "Outline of a document",
		description: "The headings of a document with their section ids and line numbers, and how many lines " +
			"and bytes each section has (to the next heading of the same or a higher level). Read one section " +
			"with read_section (its id) instead of the whole document. section limits it to one section's " +
			"subsections, maxLevel to the upper levels.",
		input: obj(map[string]any{"doc": docArg,
			"section":  str("only this section and its subsections: an id, or a heading path (\"Operations > Self-check\")"),
			"maxLevel": integer(1, 6, "only headings down to this level (default 6: all)")}, "doc"),
		output: obj(map[string]any{
			"doc": strType, "version": strType,
			"headings": map[string]any{"type": "array", "items": obj(map[string]any{
				"level": intType, "title": strType, "id": strType, "line": intType, "lines": intType,
				"bytes": intType}, "level", "title", "id", "line", "lines", "bytes")},
			"lines": intType, "bytes": intType, "truncated": map[string]any{"type": "boolean"}},
			"doc", "version", "headings", "lines", "bytes", "truncated"),
		run: outline}
}

func outline(s *Server, ctx context.Context, args json.RawMessage) (*result, error) {
	var a struct {
		Doc      string `json:"doc"`
		Section  string `json:"section"`
		MaxLevel int    `json:"maxLevel"`
	}
	if err := decode(args, &a); err != nil {
		return nil, err
	}
	if a.MaxLevel == 0 {
		a.MaxLevel = 6
	}
	if a.MaxLevel < 1 || a.MaxLevel > 6 {
		return nil, errors.New("maxLevel: from 1 to 6")
	}
	d, sec, e, err := s.load(ctx, a.Doc)
	if err != nil {
		return nil, err
	}
	if a.Section != "" {
		sec = a.Section
	}
	o := e.Outline
	within := -1
	if sec != "" {
		if within, err = findSection(d, o, sec); err != nil {
			return nil, err
		}
	}
	var out strings.Builder
	fmt.Fprintf(&out, "%s: %d lines, %s (%s), version %s\n", d.Address(), o.Lines, size(o.Bytes), tokens(o.Bytes), e.Version)
	if within >= 0 {
		h := o.Headings[within]
		fmt.Fprintf(&out, "§ %s: lines %d-%d\n", strings.Join(o.Path(within), " > "), h.Line, h.End())
	}
	shown := []markdown.Heading{}
	truncated := false
	w := len(strconv.Itoa(o.Lines))
	for i, h := range o.Headings {
		if h.Level > a.MaxLevel || (within >= 0 && !o.Within(i, within)) {
			continue
		}
		line := fmt.Sprintf("%*d  %s %s  #%s  [%d lines, %s]\n", w, h.Line, strings.Repeat("#", h.Level), h.Title,
			h.ID, h.Lines, size(h.Bytes))
		if int64(out.Len()+len(line)) > s.MaxRead-256 {
			truncated = true
			break
		}
		out.WriteString(line)
		shown = append(shown, h)
	}
	switch {
	case len(o.Headings) == 0:
		out.WriteString("no headings: read it with read_lines (offset and limit), or search it")
	case truncated:
		out.WriteString("(more headings than one call returns: give maxLevel, or section)")
	default:
		out.WriteString("(read a section: read_section with doc and its id)")
	}
	return &result{text: out.String(), structured: map[string]any{"doc": d.Address(), "version": e.Version,
		"headings": shown, "lines": o.Lines, "bytes": o.Bytes, "truncated": truncated}}, nil
}

// findSection finds a section by id, or by heading path: titles
// separated by ">" (or "›"), the last ones of a heading's path,
// ignoring case.
func findSection(d catalog.Doc, o *markdown.Outline, sec string) (int, error) {
	sec = strings.TrimPrefix(strings.TrimSpace(sec), "#")
	for i, h := range o.Headings {
		if h.ID == sec {
			return i, nil
		}
	}
	want := splitPath(sec)
	var found []int
	for i := range o.Headings {
		p := o.Path(i)
		if len(p) < len(want) {
			continue
		}
		p = p[len(p)-len(want):]
		match := true
		for k := range want {
			if !sameTitle(p[k], want[k]) {
				match = false
				break
			}
		}
		if match {
			found = append(found, i)
		}
	}
	switch len(found) {
	case 1:
		return found[0], nil
	case 0:
		var ids []string
		for _, h := range o.Headings {
			if h.Level <= 2 && len(ids) < 30 {
				ids = append(ids, "#"+h.ID)
			}
		}
		hint := "it has no headings"
		if len(ids) > 0 {
			hint = "its sections include " + strings.Join(ids, ", ") + " (outline lists them all)"
		}
		return -1, fmt.Errorf("%s: no section %q; %s", d.Address(), sec, hint)
	}
	var c []string
	for _, i := range found {
		c = append(c, fmt.Sprintf("#%s (%s)", o.Headings[i].ID, strings.Join(o.Path(i), " > ")))
	}
	return -1, fmt.Errorf("%s: %q names %d sections: %s; give its id", d.Address(), sec, len(found), strings.Join(c, ", "))
}

// sameTitle compares a heading's title with one asked for, ignoring
// case and a leading section number ("11. Roadmap", "6.6 Policy").
func sameTitle(title, want string) bool {
	title, want = strings.TrimSpace(title), strings.TrimSpace(want)
	return strings.EqualFold(title, want) || strings.EqualFold(sectionNumber.ReplaceAllString(title, ""), sectionNumber.ReplaceAllString(want, ""))
}

var sectionNumber = regexp.MustCompile(`^(?:\d+\.)*\d+\.?\s+`)

func splitPath(p string) []string {
	p = strings.ReplaceAll(p, "›", ">")
	var out []string
	for _, part := range strings.Split(p, ">") {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}

// --- read_section ------------------------------------------------------------

func readSectionTool(s *Server) tool {
	return tool{name: "read_section", title: "Read a section",
		description: "The text of one section of a document, from its heading to the next heading of the same or " +
			"a higher level. section is an id from outline or search (or give it in doc after #), or a heading " +
			"path. depth limits the subsections included whole (0: the section's introduction, then the outline " +
			"of its subsections). A section larger than " + size(s.MaxRead) + " comes in parts: the first says " +
			"how many and what the rest holds; ask for part 2, ….",
		input: obj(map[string]any{"doc": docArg,
			"section": str("the section's id (from outline or search), or its heading path (\"Operations > Self-check\")"),
			"depth":   integer(0, 6, "levels of subsections to include whole (default: all)"),
			"part":    integer(1, 0, "the part of a large section (default 1)")}, "doc"),
		run: readSection}
}

// unit is a line of a section, or a subsection left out.
type unit struct {
	line int // 0: a subsection left out
	text string
	head bool // the line is a heading
}

func readSection(s *Server, ctx context.Context, args json.RawMessage) (*result, error) {
	var a struct {
		Doc     string `json:"doc"`
		Section string `json:"section"`
		Depth   *int   `json:"depth"`
		Part    int    `json:"part"`
	}
	if err := decode(args, &a); err != nil {
		return nil, err
	}
	if a.Part == 0 {
		a.Part = 1
	}
	if a.Part < 1 || (a.Depth != nil && *a.Depth < 0) {
		return nil, errors.New("part is from 1, depth from 0")
	}
	d, sec, e, err := s.load(ctx, a.Doc)
	if err != nil {
		return nil, err
	}
	if a.Section != "" {
		sec = a.Section
	}
	if sec == "" {
		return nil, errors.New("section: give a section's id (outline lists them), or read_lines for a whole document")
	}
	o := e.Outline
	i, err := findSection(d, o, sec)
	if err != nil {
		return nil, err
	}
	h := o.Headings[i]
	lines, err := s.Cat.Lines(d, e, h.Line, h.End())
	if err != nil {
		return nil, err
	}
	starts := map[int]int{} // line → heading
	for j := i; j < len(o.Headings) && o.Headings[j].Line <= h.End(); j++ {
		starts[o.Headings[j].Line] = j
	}
	var units []unit
	for n := h.Line; n <= h.End() && n-h.Line < len(lines); n++ {
		j, isHead := starts[n]
		if isHead && a.Depth != nil && o.Headings[j].Level > h.Level+*a.Depth {
			sub := o.Headings[j]
			units = append(units, unit{text: fmt.Sprintf("… %s %s  #%s  [%d lines, %s: read_section with its id]\n",
				strings.Repeat("#", sub.Level), sub.Title, sub.ID, sub.Lines, size(sub.Bytes)), head: true})
			n = sub.End()
			continue
		}
		units = append(units, unit{line: n, text: lines[n-h.Line], head: isHead})
	}

	parts := pack(units, max(s.MaxRead-1024, 1024))
	if a.Part > len(parts) {
		return nil, fmt.Errorf("%s#%s: has %d part(s)", d.Address(), h.ID, len(parts))
	}
	p := parts[a.Part-1]
	var out strings.Builder
	first, last := 0, 0
	for _, u := range units[p[0]:p[1]] {
		out.WriteString(u.text)
		if u.line > 0 {
			if first == 0 {
				first = u.line
			}
			last = u.line
		}
	}
	body := out.String()
	if body != "" && !strings.HasSuffix(body, "\n") {
		out.WriteString("\n")
	}
	fmt.Fprintf(&out, "[§ #%s, lines %d-%d of %d", h.ID, first, last, o.Lines)
	if len(parts) > 1 {
		fmt.Fprintf(&out, ", part %d of %d", a.Part, len(parts))
	}
	out.WriteString("]")
	if a.Part < len(parts) {
		next := units[parts[a.Part][0]].line
		fmt.Fprintf(&out, "\n(continued in part %d", a.Part+1)
		if next > 0 {
			fmt.Fprintf(&out, ", from line %d", next)
		}
		out.WriteString("; the rest of the section:")
		shown := 0
		for j := i + 1; j < len(o.Headings) && o.Headings[j].Line <= h.End(); j++ {
			sub := o.Headings[j]
			if sub.Line <= last || (a.Depth != nil && sub.Level > h.Level+*a.Depth+1) {
				continue
			}
			if shown == 30 {
				out.WriteString("\n  … (outline with section lists them all)")
				break
			}
			fmt.Fprintf(&out, "\n  %d  %s %s  #%s  [%d lines, %s]", sub.Line, strings.Repeat("#", sub.Level),
				sub.Title, sub.ID, sub.Lines, size(sub.Bytes))
			shown++
		}
		if shown == 0 {
			out.WriteString(" no more headings")
		}
		out.WriteString(")")
	}
	return &result{text: out.String()}, nil
}

// pack splits units into parts of at most budget bytes: [from, to)
// each. A part that must be cut is cut before a heading in its second
// half if there is one; a single line longer than the budget is cut.
func pack(units []unit, budget int64) [][2]int {
	var parts [][2]int
	start := 0
	for start < len(units) {
		var n int64
		end := start
		for end < len(units) && n+int64(len(units[end].text)) <= budget {
			n += int64(len(units[end].text))
			end++
		}
		if end == len(units) {
			parts = append(parts, [2]int{start, end})
			break
		}
		if end == start { // one line longer than a part
			units[start].text = cutText(units[start].text, budget)
			end = start + 1
		} else {
			var m int64
			best := -1
			for k := start; k < end; k++ {
				if k > start && units[k].head && m >= budget/2 {
					best = k
				}
				m += int64(len(units[k].text))
			}
			if best > start {
				end = best
			}
		}
		parts = append(parts, [2]int{start, end})
		start = end
	}
	if len(parts) == 0 {
		parts = append(parts, [2]int{0, 0})
	}
	return parts
}

func cutText(t string, budget int64) string {
	b := []byte(t[:budget-8])
	for len(b) > 0 && b[len(b)-1]&0xC0 == 0x80 {
		b = b[:len(b)-1]
	}
	if len(b) > 0 && b[len(b)-1] >= 0xC0 {
		b = b[:len(b)-1]
	}
	return string(b) + " …\n"
}

// --- read_lines --------------------------------------------------------------

func readLinesTool(s *Server) tool {
	return tool{name: "read_lines", title: "Read lines of a document",
		description: "Lines of a document by number: from offset (limit of them), the first (head) or the last " +
			"(tail) lines, or all of it. For the lines around a search match, and documents without headings; " +
			"a section is easier read with read_section. With version (from outline or search), a document " +
			"changed since is an error, not the wrong lines. At most " + size(s.MaxRead) + " per call: the result " +
			"says which lines it holds and where to go on.",
		input: obj(map[string]any{"doc": docArg,
			"offset":  integer(1, 0, "the first line to read (from 1)"),
			"limit":   integer(1, 0, "how many lines from offset (default: to the end)"),
			"head":    integer(1, 0, "only the first N lines"),
			"tail":    integer(1, 0, "only the last N lines"),
			"version": str("the document's version from outline or search: fail if it changed")}, "doc"),
		run: readLines}
}

func readLines(s *Server, ctx context.Context, args json.RawMessage) (*result, error) {
	var a struct {
		Doc     string `json:"doc"`
		Offset  int    `json:"offset"`
		Limit   int    `json:"limit"`
		Head    int    `json:"head"`
		Tail    int    `json:"tail"`
		Version string `json:"version"`
	}
	if err := decode(args, &a); err != nil {
		return nil, err
	}
	d, _, e, err := s.load(ctx, a.Doc)
	if err != nil {
		return nil, err
	}
	if a.Version != "" && a.Version != e.Version {
		return nil, fmt.Errorf("%s: changed since version %s (now %s): outline or search it again for the line numbers",
			d.Address(), a.Version, e.Version)
	}
	return s.lines(d, e, a.Offset, a.Limit, a.Head, a.Tail, d.Address())
}

// lines reads a range of lines (offset and limit, head or tail), up to
// MaxRead bytes, and says which it holds.
func (s *Server) lines(d catalog.Doc, e *catalog.Entry, offset, limit, head, tail int, name string) (*result, error) {
	if head < 0 || tail < 0 || offset < 0 || limit < 0 {
		return nil, errors.New("head, tail, offset and limit are positive numbers of lines")
	}
	ranged := offset > 0 || limit > 0
	if (head > 0 && tail > 0) || (ranged && (head > 0 || tail > 0)) {
		return nil, errors.New("give one of head, tail, or offset and limit")
	}
	n := e.Outline.Lines
	from, to := max(offset, 1), n
	switch {
	case head > 0:
		to = min(head, n)
	case tail > 0:
		from = max(n-tail+1, 1)
	case limit > 0:
		to = min(from+limit-1, n)
	}
	if n == 0 {
		return &result{text: name + ": empty"}, nil
	}
	if from > n {
		return nil, fmt.Errorf("%s: has %d lines, offset %d is past its end", name, n, from)
	}
	ls, err := s.Cat.Lines(d, e, from, to)
	if err != nil {
		return nil, err
	}
	var out strings.Builder
	last := from - 1
	budget := s.MaxRead - 256
	for _, l := range ls {
		if int64(out.Len()+len(l)) > budget {
			if last < from {
				out.WriteString(cutText(l, budget))
				last = from
			}
			break
		}
		out.WriteString(l)
		last++
	}
	if out.Len() > 0 && !strings.HasSuffix(out.String(), "\n") {
		out.WriteString("\n")
	}
	fmt.Fprintf(&out, "[lines %d-%d of %d]", from, last, n)
	if last < to {
		fmt.Fprintf(&out, "\n(stopped at the %s one call returns: go on with offset %d", size(s.MaxRead), last+1)
		if len(e.Outline.Headings) > 0 {
			out.WriteString(", or read by section: outline")
		}
		out.WriteString(")")
	}
	return &result{text: out.String()}, nil
}
