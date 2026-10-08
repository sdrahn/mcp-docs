package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"strconv"
	"strings"

	"github.com/sdrahn/mcp-docs/internal/catalog"
)

// With --fs-compat the server also offers mcp-server-fs's reading tools
// with their names, arguments and output (§5.7), so that mcp-gateway's
// gateway-docs server, its roles and agents' habits move to mcp-docs
// unchanged. Paths are file paths: absolute, or relative to the first
// collection's directory. Only documents are seen.

var pathArg = str("absolute path, or relative to the first allowed directory")

var excludesArg = map[string]any{"type": "array", "items": strType,
	"description": "glob patterns of paths to leave out (\"node_modules\", \"**/.git\")"}

func compatTools(s *Server) []tool {
	search := map[string]any{"path": pathArg, "excludePatterns": excludesArg}
	for k, v := range queryArgs {
		search[k] = v
	}
	return []tool{
		{name: "outline_file", title: "Outline of a Markdown file", compat: true,
			description: "The headings of a Markdown file with their line numbers and how many lines and bytes each " +
				"section has (to the next heading of the same or a higher level). Read one section with " +
				"read_text_file (offset: its line, limit: its lines) instead of the whole file. Headings in fenced " +
				"code blocks are not headings.",
			input: obj(map[string]any{"path": pathArg,
				"maxLevel": integer(1, 6, "only headings down to this level (default 6: all)")}, "path"),
			output: obj(map[string]any{
				"headings": map[string]any{"type": "array", "items": obj(map[string]any{
					"level": intType, "title": strType, "line": intType, "lines": intType, "bytes": intType},
					"level", "title", "line", "lines", "bytes")},
				"lines": intType, "bytes": intType}, "headings", "lines", "bytes"),
			run: outlineFile},
		{name: "search_text", title: "Search text in files", compat: true,
			description: "Find the lines of text files below a path, or in one file, that contain a text (or match a " +
				"regular expression with regexp: true), case-insensitive unless caseSensitive. Gives each match " +
				"with its file, line number and context lines; read more around it with read_text_file (offset, limit).",
			input: obj(search, "path", "query"),
			output: obj(map[string]any{
				"matches": map[string]any{"type": "array", "items": obj(map[string]any{
					"path": strType, "line": intType, "text": strType}, "path", "line", "text")},
				"truncated": map[string]any{"type": "boolean"}}, "matches", "truncated"),
			run: searchText},
		{name: "read_text_file", title: "Read a text file", compat: true,
			description: "Read a text file whole, its first (head) or last (tail) lines, or the lines from offset " +
				"(limit of them), e.g. around a match of search_text. Give one of head, tail, or offset and limit. " +
				"At most " + size(s.MaxRead) + " per call.",
			input: obj(map[string]any{"path": pathArg,
				"head":   integer(1, 0, "only the first N lines"),
				"tail":   integer(1, 0, "only the last N lines"),
				"offset": integer(1, 0, "the first line to read (from 1)"),
				"limit":  integer(1, 0, "how many lines from offset (default: to the end)")}, "path"),
			run: readTextFile},
		{name: "search_files", title: "Find files", compat: true,
			description: fmt.Sprintf("Find files and directories below a path whose path matches a glob pattern: \"*.md\" "+
				"matches names at any depth, \"guide/**/*.md\" paths relative to the start; at most %d matches. "+
				"Text inside files: search_text.", s.Cat.MaxEntries),
			input: obj(map[string]any{"path": pathArg, "pattern": str("glob pattern"), "excludePatterns": excludesArg},
				"path", "pattern"),
			output: obj(map[string]any{"matches": map[string]any{"type": "array", "items": strType},
				"truncated": map[string]any{"type": "boolean"}}, "matches", "truncated"),
			run: searchFiles},
		{name: "list_directory", title: "List a directory", compat: true,
			description: fmt.Sprintf("List a directory's entries, each marked [DIR] or [FILE] (at most %d).", s.Cat.MaxEntries),
			input:       obj(map[string]any{"path": pathArg}, "path"),
			run:         listDirectory},
	}
}

// file resolves a file path argument.
func (s *Server) file(p string) (catalog.Doc, error) {
	if p == "" {
		return catalog.Doc{}, errors.New("path is required")
	}
	d, _, err := s.Cat.Resolve(p, true)
	return d, err
}

func outlineFile(s *Server, ctx context.Context, args json.RawMessage) (*result, error) {
	a := struct {
		Path     string `json:"path"`
		MaxLevel int    `json:"maxLevel"`
	}{}
	if err := decode(args, &a); err != nil {
		return nil, err
	}
	if a.MaxLevel == 0 {
		a.MaxLevel = 6
	}
	if a.MaxLevel < 1 || a.MaxLevel > 6 {
		return nil, errors.New("maxLevel: from 1 to 6")
	}
	d, err := s.file(a.Path)
	if err != nil {
		return nil, err
	}
	e, err := s.Cat.Load(ctx, d)
	if err != nil {
		return nil, err
	}
	o := e.Outline
	type heading struct {
		Level int    `json:"level"`
		Title string `json:"title"`
		Line  int    `json:"line"`
		Lines int    `json:"lines"`
		Bytes int64  `json:"bytes"`
	}
	shown := []heading{}
	for _, h := range o.Headings {
		if h.Level <= a.MaxLevel {
			shown = append(shown, heading{h.Level, h.Title, h.Line, h.Lines, h.Bytes})
		}
	}
	var out strings.Builder
	fmt.Fprintf(&out, "%s: %d lines, %s\n", d.Full(), o.Lines, size(o.Bytes))
	if len(shown) == 0 {
		out.WriteString("no Markdown headings: read it with read_text_file (head, or offset and limit)")
	} else {
		w := len(strconv.Itoa(shown[len(shown)-1].Line))
		for _, h := range shown {
			fmt.Fprintf(&out, "%*d  %s %s  [%d lines, %s]\n", w, h.Line, strings.Repeat("#", h.Level), h.Title,
				h.Lines, size(h.Bytes))
		}
		out.WriteString("(read a section: read_text_file with offset = its line, limit = its lines)")
	}
	return &result{text: out.String(), structured: map[string]any{"headings": shown, "lines": o.Lines, "bytes": o.Bytes}}, nil
}

func searchText(s *Server, ctx context.Context, args json.RawMessage) (*result, error) {
	var a struct {
		queryOpts
		Path            string   `json:"path"`
		ExcludePatterns []string `json:"excludePatterns"`
	}
	if err := decode(args, &a); err != nil {
		return nil, err
	}
	d, err := s.file(a.Path)
	if err != nil {
		return nil, err
	}
	sr, err := s.newSearch(a.queryOpts, true)
	if err != nil {
		return nil, err
	}
	if err := sr.below(ctx, d, excluder(d, a.ExcludePatterns)); err != nil {
		return nil, err
	}
	res := sr.result()
	type textMatch struct {
		Path string `json:"path"`
		Line int    `json:"line"`
		Text string `json:"text"`
	}
	ms := []textMatch{}
	for _, m := range sr.matches {
		ms = append(ms, textMatch{m.Doc, m.Line, m.Text})
	}
	res.structured = map[string]any{"matches": ms, "truncated": sr.truncated}
	return res, nil
}

// excluder leaves out what a pattern matches, relative to start.
func excluder(start catalog.Doc, patterns []string) func(catalog.Doc) bool {
	if len(patterns) == 0 {
		return nil
	}
	return func(d catalog.Doc) bool {
		rel := relTo(start, d)
		for _, p := range patterns {
			if globMatch(p, rel) {
				return true
			}
		}
		return false
	}
}

func relTo(start, d catalog.Doc) string {
	if start.Rel == "." {
		return d.Rel
	}
	return strings.TrimPrefix(d.Rel, start.Rel+"/")
}

func readTextFile(s *Server, ctx context.Context, args json.RawMessage) (*result, error) {
	var a struct {
		Path   string `json:"path"`
		Head   int    `json:"head"`
		Tail   int    `json:"tail"`
		Offset int    `json:"offset"`
		Limit  int    `json:"limit"`
	}
	if err := decode(args, &a); err != nil {
		return nil, err
	}
	d, err := s.file(a.Path)
	if err != nil {
		return nil, err
	}
	e, err := s.Cat.Load(ctx, d)
	if err != nil {
		return nil, err
	}
	return s.lines(d, e, a.Offset, a.Limit, a.Head, a.Tail, d.Full())
}

func searchFiles(s *Server, ctx context.Context, args json.RawMessage) (*result, error) {
	var a struct {
		Path            string   `json:"path"`
		Pattern         string   `json:"pattern"`
		ExcludePatterns []string `json:"excludePatterns"`
	}
	if err := decode(args, &a); err != nil {
		return nil, err
	}
	d, err := s.file(a.Path)
	if err != nil {
		return nil, err
	}
	if !validGlob(a.Pattern) {
		return nil, fmt.Errorf("pattern: %q is not a valid glob pattern", a.Pattern)
	}
	matches := []string{}
	truncated := false
	err = s.Cat.Walk(ctx, d, excluder(d, a.ExcludePatterns), func(doc catalog.Doc, _ fs.DirEntry) bool {
		if !globMatch(a.Pattern, relTo(d, doc)) {
			return true
		}
		if len(matches) >= s.Cat.MaxEntries {
			truncated = true
			return false
		}
		matches = append(matches, doc.Full())
		return true
	})
	if err != nil {
		return nil, err
	}
	body := strings.Join(matches, "\n")
	switch {
	case len(matches) == 0:
		body = "no matches"
	case truncated:
		body += fmt.Sprintf("\n(stopped at %d entries)", s.Cat.MaxEntries)
	}
	return &result{text: body, structured: map[string]any{"matches": matches, "truncated": truncated}}, nil
}

func listDirectory(s *Server, ctx context.Context, args json.RawMessage) (*result, error) {
	var a struct {
		Path string `json:"path"`
	}
	if err := decode(args, &a); err != nil {
		return nil, err
	}
	d, err := s.file(a.Path)
	if err != nil {
		return nil, err
	}
	fi, err := s.Cat.Stat(d)
	if err != nil {
		return nil, err
	}
	if !fi.IsDir() {
		return nil, fmt.Errorf("%s: not a directory", d.Full())
	}
	var lines []string
	oneLevel := func(doc catalog.Doc) bool { return strings.Contains(relTo(d, doc), "/") }
	err = s.Cat.Walk(ctx, d, oneLevel, func(doc catalog.Doc, e fs.DirEntry) bool {
		if len(lines) >= s.Cat.MaxEntries {
			lines = append(lines, fmt.Sprintf("(stopped at %d entries)", s.Cat.MaxEntries))
			return false
		}
		kind := "[FILE] "
		if e.IsDir() {
			kind = "[DIR] "
		}
		lines = append(lines, kind+path.Base(doc.Rel))
		return true
	})
	if err != nil {
		return nil, err
	}
	return &result{text: strings.Join(lines, "\n")}, nil
}
