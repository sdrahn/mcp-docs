// Command mcp-docs is an MCP server that gives agents the documentation
// on a system a part at a time (docs/architecture.md): the documents of
// its collections, their outlines, search that answers with sections,
// and reads of one section or a range of lines. It speaks MCP on
// stdin/stdout, only reads, and restricts itself with Landlock to
// reading its collections.
//
//	mcp-docs --root /usr/share/mcp-gateway/docs
//	mcp-docs --collection 'gateway=/usr/share/mcp-gateway/docs:The MCP gateway' --collection kit=/usr/share/doc/kit
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/sdrahn/mcp-docs/internal/catalog"
	"github.com/sdrahn/mcp-docs/internal/landlock"
	"github.com/sdrahn/mcp-docs/internal/mcpserver"
	"github.com/sdrahn/mcp-docs/internal/server"
)

// version is set at build time (-ldflags "-X main.version=…").
var version = "0.1.0-dev"

type listFlag []string

func (l *listFlag) String() string     { return strings.Join(*l, ",") }
func (l *listFlag) Set(v string) error { *l = append(*l, v); return nil }

func main() {
	var roots, colls listFlag
	flag.Var(&roots, "root", "a directory of documents, a collection named after it (repeatable)")
	flag.Var(&colls, "collection", "a collection: name=/path[:description] (repeatable)")
	about := flag.String("instructions", "", "what the documents are, for the client's model (opens the server's instructions)")
	maxRead := flag.Int64("max-read", 32<<10, "bytes one call returns at most")
	maxFile := flag.Int64("max-file", 16<<20, "bytes of the largest document search reads")
	maxEntries := flag.Int("max-entries", 10000, "documents a listing or search covers at most")
	exts := flag.String("ext", ".md,.markdown", "extensions of documents, comma-separated")
	fsCompat := flag.Bool("fs-compat", false, "also offer mcp-server-fs's reading tools (outline_file, search_text, read_text_file, search_files, list_directory)")
	showVersion := flag.Bool("version", false, "print the version and exit")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "usage: %s [options]\n\nAn MCP server for the documentation on this system, on stdin/stdout.\n\n", server.Name)
		flag.PrintDefaults()
	}
	flag.Parse()
	if *showVersion {
		fmt.Println(server.Name, version)
		return
	}
	if flag.NArg() > 0 {
		flag.Usage()
		os.Exit(2)
	}
	if *maxRead < 4096 || *maxFile <= 0 || *maxEntries <= 0 {
		fail(2, "the limits must be positive, --max-read at least 4096")
	}
	collections, err := parseCollections(roots, colls)
	if err != nil {
		fail(2, err.Error())
	}
	var extList []string
	for _, e := range strings.Split(*exts, ",") {
		if e = strings.ToLower(strings.TrimSpace(e)); e != "" {
			if !strings.HasPrefix(e, ".") {
				e = "." + e
			}
			extList = append(extList, e)
		}
	}

	// The kernel keeps the server to reading its collections (Landlock,
	// docs/architecture.md §8), whatever starts it.
	var paths []string
	for _, k := range collections {
		paths = append(paths, k.Path)
	}
	ll, err := landlock.Self(landlock.Rules{Read: paths})
	if err != nil {
		fail(1, err.Error())
	}
	// A log line on stderr, never on the MCP connection.
	var names []string
	for _, k := range collections {
		names = append(names, k.Name+"="+k.Path)
	}
	fmt.Fprintf(os.Stderr, "%s: serving %s; %s\n", server.Name, strings.Join(names, ", "), ll)

	s := &server.Server{Cat: catalog.New(collections, extList, *maxEntries), About: *about, MaxRead: *maxRead,
		MaxFile: *maxFile, FSCompat: *fsCompat, Version: version}
	if err := mcpserver.New(s.Handle, os.Stdout).Serve(os.Stdin, 1<<20); err != nil {
		fail(1, err.Error())
	}
}

func fail(code int, msg string) {
	fmt.Fprintln(os.Stderr, server.Name+": "+msg)
	os.Exit(code)
}

// parseCollections makes the collections of --root and --collection;
// without either, the current directory is one.
func parseCollections(roots, colls []string) ([]*catalog.Collection, error) {
	var out []*catalog.Collection
	taken := map[string]bool{}
	add := func(name, dir, desc string) error {
		if name == "" || strings.ContainsAny(name, "/#") {
			return fmt.Errorf("collection name %q: give a name without / or #", name)
		}
		if taken[name] {
			return fmt.Errorf("collection %q given twice", name)
		}
		abs, err := filepath.Abs(dir)
		if err != nil {
			return err
		}
		if fi, err := os.Stat(abs); err != nil || !fi.IsDir() {
			return fmt.Errorf("collection %q: %s is not a directory", name, abs)
		}
		taken[name] = true
		out = append(out, &catalog.Collection{Name: name, Path: abs, Description: desc})
		return nil
	}
	for _, c := range colls {
		name, rest, ok := strings.Cut(c, "=")
		if !ok {
			return nil, fmt.Errorf("--collection %q: give name=/path[:description]", c)
		}
		dir, desc, _ := strings.Cut(rest, ":")
		if err := add(name, dir, strings.TrimSpace(desc)); err != nil {
			return nil, err
		}
	}
	if len(roots) == 0 && len(colls) == 0 {
		roots = []string{"."}
	}
	for _, r := range roots {
		abs, err := filepath.Abs(r)
		if err != nil {
			return nil, err
		}
		name := filepath.Base(abs)
		if name == "/" || name == "." {
			name = "docs"
		}
		for i := 2; taken[name]; i++ {
			name = fmt.Sprintf("%s-%d", filepath.Base(abs), i)
		}
		if err := add(name, abs, ""); err != nil {
			return nil, err
		}
	}
	return out, nil
}
