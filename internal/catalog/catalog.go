// Package catalog holds the collections of documents mcp-docs serves:
// where they are, how an address names a document, and each document's
// outline, cached by the file's identity so that a changed file is seen
// on the next call (docs/architecture.md, §4 and §7).
//
// Every file operation goes through the collection's os.Root, so a
// symbolic link that leads out of a collection is refused by the kernel,
// not only by a check of the path as a string. The exception are a
// collection's also documents, opened by the absolute paths its
// configuration gives; an agent names them only by their base names.
package catalog

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"syscall"
	"unicode/utf8"

	"github.com/sdrahn/mcp-docs/internal/glob"
	"github.com/sdrahn/mcp-docs/internal/markdown"
)

// Collection is a named directory of documents.
type Collection struct {
	Name        string
	Title       string
	Path        string // absolute, cleaned
	Description string
	// Instructions are added to the server's instructions.
	Instructions string
	// IndexName is the index document, relative to Path; "": the first
	// of IndexNames there is.
	IndexName string
	// Include and Exclude are glob patterns of paths relative to Path
	// (internal/glob): a document is one that Include matches (empty:
	// all) and Exclude does not; an excluded directory is not entered.
	Include, Exclude []string
	// Also are single documents elsewhere (absolute paths), named by
	// their base names in the collection.
	Also []string

	once sync.Once
	root *os.Root
	err  error
}

// open opens the directory on first use, not at start: a server that
// only lists its tools may run where it cannot read its collections.
func (c *Collection) open() (*os.Root, error) {
	c.once.Do(func() { c.root, c.err = os.OpenRoot(c.Path) })
	return c.root, c.err
}

// IndexNames are the documents taken as a collection's index, in this
// order.
var IndexNames = []string{"README.md", "index.md", "README.markdown", "index.markdown"}

// Doc is a document, or a directory, of a collection.
type Doc struct {
	Coll *Collection
	Rel  string // slash-separated, relative to the collection; "." for its root
}

// Address is the document's name for agents: collection/path.
func (d Doc) Address() string {
	if d.Rel == "." {
		return d.Coll.Name
	}
	return d.Coll.Name + "/" + d.Rel
}

// Full is the document's absolute path.
func (d Doc) Full() string {
	if a := d.Coll.also(d.Rel); a != "" {
		return a
	}
	return filepath.Join(d.Coll.Path, filepath.FromSlash(d.Rel))
}

// also returns the absolute path of the also document named rel, or "".
func (k *Collection) also(rel string) string {
	for _, a := range k.Also {
		if filepath.Base(a) == rel {
			return a
		}
	}
	return ""
}

// member reports whether rel, below the collection's directory, is
// part of the collection by its patterns.
func (k *Collection) member(rel string, dir bool) bool {
	if rel == "." {
		return true
	}
	// An excluded directory excludes what is below it.
	parts := strings.Split(rel, "/")
	for i := range parts {
		prefix := strings.Join(parts[:i+1], "/")
		for _, p := range k.Exclude {
			if glob.Match(p, prefix) {
				return false
			}
		}
	}
	if dir || len(k.Include) == 0 {
		return true
	}
	for _, p := range k.Include {
		if glob.Match(p, rel) {
			return true
		}
	}
	return false
}

// Catalog is the collections and the cache of their documents' outlines.
type Catalog struct {
	Collections []*Collection
	// Exts are the extensions of documents (".md").
	Exts []string
	// MaxEntries bounds a listing.
	MaxEntries int

	mu    sync.Mutex
	cache map[string]*Entry
}

// New returns a catalog of the collections.
func New(colls []*Collection, exts []string, maxEntries int) *Catalog {
	return &Catalog{Collections: colls, Exts: exts, MaxEntries: maxEntries, cache: map[string]*Entry{}}
}

// Collection returns the collection of that name, or nil.
func (c *Catalog) Collection(name string) *Collection {
	for _, k := range c.Collections {
		if k.Name == name {
			return k
		}
	}
	return nil
}

// IsDoc reports whether a name has a document's extension.
func (c *Catalog) IsDoc(name string) bool {
	return slices.Contains(c.Exts, strings.ToLower(path.Ext(name)))
}

// Index returns the collection's index document, if it has one.
func (c *Catalog) Index(k *Collection) (Doc, bool) {
	names := IndexNames
	if k.IndexName != "" {
		names = []string{filepath.ToSlash(filepath.Clean(k.IndexName))}
	}
	for _, n := range names {
		d := Doc{Coll: k, Rel: n}
		if fi, err := c.Stat(d); err == nil && fi.Mode().IsRegular() {
			return d, true
		}
	}
	return Doc{}, false
}

// Resolve maps an address to a document (or directory) and the section
// after "#", if any:
//
//   - an absolute path below a collection's directory;
//   - collection/path, or the collection's name alone;
//   - a path relative to the first collection.
//
// With filePaths (mcp-server-fs's tools), a relative path is only ever
// relative to the first collection, as that server's are to its first
// directory. Nothing is checked to exist but the collection.
func (c *Catalog) Resolve(addr string, filePaths bool) (Doc, string, error) {
	section := ""
	if i := strings.LastIndexByte(addr, '#'); i >= 0 && !filePaths {
		addr, section = addr[:i], addr[i+1:]
	}
	if addr == "" {
		return Doc{}, "", errors.New("doc: give a document (list_docs lists them)")
	}
	if filepath.IsAbs(addr) {
		p := filepath.Clean(addr)
		for _, k := range c.Collections {
			for _, a := range k.Also {
				if a == p {
					return Doc{Coll: k, Rel: filepath.Base(a)}, section, nil
				}
			}
		}
		var best *Collection
		for _, k := range c.Collections {
			if (p == k.Path || strings.HasPrefix(p, k.Path+"/") || k.Path == "/") && (best == nil || len(k.Path) > len(best.Path)) {
				best = k
			}
		}
		if best == nil {
			return Doc{}, "", fmt.Errorf("%s: outside the documentation (%s)", p, strings.Join(c.paths(), ", "))
		}
		rel, err := filepath.Rel(best.Path, p)
		if err != nil {
			return Doc{}, "", err
		}
		return Doc{Coll: best, Rel: filepath.ToSlash(rel)}, section, nil
	}
	rel := path.Clean(strings.TrimPrefix(filepath.ToSlash(addr), "./"))
	if rel == ".." || strings.HasPrefix(rel, "../") {
		return Doc{}, "", fmt.Errorf("%s: outside the documentation", addr)
	}
	if !filePaths {
		first, rest, _ := strings.Cut(rel, "/")
		if k := c.Collection(first); k != nil {
			if rest == "" {
				rest = "."
			}
			d := Doc{Coll: k, Rel: rest}
			// A path of the first collection that happens to start with
			// a collection's name is still found.
			if k != c.Collections[0] || c.exists(d) || !c.exists(Doc{Coll: c.Collections[0], Rel: rel}) {
				return d, section, nil
			}
		}
	}
	return Doc{Coll: c.Collections[0], Rel: rel}, section, nil
}

func (c *Catalog) exists(d Doc) bool {
	_, err := c.Stat(d)
	return err == nil
}

func (c *Catalog) paths() []string {
	var out []string
	for _, k := range c.Collections {
		out = append(out, k.Path)
	}
	return out
}

// Stat returns what the document or directory is; what the collection's
// patterns leave out is not there.
func (c *Catalog) Stat(d Doc) (fs.FileInfo, error) {
	if a := d.Coll.also(d.Rel); a != "" {
		fi, err := os.Stat(a)
		if err != nil {
			return nil, Explain(d, err)
		}
		return fi, nil
	}
	root, err := d.Coll.open()
	if err != nil {
		return nil, Explain(d, err)
	}
	fi, err := root.Stat(d.Rel)
	if err != nil {
		return nil, Explain(d, err)
	}
	if !d.Coll.member(d.Rel, fi.IsDir()) {
		return nil, notMember(d)
	}
	return fi, nil
}

func notMember(d Doc) error {
	return fmt.Errorf("%s: not part of the collection %s", d.Address(), d.Coll.Name)
}

// open opens a document: an also document by its path (fixed by the
// configuration), any other through the collection's os.Root.
func (c *Catalog) open(d Doc) (*os.File, error) {
	if a := d.Coll.also(d.Rel); a != "" {
		f, err := os.Open(a)
		if err != nil {
			return nil, Explain(d, err)
		}
		return f, nil
	}
	root, err := d.Coll.open()
	if err != nil {
		return nil, Explain(d, err)
	}
	f, err := root.Open(d.Rel)
	if err != nil {
		return nil, Explain(d, err)
	}
	if fi, err := f.Stat(); err == nil && !d.Coll.member(d.Rel, fi.IsDir()) {
		_ = f.Close()
		return nil, notMember(d)
	}
	return f, nil
}

// Entry is what is known of a document's version.
type Entry struct {
	Outline *markdown.Outline
	// Version names the file's identity and contents: device, inode,
	// size and modification time.
	Version string
	Size    int64
}

// Load returns the document's outline, scanning it unless the cached
// one is of the same version.
func (c *Catalog) Load(ctx context.Context, d Doc) (*Entry, error) {
	f, err := c.open(d)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	fi, err := f.Stat()
	if err != nil {
		return nil, Explain(d, err)
	}
	if fi.IsDir() {
		return nil, fmt.Errorf("%s: is a directory (list_docs lists its documents)", d.Address())
	}
	if !fi.Mode().IsRegular() {
		return nil, fmt.Errorf("%s: not a regular file", d.Address())
	}
	version := versionOf(fi)
	key := d.Coll.Name + "\x00" + d.Rel
	c.mu.Lock()
	e := c.cache[key]
	c.mu.Unlock()
	if e != nil && e.Version == version {
		return e, nil
	}
	br := bufio.NewReader(f)
	if sample, _ := br.Peek(8192); IsBinary(sample) {
		return nil, fmt.Errorf("%s: not a text file", d.Address())
	}
	o, err := markdown.Scan(ctx, br)
	if err != nil {
		return nil, Explain(d, err)
	}
	e = &Entry{Outline: o, Version: version, Size: fi.Size()}
	c.mu.Lock()
	c.cache[key] = e
	c.mu.Unlock()
	return e, nil
}

func versionOf(fi fs.FileInfo) string {
	h := fnv.New64a()
	if st, ok := fi.Sys().(*syscall.Stat_t); ok {
		fmt.Fprintf(h, "%d:%d:", st.Dev, st.Ino)
	}
	fmt.Fprintf(h, "%d:%d", fi.Size(), fi.ModTime().UnixNano())
	return fmt.Sprintf("%016x", h.Sum64())[:12]
}

// Lines returns lines from to to (from 1, inclusive, each with its line
// end) of a document whose outline is e, seeking to the nearest stored
// offset. It fails when the document is no longer of e's version.
func (c *Catalog) Lines(d Doc, e *Entry, from, to int) ([]string, error) {
	if from < 1 || to < from {
		return nil, nil
	}
	f, err := c.open(d)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	if fi, err := f.Stat(); err != nil {
		return nil, Explain(d, err)
	} else if versionOf(fi) != e.Version {
		return nil, fmt.Errorf("%s: changed while it was read: try again", d.Address())
	}
	k := (from - 1) / markdown.OffsetEvery
	if k >= len(e.Outline.Offsets) {
		return nil, nil
	}
	if _, err := f.Seek(e.Outline.Offsets[k], io.SeekStart); err != nil {
		return nil, Explain(d, err)
	}
	br := bufio.NewReader(f)
	var out []string
	for n := k*markdown.OffsetEvery + 1; n <= to; n++ {
		line, err := br.ReadString('\n')
		if n >= from && len(line) > 0 {
			out = append(out, line)
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, Explain(d, err)
		}
	}
	return out, nil
}

// ReadAll reads a whole document of at most max bytes; a larger one is
// errTooLarge.
func (c *Catalog) ReadAll(d Doc, max int64) ([]byte, error) {
	f, err := c.open(d)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	b, err := io.ReadAll(io.LimitReader(f, max+1))
	if err != nil {
		return nil, Explain(d, err)
	}
	if int64(len(b)) > max {
		return nil, ErrTooLarge
	}
	return b, nil
}

// ErrTooLarge is a file larger than a read may be.
var ErrTooLarge = errors.New("too large")

// Walk calls fn for the documents and directories below d (a directory
// or a document), in lexical order, skipping hidden directories and
// btrfs .snapshots, what the collection's patterns and skip (if set)
// leave out, not following symbolic links; then, from the collection's
// directory, its also documents. fn returns false to stop.
func (c *Catalog) Walk(ctx context.Context, d Doc, skip func(Doc) bool, fn func(Doc, fs.DirEntry) bool) error {
	if a := d.Coll.also(d.Rel); a != "" {
		fi, err := os.Stat(a)
		if err != nil {
			return Explain(d, err)
		}
		fn(d, fs.FileInfoToDirEntry(fi))
		return nil
	}
	root, err := d.Coll.open()
	if err != nil {
		return Explain(d, err)
	}
	stopped := false
	err = fs.WalkDir(root.FS(), d.Rel, func(p string, e fs.DirEntry, err error) error {
		if err != nil {
			if p == d.Rel {
				return err
			}
			return nil // unreadable below: skipped
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if p != d.Rel && e.IsDir() && (strings.HasPrefix(e.Name(), ".") || e.Name() == ".snapshots") {
			return fs.SkipDir
		}
		if p == d.Rel && e.IsDir() {
			return nil
		}
		if !e.IsDir() && !(e.Type().IsRegular() && c.IsDoc(e.Name())) {
			return nil
		}
		if !d.Coll.member(p, e.IsDir()) || (skip != nil && skip(Doc{Coll: d.Coll, Rel: p})) {
			if e.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if !fn(Doc{Coll: d.Coll, Rel: p}, e) {
			stopped = true
			return fs.SkipAll
		}
		return nil
	})
	if err != nil {
		return Explain(d, err)
	}
	if d.Rel != "." || stopped {
		return nil
	}
	for _, a := range d.Coll.Also {
		ad := Doc{Coll: d.Coll, Rel: filepath.Base(a)}
		if skip != nil && skip(ad) {
			continue
		}
		fi, err := os.Stat(a)
		if err != nil || !fi.Mode().IsRegular() {
			continue
		}
		if !fn(ad, fs.FileInfoToDirEntry(fi)) {
			return nil
		}
	}
	return nil
}

// Explain turns an error from a file operation into a message that
// names the document and says what went wrong.
func Explain(d Doc, err error) error {
	var pe *fs.PathError
	if errors.As(err, &pe) {
		err = pe.Err
	}
	switch {
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return err
	case errors.Is(err, fs.ErrNotExist):
		return fmt.Errorf("%s: no such document (list_docs lists them)", d.Address())
	case errors.Is(err, fs.ErrPermission):
		return fmt.Errorf("%s: permission denied", d.Address())
	case strings.Contains(err.Error(), "escapes from parent"), strings.Contains(err.Error(), "path escapes"):
		return fmt.Errorf("%s: leads outside the documentation (through a symbolic link)", d.Address())
	case errors.Is(err, syscall.ENOTDIR):
		return fmt.Errorf("%s: a component is not a directory", d.Address())
	}
	return fmt.Errorf("%s: %v", d.Address(), err)
}

// IsBinary reports whether a sample of a file is not text: a NUL byte,
// or not UTF-8.
func IsBinary(b []byte) bool {
	sample := b
	if len(sample) > 8192 {
		sample = sample[:8192]
	}
	if strings.IndexByte(string(sample), 0) >= 0 {
		return true
	}
	return !utf8.Valid(trimPartialRune(sample))
}

// trimPartialRune drops an incomplete UTF-8 sequence at the end of a
// sample cut from a longer text (mcp-gateway's).
func trimPartialRune(b []byte) []byte {
	for i := 1; i <= 3 && i <= len(b); i++ {
		if r, _ := utf8.DecodeLastRune(b[:len(b)-i+1]); r != utf8.RuneError {
			return b[:len(b)-i+1]
		}
	}
	return b
}
