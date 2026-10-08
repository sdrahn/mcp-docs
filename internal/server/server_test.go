package server

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/sdrahn/mcp-docs/internal/catalog"
)

// newServer returns a server on one collection per name, each a fresh
// directory (returned by name).
func newServer(t *testing.T, maxRead int64, names ...string) (*Server, map[string]string) {
	t.Helper()
	dirs := map[string]string{}
	var colls []*catalog.Collection
	for _, n := range names {
		d := t.TempDir()
		dirs[n] = d
		colls = append(colls, &catalog.Collection{Name: n, Path: d, Description: "the " + n + " docs"})
	}
	return &Server{Cat: catalog.New(colls, []string{".md", ".markdown"}, 1000), MaxRead: maxRead, MaxFile: 1 << 20,
		FSCompat: true, Version: "test"}, dirs
}

type callResult struct {
	Content []struct {
		Text string `json:"text"`
	} `json:"content"`
	IsError    bool           `json:"isError"`
	Structured map[string]any `json:"structuredContent"`
}

func (r callResult) text() string {
	var b strings.Builder
	for _, c := range r.Content {
		b.WriteString(c.Text)
	}
	return b.String()
}

func call(t *testing.T, s *Server, name string, args any) callResult {
	t.Helper()
	raw, _ := json.Marshal(args)
	res, err := s.call(context.Background(), name, raw)
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	b, _ := json.Marshal(res)
	var r callResult
	if err := json.Unmarshal(b, &r); err != nil {
		t.Fatal(err)
	}
	return r
}

func mustOK(t *testing.T, r callResult) string {
	t.Helper()
	if r.IsError {
		t.Fatalf("tool error: %s", r.text())
	}
	return r.text()
}

func mustFail(t *testing.T, r callResult, want string) {
	t.Helper()
	if !r.IsError || !strings.Contains(r.text(), want) {
		t.Fatalf("want a tool error with %q, got isError=%v %q", want, r.IsError, r.text())
	}
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

const guide = "# Guide\n" + // 1
	"Intro text.\n" + // 2
	"## Install\n" + // 3
	"Run the installer.\n" + // 4
	"### Packages\n" + // 5
	"zypper in thing\n" + // 6
	"## Configure\n" + // 7
	"Set `timeout` in the file.\n" + // 8
	"### Timeouts\n" + // 9
	"timeout: 30s\n" + // 10
	"more\n" + // 11
	"## Install\n" + // 12
	"Again.\n" // 13

func TestListDocs(t *testing.T) {
	s, dirs := newServer(t, 32<<10, "app")
	write(t, filepath.Join(dirs["app"], "guide.md"), guide)
	write(t, filepath.Join(dirs["app"], "README.md"), "# App docs\nsee guide.md\n")
	write(t, filepath.Join(dirs["app"], "sub", "deep.markdown"), "# Deep\n")
	write(t, filepath.Join(dirs["app"], "notes.txt"), "not a document\n")
	write(t, filepath.Join(dirs["app"], ".git", "x.md"), "# hidden\n")

	got := mustOK(t, call(t, s, "list_docs", map[string]any{}))
	for _, want := range []string{"app: 3 documents", "app/README.md — App docs (index: read it first)  [2 lines",
		"app/guide.md — Guide  [13 lines", "app/sub/deep.markdown — Deep"} {
		if !strings.Contains(got, want) {
			t.Errorf("list_docs: no %q in\n%s", want, got)
		}
	}
	if strings.Contains(got, "notes.txt") || strings.Contains(got, "hidden") {
		t.Errorf("list_docs lists what is no document:\n%s", got)
	}
	if strings.Index(got, "README.md") > strings.Index(got, "guide.md") {
		t.Errorf("the index is not first:\n%s", got)
	}
	got = mustOK(t, call(t, s, "list_docs", map[string]any{"path": "app/sub"}))
	if !strings.Contains(got, "app/sub/deep.markdown") || strings.Contains(got, "guide.md") {
		t.Errorf("list_docs path:\n%s", got)
	}
	mustFail(t, call(t, s, "list_docs", map[string]any{"collection": "nope"}), "no such collection")
	mustFail(t, call(t, s, "list_docs", map[string]any{"path": "app/guide.md"}), "not a directory")
}

func TestListCollections(t *testing.T) {
	s, dirs := newServer(t, 32<<10, "one", "two")
	write(t, filepath.Join(dirs["one"], "a.md"), "# A\n")
	write(t, filepath.Join(dirs["two"], "README.md"), "# B\n")
	r := call(t, s, "list_docs", map[string]any{})
	got := mustOK(t, r)
	if !strings.Contains(got, "one: the one docs\n  1 documents") || !strings.Contains(got, "index two/README.md") {
		t.Errorf("collections:\n%s", got)
	}
	if cs := r.Structured["collections"].([]any); len(cs) != 2 {
		t.Errorf("structured: %v", r.Structured)
	}
	got = mustOK(t, call(t, s, "list_docs", map[string]any{"collection": "two"}))
	if !strings.Contains(got, "two/README.md") || strings.Contains(got, "a.md") {
		t.Errorf("one collection:\n%s", got)
	}
}

func TestOutline(t *testing.T) {
	s, dirs := newServer(t, 32<<10, "app")
	write(t, filepath.Join(dirs["app"], "guide.md"), guide)
	r := call(t, s, "outline", map[string]any{"doc": "app/guide.md"})
	got := mustOK(t, r)
	e, _ := s.Cat.Load(context.Background(), catalog.Doc{Coll: s.Cat.Collections[0], Rel: "guide.md"})
	want := "app/guide.md: 13 lines, " + size(int64(len(guide))) + " (~" + fmt.Sprint((len(guide)+3)/4) +
		" tokens), version " + e.Version + "\n" +
		" 1  # Guide  #guide  [13 lines, 168 B]\n" +
		" 3  ## Install  #install  [4 lines, 59 B]\n" +
		" 5  ### Packages  #packages  [2 lines, 29 B]\n" +
		" 7  ## Configure  #configure  [5 lines, 71 B]\n" +
		" 9  ### Timeouts  #timeouts  [3 lines, 31 B]\n" +
		"12  ## Install  #install-1  [2 lines, 18 B]\n" +
		"(read a section: read_section with doc and its id)"
	if got != want {
		t.Errorf("outline:\n%s\nwant:\n%s", got, want)
	}
	if r.Structured["version"] != e.Version || len(r.Structured["headings"].([]any)) != 6 {
		t.Errorf("structured: %v", r.Structured)
	}

	got = mustOK(t, call(t, s, "outline", map[string]any{"doc": "app/guide.md", "maxLevel": 2}))
	if strings.Contains(got, "Packages") || !strings.Contains(got, "#install-1") {
		t.Errorf("maxLevel:\n%s", got)
	}
	got = mustOK(t, call(t, s, "outline", map[string]any{"doc": "app/guide.md#configure"}))
	if !strings.Contains(got, "§ Guide > Configure: lines 7-11") || strings.Contains(got, "#install") ||
		!strings.Contains(got, "#timeouts") {
		t.Errorf("section:\n%s", got)
	}
	mustFail(t, call(t, s, "outline", map[string]any{"doc": "app/guide.md", "maxLevel": 7}), "maxLevel")
	mustFail(t, call(t, s, "outline", map[string]any{"doc": "app/nope.md"}), "no such document")
	mustFail(t, call(t, s, "outline", map[string]any{"doc": "app"}), "is a directory")
	mustFail(t, call(t, s, "outline", map[string]any{"doc": "app/guide.md", "bogus": 1}), "invalid arguments")
}

func TestReadSection(t *testing.T) {
	s, dirs := newServer(t, 32<<10, "app")
	write(t, filepath.Join(dirs["app"], "guide.md"), guide)
	want := "## Configure\nSet `timeout` in the file.\n### Timeouts\ntimeout: 30s\nmore\n[§ #configure, lines 7-11 of 13]"
	for _, args := range []map[string]any{
		{"doc": "app/guide.md", "section": "configure"},
		{"doc": "app/guide.md#configure"},
		{"doc": "guide.md#configure"}, // relative to the first collection
		{"doc": "app/guide.md", "section": "Guide > Configure"},
		{"doc": "app/guide.md", "section": "configure"},
		{"doc": filepath.Join(dirs["app"], "guide.md"), "section": "#configure"},
	} {
		if got := mustOK(t, call(t, s, "read_section", args)); got != want {
			t.Errorf("%v:\n%s\nwant:\n%s", args, got, want)
		}
	}
	got := mustOK(t, call(t, s, "read_section", map[string]any{"doc": "app/guide.md#configure", "depth": 0}))
	if got != "## Configure\nSet `timeout` in the file.\n"+
		"… ### Timeouts  #timeouts  [3 lines, 31 B: read_section with its id]\n[§ #configure, lines 7-8 of 13]" {
		t.Errorf("depth 0:\n%s", got)
	}
	mustFail(t, call(t, s, "read_section", map[string]any{"doc": "app/guide.md", "section": "Install"}),
		`names 2 sections: #install (Guide > Install), #install-1 (Guide > Install)`)
	mustFail(t, call(t, s, "read_section", map[string]any{"doc": "app/guide.md", "section": "nope"}),
		`no section "nope"; its sections include #guide, #install, #configure, #install-1`)
	mustFail(t, call(t, s, "read_section", map[string]any{"doc": "app/guide.md"}), "section: give")
	mustFail(t, call(t, s, "read_section", map[string]any{"doc": "app/guide.md#configure", "part": 2}), "has 1 part")
}

// A heading path matches titles without their section numbers.
func TestSectionNumbers(t *testing.T) {
	s, dirs := newServer(t, 32<<10, "app")
	write(t, filepath.Join(dirs["app"], "a.md"), "# A\n## 11. Roadmap\nx\n### 6.6 Policy lifecycle\ny\n")
	if got := mustOK(t, call(t, s, "read_section", map[string]any{"doc": "app/a.md", "section": "Roadmap > Policy lifecycle"})); !strings.HasPrefix(got, "### 6.6 Policy lifecycle\ny\n") {
		t.Errorf("numbered path: %q", got)
	}
}

// A section larger than one call comes in parts, cut before a heading
// when one is in the second half; each part names the rest.
func TestReadSectionParts(t *testing.T) {
	s, dirs := newServer(t, 4096, "app")
	var b strings.Builder
	b.WriteString("# Big\n")
	for i := 1; i <= 6; i++ {
		fmt.Fprintf(&b, "## Part %d\n", i)
		for j := 0; j < 20; j++ {
			fmt.Fprintf(&b, "line %d.%d %s\n", i, j, strings.Repeat("x", 40))
		}
	}
	write(t, filepath.Join(dirs["app"], "big.md"), b.String())

	got := mustOK(t, call(t, s, "read_section", map[string]any{"doc": "app/big.md#big"}))
	m := regexp.MustCompile(`part 1 of (\d+)\]`).FindStringSubmatch(got)
	if m == nil || len(got) > 4096 {
		t.Fatalf("part 1 (%d bytes):\n%s", len(got), got)
	}
	parts := 0
	fmt.Sscan(m[1], &parts)
	if !strings.Contains(got, "(continued in part 2, from line ") || !strings.Contains(got, "## Part 6  #part-6") {
		t.Errorf("part 1 does not say what follows:\n%s", got[len(got)-600:])
	}
	// The cut is before a heading: the next part starts with one.
	var all strings.Builder
	for p := 1; p <= parts; p++ {
		got := mustOK(t, call(t, s, "read_section", map[string]any{"doc": "app/big.md#big", "part": p}))
		if len(got) > 4096 {
			t.Errorf("part %d: %d bytes", p, len(got))
		}
		if p > 1 && !strings.HasPrefix(got, "## Part ") {
			t.Errorf("part %d does not start at a heading: %q", p, got[:40])
		}
		all.WriteString(got[:strings.Index(got, "[§ #big")])
	}
	if all.String() != b.String() {
		t.Error("the parts together are not the section")
	}

	// One line longer than a part is cut.
	write(t, filepath.Join(dirs["app"], "long.md"), "# Long\n"+strings.Repeat("y", 10000)+"\n")
	got = mustOK(t, call(t, s, "read_section", map[string]any{"doc": "app/long.md#long", "part": 2}))
	if len(got) > 4096 || !strings.Contains(got, " …\n") || !strings.Contains(got, "part 2 of 2") {
		t.Errorf("long line: %d bytes %q", len(got), got[len(got)-100:])
	}
}

func TestReadLines(t *testing.T) {
	s, dirs := newServer(t, 4096, "app")
	write(t, filepath.Join(dirs["app"], "guide.md"), guide)
	got := mustOK(t, call(t, s, "read_lines", map[string]any{"doc": "app/guide.md", "offset": 9, "limit": 2}))
	if got != "### Timeouts\ntimeout: 30s\n[lines 9-10 of 13]" {
		t.Errorf("range: %q", got)
	}
	if got := mustOK(t, call(t, s, "read_lines", map[string]any{"doc": "app/guide.md", "tail": 2})); got != "## Install\nAgain.\n[lines 12-13 of 13]" {
		t.Errorf("tail: %q", got)
	}
	if got := mustOK(t, call(t, s, "read_lines", map[string]any{"doc": "app/guide.md", "head": 1})); got != "# Guide\n[lines 1-1 of 13]" {
		t.Errorf("head: %q", got)
	}
	if got := mustOK(t, call(t, s, "read_lines", map[string]any{"doc": "app/guide.md"})); got != guide+"[lines 1-13 of 13]" {
		t.Errorf("whole: %q", got)
	}
	mustFail(t, call(t, s, "read_lines", map[string]any{"doc": "app/guide.md", "offset": 20}), "has 13 lines")
	mustFail(t, call(t, s, "read_lines", map[string]any{"doc": "app/guide.md", "head": 1, "tail": 1}), "give one of")

	// Lines past a stored offset are found by seeking.
	var b strings.Builder
	for i := 1; i <= 1000; i++ {
		fmt.Fprintf(&b, "line %d\n", i)
	}
	write(t, filepath.Join(dirs["app"], "long.md"), b.String())
	if got := mustOK(t, call(t, s, "read_lines", map[string]any{"doc": "app/long.md", "offset": 600, "limit": 2})); got != "line 600\nline 601\n[lines 600-601 of 1000]" {
		t.Errorf("seek: %q", got)
	}
	// More than one call returns: stopped, with where to go on.
	got = mustOK(t, call(t, s, "read_lines", map[string]any{"doc": "app/long.md"}))
	if len(got) > 4096 || !strings.Contains(got, "(stopped at the 4.0 KiB one call returns: go on with offset ") {
		t.Errorf("limit: %d bytes, %q", len(got), got[len(got)-200:])
	}
}

// A changed document is seen on the next call, and line numbers from
// before it are refused.
func TestVersions(t *testing.T) {
	s, dirs := newServer(t, 32<<10, "app")
	p := filepath.Join(dirs["app"], "guide.md")
	write(t, p, guide)
	r := call(t, s, "outline", map[string]any{"doc": "app/guide.md"})
	v := r.Structured["version"].(string)
	mustOK(t, call(t, s, "read_lines", map[string]any{"doc": "app/guide.md", "offset": 1, "limit": 1, "version": v}))

	write(t, p, "# New\n"+guide)
	_ = os.Chtimes(p, time.Now().Add(time.Minute), time.Now().Add(time.Minute))
	mustFail(t, call(t, s, "read_lines", map[string]any{"doc": "app/guide.md", "offset": 1, "version": v}), "changed since version")
	got := mustOK(t, call(t, s, "read_section", map[string]any{"doc": "app/guide.md#configure"}))
	if !strings.Contains(got, "lines 8-12 of 14") {
		t.Errorf("after the change: %s", got)
	}
}

func TestSearch(t *testing.T) {
	s, dirs := newServer(t, 32<<10, "app", "other")
	write(t, filepath.Join(dirs["app"], "guide.md"), guide)
	write(t, filepath.Join(dirs["other"], "x.md"), "before\ntimeout here\n# H\n")
	r := call(t, s, "search", map[string]any{"query": "TIMEOUT", "collection": "app"})
	got := mustOK(t, r)
	e, _ := s.Cat.Load(context.Background(), catalog.Doc{Coll: s.Cat.Collections[0], Rel: "guide.md"})
	want := "app/guide.md (version " + e.Version + ")\n" +
		"  § Configure  #configure  [5 lines, 71 B]\n" +
		"  7- ## Configure\n" +
		"  8: Set `timeout` in the file.\n" +
		"  § Configure > Timeouts  #timeouts  [3 lines, 31 B]\n" +
		"  9: ### Timeouts\n" +
		"  10: timeout: 30s\n" +
		"  11- more\n" +
		"(read a match's section: read_section with doc and its id; or the lines: read_lines)"
	if got != want {
		t.Errorf("search:\n%s\nwant:\n%s", got, want)
	}
	ms := r.Structured["matches"].([]any)
	if m := ms[0].(map[string]any); len(ms) != 3 || m["section"] != "configure" || m["line"].(float64) != 8 ||
		m["sectionLine"].(float64) != 7 || m["sectionBytes"].(float64) != 71 || m["version"] != e.Version {
		t.Errorf("structured: %v", ms)
	}

	got = mustOK(t, call(t, s, "search", map[string]any{"query": "timeout"}))
	if !strings.Contains(got, "other/x.md") || !strings.Contains(got, "§ (before the first heading)") ||
		strings.Contains(got, "3- # H") {
		t.Errorf("all collections:\n%s", got)
	}
	got = mustOK(t, call(t, s, "search", map[string]any{"query": "time(out)?:", "regexp": true, "doc": "app/guide.md", "context": 0}))
	if !strings.Contains(got, "10: timeout: 30s") || strings.Contains(got, "11-") {
		t.Errorf("regexp:\n%s", got)
	}
	r = call(t, s, "search", map[string]any{"query": "timeout", "maxResults": 1})
	if got := mustOK(t, r); !r.Structured["truncated"].(bool) || !strings.Contains(got, "stopped after 1 matching lines") ||
		strings.Contains(got, "other/") {
		t.Errorf("truncated:\n%s", got)
	}
	if got := mustOK(t, call(t, s, "search", map[string]any{"query": "nothing like it"})); got != "no matches" {
		t.Errorf("no matches: %q", got)
	}
	mustFail(t, call(t, s, "search", map[string]any{"query": "(", "regexp": true}), "not a valid regular expression")
	mustFail(t, call(t, s, "search", map[string]any{"query": ""}), "query")
	mustFail(t, call(t, s, "search", map[string]any{"query": "x", "context": 11}), "context")
}

// Nothing outside the collections: not by .., not by an absolute path,
// not through a symbolic link; binary files are not documents.
func TestConfinement(t *testing.T) {
	s, dirs := newServer(t, 32<<10, "app")
	outside := t.TempDir()
	write(t, filepath.Join(outside, "secret.md"), "# secret\n")
	if err := os.Symlink(filepath.Join(outside, "secret.md"), filepath.Join(dirs["app"], "link.md")); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(dirs["app"], "bin.md"), "# x\x00\x01")
	mustFail(t, call(t, s, "outline", map[string]any{"doc": "app/link.md"}), "outside the documentation")
	mustFail(t, call(t, s, "read_lines", map[string]any{"doc": "../" + filepath.Base(outside) + "/secret.md"}), "outside")
	mustFail(t, call(t, s, "read_lines", map[string]any{"doc": filepath.Join(outside, "secret.md")}), "outside the documentation")
	mustFail(t, call(t, s, "outline", map[string]any{"doc": "app/bin.md"}), "not a text file")
	if got := mustOK(t, call(t, s, "search", map[string]any{"query": "secret"})); got != "no matches" {
		t.Errorf("search followed a link out: %s", got)
	}
}

// mcp-gateway's outline_file test, through --fs-compat: same output.
func TestCompatOutlineFile(t *testing.T) {
	s, dirs := newServer(t, 32<<10, "app")
	root := dirs["app"]
	doc := "# Title\nintro\n## One ##\n```sh\n# not a heading\n~~~\n```\n#tag is not one\n   ### Deep\ntext\n" +
		"## Two\nSetext\n------\n    # indented code\n~~~~\n## in tildes\n~~~~\nend"
	write(t, filepath.Join(root, "doc.md"), doc)
	got := mustOK(t, call(t, s, "outline_file", map[string]any{"path": "doc.md"}))
	want := filepath.Join(root, "doc.md") + ": 18 lines, 154 B\n" +
		" 1  # Title  [18 lines, 154 B]\n" +
		" 3  ## One  [8 lines, 73 B]\n" +
		" 9  ### Deep  [2 lines, 17 B]\n" +
		"11  ## Two  [8 lines, 67 B]\n" +
		"(read a section: read_text_file with offset = its line, limit = its lines)"
	if got != want {
		t.Errorf("outline_file:\n%s\nwant:\n%s", got, want)
	}
	sec := mustOK(t, call(t, s, "read_text_file", map[string]any{"path": "doc.md", "offset": 11, "limit": 8}))
	if !strings.HasPrefix(sec, "## Two\n") || !strings.HasSuffix(sec, "end\n[lines 11-18 of 18]") {
		t.Errorf("section read: %q", sec)
	}
}

func TestCompatSearch(t *testing.T) {
	s, dirs := newServer(t, 32<<10, "app")
	root := dirs["app"]
	write(t, filepath.Join(root, "guide.md"), guide)
	write(t, filepath.Join(root, "skip", "g.md"), "timeout\n")
	r := call(t, s, "search_text", map[string]any{"path": ".", "query": "timeout: 30", "excludePatterns": []string{"skip"}})
	got := mustOK(t, r)
	if got != filepath.Join(root, "guide.md")+"\n8- Set `timeout` in the file.\n9- ### Timeouts\n10: timeout: 30s\n11- more\n12- ## Install" {
		t.Errorf("search_text:\n%s", got)
	}
	m := r.Structured["matches"].([]any)[0].(map[string]any)
	if m["path"] != filepath.Join(root, "guide.md") || m["line"].(float64) != 10 {
		t.Errorf("structured: %v", m)
	}
	// The path of a match is what read_text_file takes.
	if got := mustOK(t, call(t, s, "read_text_file", map[string]any{"path": m["path"], "offset": 10, "limit": 1})); got != "timeout: 30s\n[lines 10-10 of 13]" {
		t.Errorf("read_text_file: %q", got)
	}
	if got := mustOK(t, call(t, s, "search_files", map[string]any{"path": ".", "pattern": "*.md"})); got != filepath.Join(root, "guide.md")+"\n"+filepath.Join(root, "skip", "g.md") {
		t.Errorf("search_files:\n%s", got)
	}
	if got := mustOK(t, call(t, s, "list_directory", map[string]any{"path": root})); got != "[FILE] guide.md\n[DIR] skip" {
		t.Errorf("list_directory:\n%s", got)
	}
}

// The instructions name only tools the server offers, with and without
// --fs-compat.
func TestInstructionsNameOfferedTools(t *testing.T) {
	for _, compat := range []bool{false, true} {
		s, dirs := newServer(t, 32<<10, "app")
		s.FSCompat = compat
		write(t, filepath.Join(dirs["app"], "README.md"), "# x\n")
		ins := s.Instructions()
		offered := map[string]bool{}
		for _, tl := range s.tools() {
			offered[tl.name] = true
		}
		for _, n := range allToolNames() {
			if regexp.MustCompile(`\b`+n+`\b`).MatchString(ins) && !offered[n] {
				t.Errorf("compat %v: the instructions name %s, which is not offered:\n%s", compat, n, ins)
			}
		}
		for _, n := range []string{"search", "outline", "read_section", "read_lines", "list_docs", "app/README.md"} {
			if !strings.Contains(ins, n) {
				t.Errorf("the instructions do not mention %s:\n%s", n, ins)
			}
		}
	}
}

func TestToolList(t *testing.T) {
	s, _ := newServer(t, 32<<10, "app")
	s.FSCompat = false
	res, err := s.Handle(context.Background(), "tools/list", nil)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, tl := range res.(map[string]any)["tools"].([]map[string]any) {
		names = append(names, tl["name"].(string))
		if a := tl["annotations"].(map[string]any); a["readOnlyHint"] != true || a["openWorldHint"] != false {
			t.Errorf("%s: annotations %v", tl["name"], a)
		}
	}
	if strings.Join(names, ",") != "list_docs,outline,search,read_section,read_lines" {
		t.Errorf("tools: %v", names)
	}
}

// What an answer costs, as mcp-gateway's end-to-end tests measure it:
// the outline, a section, and a search each a small part of a large
// document.
func TestCost(t *testing.T) {
	s, dirs := newServer(t, 32<<10, "app")
	var b strings.Builder
	b.WriteString("# Architecture\n")
	for i := 1; i <= 12; i++ {
		fmt.Fprintf(&b, "## %d. Chapter\n", i)
		for j := 1; j <= 6; j++ {
			fmt.Fprintf(&b, "### %d.%d Topic %d-%d\n", i, j, i, j)
			for k := 0; k < 30; k++ {
				fmt.Fprintf(&b, "Text of %d.%d, line %d, about this and that and the other.\n", i, j, k)
			}
		}
	}
	b.WriteString("The setting agents.no_request_timeout is here.\n")
	doc := b.String()
	write(t, filepath.Join(dirs["app"], "architecture.md"), doc)

	o := mustOK(t, call(t, s, "outline", map[string]any{"doc": "app/architecture.md", "maxLevel": 2}))
	sec := mustOK(t, call(t, s, "read_section", map[string]any{"doc": "app/architecture.md#76-topic-7-6"}))
	sr := mustOK(t, call(t, s, "search", map[string]any{"query": "no_request_timeout", "maxResults": 5}))
	for name, got := range map[string]string{"outline": o, "section": sec, "search": sr} {
		if len(got) > len(doc)/20 {
			t.Errorf("%s: %d bytes of a %d-byte document", name, len(got), len(doc))
		}
	}
	if !strings.Contains(sec, "Text of 7.6, line 29") || !strings.Contains(sr, "#126-topic-12-6") {
		t.Errorf("section or search missed:\n%s\n%s", sec, sr)
	}
}
