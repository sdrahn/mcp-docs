// Command mcp-docs is an MCP server that gives agents the documentation
// on a system a part at a time (docs/architecture.md): the documents of
// its collections, their outlines, search that answers with sections,
// and reads of one section or a range of lines. It speaks MCP on
// stdin/stdout, or with --listen over Streamable HTTP (stateless:
// POST /mcp), only reads, and restricts itself with Landlock to reading
// its collections and, over HTTP, to binding its port.
//
//	mcp-docs                                  # the collections of /usr/share/mcp-docs/collections.d and /etc/mcp-docs/collections.d
//	mcp-docs --root /usr/share/mcp-gateway/docs
//	mcp-docs --collection 'gateway=/usr/share/mcp-gateway/docs:The MCP gateway' --collection kit=/usr/share/doc/kit
//	mcp-docs --listen :8080                     # http://host:8080/mcp, health at /healthz
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/sdrahn/mcp-docs/internal/catalog"
	"github.com/sdrahn/mcp-docs/internal/config"
	"github.com/sdrahn/mcp-docs/internal/landlock"
	"github.com/sdrahn/mcp-docs/internal/mcphttp"
	"github.com/sdrahn/mcp-docs/internal/mcpserver"
	"github.com/sdrahn/mcp-docs/internal/server"
)

// version is set at build time (-ldflags "-X main.version=…").
var version = "0.1.0-dev"

type listFlag []string

func (l *listFlag) String() string     { return strings.Join(*l, ",") }
func (l *listFlag) Set(v string) error { *l = append(*l, v); return nil }

func main() {
	var roots, colls, origins listFlag
	flag.Var(&roots, "root", "a directory of documents, a collection named after it (repeatable)")
	flag.Var(&colls, "collection", "a collection: name=/path[:description] (repeatable)")
	about := flag.String("instructions", "", "what the documents are, for the client's model (opens the server's instructions)")
	maxRead := flag.Int64("max-read", 16<<10, "bytes one call returns at most")
	maxFile := flag.Int64("max-file", 16<<20, "bytes of the largest document search reads")
	maxEntries := flag.Int("max-entries", 10000, "documents a listing or search covers at most")
	exts := flag.String("ext", ".md,.markdown", "extensions of documents, comma-separated")
	configDirs := flag.String("config-dirs", "", "directories of collection files, comma-separated, later ones "+
		"replacing earlier ones (default without --root or --collection: "+strings.Join(config.DefaultDirs, ",")+")")
	listItems := flag.Bool("list-items", false, "in the collections of --root and --collection, take list items "+
		"and paragraphs that open with a bold title as sections")
	fsCompat := flag.Bool("fs-compat", false, "also offer mcp-server-fs's reading tools (outline_file, search_text, read_text_file, search_files, list_directory)")
	structured := flag.Bool("structured", false, "also return results as structuredContent, with output schemas (for clients that read the data; a model needs only the text)")
	listen := flag.String("listen", "", "serve MCP over Streamable HTTP on this address (\":8080\", \"127.0.0.1:8080\") "+
		"instead of stdin/stdout: POST /mcp, health at /healthz")
	flag.Var(&origins, "allow-origin", "with --listen, a browser origin (\"https://app.example\") requests may come from "+
		"(repeatable); requests without an Origin header are always served")
	maxConcurrent := flag.Int("max-concurrent", 32, "with --listen, requests handled at once")
	showVersion := flag.Bool("version", false, "print the version and exit")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "usage: %s [options]\n\nAn MCP server for the documentation on this system, on stdin/stdout or HTTP (--listen).\n\n", server.Name)
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
	port := -1 // stdio: no TCP port
	if *listen != "" {
		var err error
		if port, err = listenPort(*listen); err != nil {
			fail(2, "--listen: "+err.Error())
		}
	} else if len(origins) > 0 {
		fail(2, "--allow-origin needs --listen")
	}
	if *maxRead < 4096 || *maxFile <= 0 || *maxEntries <= 0 || *maxConcurrent <= 0 {
		fail(2, "the limits must be positive, --max-read at least 4096")
	}
	dirs := splitList(*configDirs)
	if *configDirs == "" && len(roots) == 0 && len(colls) == 0 {
		dirs = config.DefaultDirs
	}
	collections, problems, err := loadCollections(dirs, roots, colls, *listItems)
	if err != nil {
		fail(2, err.Error())
	}
	if len(collections) == 0 {
		// Exiting here, before Landlock executes the program again: the
		// reasons are printed once all the same.
		for _, p := range problems {
			fmt.Fprintf(os.Stderr, "%s: %v; left out\n", server.Name, p)
		}
		fail(2, "no collections: give --root or --collection, or install collection files in "+strings.Join(dirs, " or "))
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
	rules := landlockRules(collections, dirs, port)
	ll, err := landlock.Self(rules)
	if err != nil {
		fail(1, err.Error())
	}
	// Log lines on stderr, never on the MCP connection; after Self, which
	// executes the program again: once, not twice.
	for _, p := range problems {
		fmt.Fprintf(os.Stderr, "%s: %v; left out\n", server.Name, p)
	}
	var names []string
	for _, k := range collections {
		names = append(names, k.Name+"="+k.Path)
	}
	fmt.Fprintf(os.Stderr, "%s: serving %s; %s%s\n", server.Name, strings.Join(names, ", "), ll, tcpNote(ll, rules))

	s := &server.Server{Cat: catalog.New(collections, extList, *maxEntries), About: *about, MaxRead: *maxRead,
		MaxFile: *maxFile, FSCompat: *fsCompat, Structured: *structured, Version: version}
	if *listen == "" {
		if err := mcpserver.New(s.Handle, os.Stdout).Serve(os.Stdin, 1<<20); err != nil {
			fail(1, err.Error())
		}
		return
	}
	if err := serveHTTP(*listen, s.Handle, mcphttp.Options{AllowedOrigins: origins, MaxConcurrent: *maxConcurrent}); err != nil {
		fail(1, err.Error())
	}
}

// listenPort is the TCP port of a --listen address, for the Landlock
// rule that allows binding it; 0 for a port the kernel chooses (which no
// rule can name: binding is then not restricted).
func listenPort(addr string) (int, error) {
	_, p, err := net.SplitHostPort(addr)
	if err != nil {
		return 0, err
	}
	n, err := strconv.Atoi(p)
	if err != nil || n < 0 || n > 65535 {
		return 0, fmt.Errorf("port %q: give a number", p)
	}
	return n, nil
}

// serveHTTP serves MCP over HTTP until SIGTERM or SIGINT, then lets the
// requests in progress finish (a container's stop).
func serveHTTP(addr string, h mcpserver.Handler, o mcphttp.Options) error {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	srv := &http.Server{Handler: mcphttp.Handler(h, o), ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout: 30 * time.Second, WriteTimeout: 90 * time.Second, IdleTimeout: 2 * time.Minute,
		MaxHeaderBytes: 64 << 10}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()
	errc := make(chan error, 1)
	go func() { errc <- srv.Serve(ln) }()
	fmt.Fprintf(os.Stderr, "%s: listening on http://%s/mcp\n", server.Name, ln.Addr())
	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}
	fmt.Fprintf(os.Stderr, "%s: stopping\n", server.Name)
	shutdown, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdown); err != nil && !errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return nil
}

func fail(code int, msg string) {
	fmt.Fprintln(os.Stderr, server.Name+": "+msg)
	os.Exit(code)
}

// landlockRules are what the server may read: its collections, their
// also documents, and the directories of collection files. Self
// executes the program again under them, and the program reads the
// collection files again there: without their directories it would find
// none outside the trees Landlock always allows (/etc, /usr, /tmp).
//
// TCP: no connection to anywhere (the server needs none), and binding
// only the port of --listen (port > 0); none at all on stdin/stdout
// (port < 0). Kernels before Landlock ABI 4 leave TCP to the container
// or unit (reported, not fatal).
func landlockRules(collections []*catalog.Collection, configDirs []string, port int) landlock.Rules {
	var paths []string
	for _, k := range collections {
		paths = append(paths, k.Path)
		paths = append(paths, k.Also...)
	}
	for _, d := range configDirs {
		// A missing directory has nothing to read (and would be logged as
		// missing: /etc/mcp-docs/collections.d in most containers).
		if abs, err := filepath.Abs(d); err == nil {
			if fi, err := os.Stat(abs); err == nil && fi.IsDir() {
				paths = append(paths, abs)
			}
		}
	}
	r := landlock.Rules{Read: paths, TCPConnect: []int{}}
	switch {
	case port < 0:
		r.TCPBind = []int{}
	case port > 0:
		r.TCPBind = []int{port}
	}
	return r
}

// tcpNote says what Landlock does to TCP, where it does it (ABI 4 and
// later; before, the result names TCP as not supported).
func tcpNote(ll landlock.Result, r landlock.Rules) string {
	if ll.ABI < 4 {
		return ""
	}
	switch {
	case r.TCPBind == nil:
		return "; TCP: no connections"
	case len(r.TCPBind) == 0:
		return "; TCP: no connections, no listening"
	}
	return fmt.Sprintf("; TCP: no connections, listening on port %d only", r.TCPBind[0])
}

func splitList(s string) []string {
	var out []string
	for _, x := range strings.Split(s, ",") {
		if x = strings.TrimSpace(x); x != "" {
			out = append(out, x)
		}
	}
	return out
}

// loadCollections reads the collection files of dirs, then adds the
// collections of the command line, which replace files' of the same
// name. A collection file that cannot be used, or whose directory is
// missing, is a problem and left out; the command line's must be right.
func loadCollections(dirs, roots, colls []string, listItems bool) ([]*catalog.Collection, []error, error) {
	files, problems := config.Load(dirs)
	cli, err := parseCollections(roots, colls)
	if err != nil {
		return nil, problems, err
	}
	for _, c := range cli {
		c.ListItems = listItems
	}
	var out []*catalog.Collection
	for _, f := range files {
		if fi, err := os.Stat(f.Root); err != nil || !fi.IsDir() {
			problems = append(problems, fmt.Errorf("%s: root %s is not a directory", f.File, f.Root))
			continue
		}
		replaced := false
		for _, c := range cli {
			replaced = replaced || c.Name == f.Name
		}
		if replaced {
			continue
		}
		out = append(out, &catalog.Collection{Name: f.Name, Title: f.Title, Path: f.Root, Description: f.Description,
			Instructions: f.Instructions, IndexName: f.Index, Include: f.Include, Exclude: f.Exclude, Also: f.Also,
			ListItems: f.ListItems})
	}
	return append(out, cli...), problems, nil
}

// parseCollections makes the collections of --root and --collection.
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
