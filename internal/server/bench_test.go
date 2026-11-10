package server

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/sdrahn/mcp-docs/internal/catalog"
	"github.com/sdrahn/mcp-docs/internal/rank"
)

// The benchmarks measure the tools on real collections: MCP_DOCS_BENCH
// names a directory whose subdirectories are collections (build/, after
// tools/suse-docs/convert); without it they are skipped.
//
//	MCP_DOCS_BENCH=$PWD/build go test ./internal/server -run '^$' -bench . -benchmem
//
// MCP_DOCS_BENCH_QUERY replaces the question asked (one of SUSE's
// documentation), MCP_DOCS_BENCH_DOC the section read (doc#id; default:
// the first of the first collection's index).
func benchServer(b *testing.B) *Server {
	root := os.Getenv("MCP_DOCS_BENCH")
	if root == "" {
		b.Skip("MCP_DOCS_BENCH is not set")
	}
	dirs, err := os.ReadDir(root)
	if err != nil {
		b.Fatal(err)
	}
	var colls []*catalog.Collection
	for _, d := range dirs {
		p := filepath.Join(root, d.Name())
		if _, err := os.Stat(filepath.Join(p, d.Name()+".yaml")); err == nil && d.IsDir() {
			colls = append(colls, &catalog.Collection{Name: d.Name(), Path: p})
		}
	}
	if len(colls) == 0 {
		b.Skipf("%s: no collections (directories with their collection file)", root)
	}
	sort.Slice(colls, func(i, j int) bool { return colls[i].Name < colls[j].Name })
	return &Server{Cat: catalog.New(colls, []string{".md"}, 10000), MaxRead: 16 << 10, MaxFile: 16 << 20}
}

func benchQuery() string {
	if q := os.Getenv("MCP_DOCS_BENCH_QUERY"); q != "" {
		return q
	}
	return "make a SELinux domain permissive"
}

func benchCall(b *testing.B, s *Server, name string, args map[string]any) {
	b.Helper()
	raw, _ := json.Marshal(args)
	res, err := s.call(context.Background(), name, raw)
	if err != nil {
		b.Fatal(err)
	}
	if m := res.(map[string]any); m["isError"] == true {
		b.Fatalf("%s %s: %v", name, raw, m["content"])
	}
}

// BenchmarkIndexBuild: what the first find_sections of a server pays.
func BenchmarkIndexBuild(b *testing.B) {
	s := benchServer(b)
	for b.Loop() {
		for _, k := range s.Cat.Collections {
			if _, err := rank.Build(context.Background(), s.Cat, k, s.MaxFile); err != nil {
				b.Fatal(err)
			}
		}
	}
}

// BenchmarkIndexLoad: the same with index files (mcp-docs --write-index
// or the converter writes them): read and checked against the
// documents, not built.
func BenchmarkIndexLoad(b *testing.B) {
	s := benchServer(b)
	for b.Loop() {
		for _, k := range s.Cat.Collections {
			if _, err := rank.LoadFile(context.Background(), s.Cat, k); err != nil {
				b.Skipf("%s: %v", k.Name, err)
			}
		}
	}
}

// TestIndexFilesAnswerAsBuilt: on the collections of MCP_DOCS_BENCH, an
// index read from its file ranks as the one built.
func TestIndexFilesAnswerAsBuilt(t *testing.T) {
	root := os.Getenv("MCP_DOCS_BENCH")
	if root == "" {
		t.Skip("MCP_DOCS_BENCH is not set")
	}
	s := benchServer(&testing.B{})
	queries := []string{benchQuery(), "put a node into maintenance mode", "install with Agama", "revert a saptune solution",
		"configure the firewall", "snapper rollback", "timeout", "what is new"}
	for _, k := range s.Cat.Collections {
		built, err := rank.Build(context.Background(), s.Cat, k, s.MaxFile)
		if err != nil {
			t.Fatal(err)
		}
		read, err := rank.LoadFile(context.Background(), s.Cat, k)
		if err != nil {
			t.Fatalf("%s: %v", k.Name, err)
		}
		for _, q := range queries {
			x, _ := built.Search(q, 25)
			y, _ := read.Search(q, 25)
			if len(x) != len(y) {
				t.Fatalf("%s %q: %d and %d sections", k.Name, q, len(x), len(y))
			}
			for i := range x {
				if x[i].Doc != y[i].Doc || x[i].Heading != y[i].Heading || x[i].Score != y[i].Score || x[i].Sum != y[i].Sum {
					t.Fatalf("%s %q, %d: built %s %d %v, read %s %d %v", k.Name, q, i, x[i].Doc.Address(), x[i].Heading,
						x[i].Score, y[i].Doc.Address(), y[i].Heading, y[i].Score)
				}
			}
		}
	}
}

// BenchmarkFindSections: every later one, across the collections.
func BenchmarkFindSections(b *testing.B) {
	s := benchServer(b)
	args := map[string]any{"query": benchQuery()}
	benchCall(b, s, "find_sections", args)
	for b.Loop() {
		benchCall(b, s, "find_sections", args)
	}
}

// BenchmarkFindSectionsWatched: as a server that runs long does
// (--listen), told of changes where the others look for them.
func BenchmarkFindSectionsWatched(b *testing.B) {
	s := benchServer(b)
	if s.Watch = catalog.NewWatcher(); s.Watch == nil {
		b.Skip("no inotify here")
	}
	args := map[string]any{"query": benchQuery()}
	benchCall(b, s, "find_sections", args)
	for b.Loop() {
		benchCall(b, s, "find_sections", args)
	}
}

// BenchmarkFindSectionsOne: in the first collection alone.
func BenchmarkFindSectionsOne(b *testing.B) {
	s := benchServer(b)
	args := map[string]any{"query": benchQuery(), "collection": s.Cat.Collections[0].Name}
	benchCall(b, s, "find_sections", args)
	for b.Loop() {
		benchCall(b, s, "find_sections", args)
	}
}

// BenchmarkRank: the ranking itself, without the files.
func BenchmarkRank(b *testing.B) {
	s := benchServer(b)
	var parts []rank.Part
	for _, k := range s.Cat.Collections {
		ix, err := rank.Build(context.Background(), s.Cat, k, s.MaxFile)
		if err != nil {
			b.Fatal(err)
		}
		parts = append(parts, rank.Part{Index: ix, Under: catalog.Doc{Coll: k, Rel: "."}})
	}
	for b.Loop() {
		if _, err := rank.Search(benchQuery(), 3, parts); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkSearch: a text no document has, so every line of every
// document is matched.
func BenchmarkSearch(b *testing.B) {
	s := benchServer(b)
	for b.Loop() {
		benchCall(b, s, "search", map[string]any{"query": "zzzqqqnotthere"})
	}
}

func BenchmarkSearchRegexp(b *testing.B) {
	s := benchServer(b)
	for b.Loop() {
		benchCall(b, s, "search", map[string]any{"query": `zzz+qqq\s?notthere`, "regexp": true})
	}
}

// BenchmarkListDocs: one page of the first collection.
func BenchmarkListDocs(b *testing.B) {
	s := benchServer(b)
	for b.Loop() {
		benchCall(b, s, "list_docs", map[string]any{"collection": s.Cat.Collections[0].Name})
	}
}

func BenchmarkReadSection(b *testing.B) {
	s := benchServer(b)
	doc := os.Getenv("MCP_DOCS_BENCH_DOC")
	if doc == "" {
		// The first section of the first collection's index.
		d, ok := s.Cat.Index(s.Cat.Collections[0])
		if !ok {
			b.Skip("the first collection has no index: set MCP_DOCS_BENCH_DOC")
		}
		e, err := s.Cat.Load(context.Background(), d)
		if err != nil || len(e.Outline.Headings) == 0 {
			b.Skipf("%s has no headings: set MCP_DOCS_BENCH_DOC", d.Address())
		}
		doc = d.Address() + "#" + e.Outline.Headings[0].ID
	}
	for b.Loop() {
		benchCall(b, s, "read_section", map[string]any{"doc": doc})
	}
}

// BenchmarkReadLinesLarge: a document of 64 MiB read without a range,
// of which one call returns its first part.
func BenchmarkReadLinesLarge(b *testing.B) {
	dir := b.TempDir()
	f, err := os.Create(filepath.Join(dir, "large.md"))
	if err != nil {
		b.Fatal(err)
	}
	line := []byte("a line of text that is about sixty bytes long, more or less.\n")
	_, _ = f.WriteString("# Large\n\n")
	for range 1 << 20 {
		_, _ = f.Write(line)
	}
	if err := f.Close(); err != nil {
		b.Fatal(err)
	}
	s := &Server{Cat: catalog.New([]*catalog.Collection{{Name: "d", Path: dir}}, []string{".md"}, 10000),
		MaxRead: 16 << 10, MaxFile: 16 << 20}
	benchCall(b, s, "outline", map[string]any{"doc": "d/large.md"})
	for b.Loop() {
		benchCall(b, s, "read_lines", map[string]any{"doc": "d/large.md"})
	}
}
