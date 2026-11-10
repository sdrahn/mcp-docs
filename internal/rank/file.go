package rank

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"sync"

	"github.com/sdrahn/mcp-docs/internal/catalog"
)

// An index file holds a collection's index, so that a server reads it
// where it would read and tokenize every document (docs/architecture.md,
// §16.3). It lies in the collection's directory, written when the
// collection is made (a package's build), and is used when it is of the
// documents as they are: the same documents, each of the same size and
// checksum. Any other file is ignored, and the index built.
//
// The format, all numbers as unsigned varints unless said otherwise:
//
//	magic, the format's version in it
//	the version of Tokens; 1 if list items are sections, else 0
//	documents: count; each: path, size, CRC-32 (4 bytes)
//	words: count; each: the word; sorted
//	units: count; each: document, heading id ("" before the first
//	  heading), length, sum of its text (8 bytes), its title's words
//	  (count; each: word)
//	postings, for each word in order: count; each: unit (the
//	  difference to the one before), frequency
//	CRC-32 of all before (4 bytes)
//
// A string is its length and its bytes; fixed-size numbers are
// little-endian. A unit's document, a word and a unit are indexes into
// the lists before.

// FileName is the index file's name in a collection's directory.
const FileName = "mcp-docs.index"

const magic = "mcp-docs index 1\n"

// TokensVersion names what Tokens makes of a text. An index file of
// another version is not used: its words would not be the query's.
// TestTokensVersion fails when Tokens changes and this does not.
const TokensVersion = 1

// maxIndexFile bounds an index file read.
const maxIndexFile = 1 << 30

// ErrNoFile is a collection without an index file.
var ErrNoFile = errors.New("no index file")

type fileDoc struct {
	rel  string
	size int64
	crc  uint32
}

// WriteFile builds the collection's index and writes its index file
// into the collection's directory.
func WriteFile(ctx context.Context, cat *catalog.Catalog, k *catalog.Collection, maxFile int64) error {
	ix, err := Build(ctx, cat, k, maxFile)
	if err != nil {
		return err
	}
	docs, _, err := documents(ctx, cat, k, nil)
	if err != nil {
		return err
	}
	b, err := ix.encode(docs, k.ListItems)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(k.Path, FileName+".*")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	if _, err := tmp.Write(b); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o644); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), filepath.Join(k.Path, FileName))
}

// documents are the collection's documents as a walk finds them, each
// with its size and checksum, and their versions. With want, a document
// that want does not have with its size ends the walk early, with an
// error: the checksums of the others are not needed.
func documents(ctx context.Context, cat *catalog.Catalog, k *catalog.Collection, want map[string]fileDoc) ([]fileDoc, map[string]string, error) {
	var docs []fileDoc
	var ds []catalog.Doc
	versions := map[string]string{}
	var mismatch error
	err := cat.Walk(ctx, catalog.Doc{Coll: k, Rel: "."}, nil, func(d catalog.Doc, e fs.DirEntry) bool {
		if e.IsDir() {
			return true
		}
		fi, err := e.Info()
		if err != nil {
			mismatch = err
			return false
		}
		if want != nil {
			if w, ok := want[d.Rel]; !ok || w.size != fi.Size() {
				mismatch = fmt.Errorf("%s is not the document it was made of", d.Address())
				return false
			}
		}
		versions[d.Address()] = catalog.Version(fi)
		docs = append(docs, fileDoc{rel: d.Rel, size: fi.Size()})
		ds = append(ds, d)
		return true
	})
	if err == nil {
		err = mismatch
	}
	if err == nil {
		err = ctx.Err()
	}
	if err != nil {
		return nil, nil, err
	}
	if want != nil && len(docs) != len(want) {
		return nil, nil, errors.New("the collection has other documents than it was made of")
	}
	// The checksums: every document is read, several at a time.
	var wg sync.WaitGroup
	errs := make([]error, len(docs))
	next := make(chan int)
	for range min(runtime.GOMAXPROCS(0), 8) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			h := crc32.NewIEEE()
			buf := make([]byte, 64<<10)
			for i := range next {
				f, err := cat.Open(ds[i])
				if err != nil {
					errs[i] = err
					continue
				}
				h.Reset()
				for err == nil {
					var n int
					n, err = f.Read(buf)
					_, _ = h.Write(buf[:n])
				}
				_ = f.Close()
				if err == io.EOF {
					err = nil
				}
				docs[i].crc, errs[i] = h.Sum32(), err
			}
		}()
	}
	for i := range docs {
		next <- i
	}
	close(next)
	wg.Wait()
	for i, err := range errs {
		// A document that cannot be read is one as it was if it could
		// not be read then: checksum 0.
		if err != nil {
			docs[i].crc = 0
		}
	}
	return docs, versions, ctx.Err()
}

func (ix *Index) encode(docs []fileDoc, listItems bool) ([]byte, error) {
	var b bytes.Buffer
	num := func(n uint64) { b.Write(binary.AppendUvarint(nil, n)) }
	str := func(s string) { num(uint64(len(s))); b.WriteString(s) }
	b.WriteString(magic)
	num(TokensVersion)
	if listItems {
		num(1)
	} else {
		num(0)
	}
	docIndex := map[string]int{}
	num(uint64(len(docs)))
	for i, d := range docs {
		docIndex[d.rel] = i
		str(d.rel)
		num(uint64(d.size))
		b.Write(binary.LittleEndian.AppendUint32(nil, d.crc))
	}
	// The words: those with postings, and those of titles that are in
	// no text (a document's title from its front matter).
	seen := make(map[string]bool, len(ix.postings))
	words := make([]string, 0, len(ix.postings))
	for w := range ix.postings {
		seen[w] = true
		words = append(words, w)
	}
	for i := range ix.units {
		for _, t := range ix.units[i].title {
			if !seen[t] {
				seen[t] = true
				words = append(words, t)
			}
		}
	}
	sort.Strings(words)
	wordIndex := make(map[string]int, len(words))
	num(uint64(len(words)))
	for i, w := range words {
		wordIndex[w] = i
		str(w)
	}
	num(uint64(len(ix.units)))
	for i := range ix.units {
		u := &ix.units[i]
		di, ok := docIndex[u.doc.doc.Rel]
		if !ok {
			return nil, fmt.Errorf("%s changed while its index was written", u.doc.doc.Address())
		}
		num(uint64(di))
		str(u.id)
		num(uint64(u.length))
		b.Write(binary.LittleEndian.AppendUint64(nil, u.sum))
		var title []int
		for _, t := range u.title {
			if wi, ok := wordIndex[t]; ok {
				title = append(title, wi)
			}
		}
		num(uint64(len(title)))
		for _, wi := range title {
			num(uint64(wi))
		}
	}
	for _, w := range words {
		ps := ix.postings[w]
		num(uint64(len(ps)))
		last := 0
		for _, p := range ps {
			num(uint64(p.unit - last))
			num(uint64(p.tf))
			last = p.unit
		}
	}
	return appendCRC(b.Bytes()), nil
}

// LoadFile reads the collection's index file and returns its index, if
// the file is of the collection's documents as they are; else an error
// that says why not (ErrNoFile: there is none), and the caller builds
// the index.
func LoadFile(ctx context.Context, cat *catalog.Catalog, k *catalog.Collection) (*Index, error) {
	b, err := cat.ReadFile(k, FileName, maxIndexFile)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, ErrNoFile
	}
	if err != nil {
		return nil, err
	}
	ix, docs, err := decode(b, k)
	if err != nil {
		return nil, err
	}
	want := make(map[string]fileDoc, len(docs))
	for _, d := range docs {
		want[d.rel] = d
	}
	if len(want) != len(docs) {
		return nil, errCorrupt
	}
	have, versions, err := documents(ctx, cat, k, want)
	if err != nil {
		return nil, err
	}
	for _, d := range have {
		if want[d.rel].crc != d.crc {
			return nil, fmt.Errorf("%s/%s is not the document it was made of", k.Name, d.rel)
		}
	}
	ix.versions, ix.cat = versions, cat
	return ix, nil
}

var errCorrupt = errors.New("not an index file, or a damaged one")

// reader reads the format from a file's bytes; after an error, every
// read returns zero and err stays.
type reader struct {
	b   []byte
	err error
}

func (r *reader) fail() {
	r.err, r.b = errCorrupt, nil
}

func (r *reader) num() uint64 {
	n, w := binary.Uvarint(r.b)
	if w <= 0 {
		r.fail()
		return 0
	}
	r.b = r.b[w:]
	return n
}

// count is a number of things of at least size bytes each that follow:
// more than the file can hold is damage, not memory to allocate.
func (r *reader) count(size int) int {
	n := r.num()
	if n > uint64(len(r.b)/size) {
		r.fail()
		return 0
	}
	return int(n)
}

func (r *reader) bytes(n int) []byte {
	if n < 0 || n > len(r.b) {
		r.fail()
		return nil
	}
	out := r.b[:n]
	r.b = r.b[n:]
	return out
}

func (r *reader) str() string { return string(r.bytes(r.count(1))) }

// index is a number below n.
func (r *reader) index(n int) int {
	i := r.num()
	if i >= uint64(n) {
		r.fail()
		return 0
	}
	return int(i)
}

func decode(b []byte, k *catalog.Collection) (*Index, []fileDoc, error) {
	if len(b) < len(magic)+4 || string(b[:len(magic)]) != magic {
		return nil, nil, errCorrupt
	}
	body, sum := b[:len(b)-4], binary.LittleEndian.Uint32(b[len(b)-4:])
	if crc32.ChecksumIEEE(body) != sum {
		return nil, nil, errCorrupt
	}
	r := &reader{b: body[len(magic):]}
	if v := r.num(); r.err == nil && v != TokensVersion {
		return nil, nil, fmt.Errorf("it is of another version of the index's words (%d, not %d)", v, TokensVersion)
	}
	if li := r.num(); r.err == nil && (li == 1) != k.ListItems {
		return nil, nil, errors.New("it was made with another setting of list_items")
	}
	docs := make([]fileDoc, r.count(6))
	idocs := make([]*idoc, len(docs))
	for i := range docs {
		docs[i].rel = r.str()
		docs[i].size = int64(r.num())
		if c := r.bytes(4); len(c) == 4 {
			docs[i].crc = binary.LittleEndian.Uint32(c)
		}
		idocs[i] = &idoc{doc: catalog.Doc{Coll: k, Rel: docs[i].rel}}
	}
	words := make([]string, r.count(1))
	for i := range words {
		words[i] = r.str()
	}
	ix := &Index{postings: make(map[string][]posting, len(words))}
	ix.units = make([]unit, r.count(12))
	for i := range ix.units {
		u := &ix.units[i]
		if len(idocs) == 0 {
			r.fail()
			break
		}
		u.doc = idocs[r.index(len(idocs))]
		u.heading = unresolved
		u.id = r.str()
		u.length = float64(r.num())
		if c := r.bytes(8); len(c) == 8 {
			u.sum = binary.LittleEndian.Uint64(c)
		}
		if n := r.count(1); n > 0 && len(words) > 0 {
			u.title = make([]string, n)
			for j := range u.title {
				u.title[j] = words[r.index(len(words))]
			}
		} else if n > 0 {
			r.fail()
		}
		ix.total += u.length
	}
	for _, w := range words {
		ps := make([]posting, r.count(2))
		last := 0
		for j := range ps {
			d := r.num()
			if d > uint64(len(ix.units)) || (j > 0 && d == 0) {
				r.fail()
				break
			}
			last += int(d)
			if last >= len(ix.units) {
				r.fail()
				break
			}
			ps[j] = posting{unit: last, tf: float64(r.num())}
		}
		if len(ps) > 0 {
			ix.postings[w] = ps
		}
	}
	if r.err != nil || len(r.b) != 0 {
		return nil, nil, errCorrupt
	}
	if len(ix.units) > 0 {
		ix.avgLen = ix.total / float64(len(ix.units))
	}
	return ix, docs, nil
}

// appendCRC ends a file's bytes with their checksum.
func appendCRC(b []byte) []byte {
	return binary.LittleEndian.AppendUint32(b, crc32.ChecksumIEEE(b))
}
