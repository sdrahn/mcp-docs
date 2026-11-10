// Package rank finds the sections of a collection that best match a
// question in words (docs/architecture.md, §5.6): BM25 over each
// section's own text (from its heading to the next heading of any
// level) and the titles above it, with a bonus for each query word in
// the section's own title.
// No embeddings, no network, no model: the index is built from the
// documents when first asked and again when one of them changes.
package rank

import (
	"context"
	"errors"
	"hash"
	"hash/fnv"
	"io"
	"io/fs"
	"math"
	"slices"
	"sort"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"

	"github.com/sdrahn/mcp-docs/internal/catalog"
	"github.com/sdrahn/mcp-docs/internal/markdown"
)

// BM25 parameters; the weight of the titles above a section in its
// text; and the bonus of each query word in the section's own title, in
// units of the word's idf. A heading that names the topic is the
// strongest sign a section answers it: as a bonus, it is not weakened
// by a long section's length (a section titled "Instances do not
// start" ranks above a short one that says "instance" five times).
const (
	k1         = 1.2
	b          = 0.75
	pathWeight = 1
	titleBonus = 2
)

// unit is a section's own text, or the text before a document's first
// heading (Heading -1).
type unit struct {
	doc *idoc
	// heading is the index of the unit's heading in the document's
	// outline, or unresolved in an index read from a file, which knows
	// the heading by its id (locate finds it in the outline).
	heading int
	id      string   // the heading's id; "" before the first heading
	length  float64  // in words; a whole number
	title   []string // the words of its own title
	sum     uint64   // of its own text, to tell copies
}

const unresolved = -2

// idoc is a document of the index; its outline is loaded when a hit
// needs it, in an index read from a file.
type idoc struct {
	doc   catalog.Doc
	entry *catalog.Entry
	heads map[string]int // heading id → index
	bad   bool           // it cannot be loaded
}

type posting struct {
	unit int
	tf   float64
}

// Index ranks the sections of one collection.
type Index struct {
	versions map[string]string // document → version it was built from
	units    []unit
	postings map[string][]posting
	avgLen   float64
	total    float64 // the units' lengths together

	cat *catalog.Catalog
	// mu guards the documents' outlines, loaded as hits need them, and
	// stale.
	mu    sync.Mutex
	stale bool
}

// Build indexes the documents of a collection; one larger than maxFile
// is left out.
func Build(ctx context.Context, cat *catalog.Catalog, k *catalog.Collection, maxFile int64) (*Index, error) {
	ix := &Index{versions: map[string]string{}, postings: map[string][]posting{}, cat: cat}
	var total float64
	err := cat.Walk(ctx, catalog.Doc{Coll: k, Rel: "."}, nil, func(d catalog.Doc, e fs.DirEntry) bool {
		if e.IsDir() {
			return true
		}
		// Every document's version, also of one that cannot be read or
		// is no text: the index is current while it stays as it is.
		fi, err := e.Info()
		if err != nil {
			return true
		}
		ix.versions[d.Address()] = catalog.Version(fi)
		en, err := cat.Load(ctx, d)
		if err != nil {
			return ctx.Err() == nil
		}
		if en.Outline.Meta.Unranked {
			return true
		}
		text, err := cat.ReadAll(d, maxFile)
		if err != nil {
			return true
		}
		total += ix.add(d, en, string(text))
		return true
	})
	if err != nil {
		return nil, err
	}
	ix.total = total
	if len(ix.units) > 0 {
		ix.avgLen = total / float64(len(ix.units))
	}
	return ix, nil
}

// add indexes a document's sections and returns their total length.
func (ix *Index) add(d catalog.Doc, en *catalog.Entry, text string) float64 {
	o := en.Outline
	lines := strings.Split(strings.TrimSuffix(text, "\n"), "\n")
	// The text of each unit: index i+1 for heading i, 0 for the text
	// before the first heading.
	tf := make([]map[string]float64, len(o.Headings)+1)
	lengths := make([]float64, len(o.Headings)+1)
	sums := make([]hash.Hash64, len(o.Headings)+1)
	for i := range tf {
		tf[i] = map[string]float64{}
		sums[i] = fnv.New64a()
	}
	for n, l := range lines {
		if n < o.FrontMatter {
			continue // metadata (its title and description are in list_docs), not text
		}
		u := o.At(n+1) + 1
		_, _ = io.WriteString(sums[u], l)
		_, _ = sums[u].Write([]byte{'\n'})
		for _, t := range Tokens(l) {
			tf[u][t]++
			lengths[u]++
		}
	}
	titles := make([][]string, len(o.Headings)+1)
	for i, h := range o.Headings {
		titles[i+1] = unique(Tokens(h.Title))
		for p := h.Parent; p >= 0; p = o.Headings[p].Parent {
			for _, t := range Tokens(o.Headings[p].Title) {
				tf[i+1][t] += pathWeight
				lengths[i+1] += pathWeight
			}
		}
	}
	// The text before the first heading has the document's title, when
	// it has any text.
	if lengths[0] > 0 {
		titles[0] = unique(Tokens(o.Title()))
	}
	doc := &idoc{doc: d, entry: en}
	var total float64
	for u := range tf {
		if lengths[u] == 0 {
			continue
		}
		id := len(ix.units)
		nu := unit{doc: doc, heading: u - 1, length: lengths[u], title: titles[u], sum: sums[u].Sum64()}
		if u > 0 {
			nu.id = o.Headings[u-1].ID
		}
		ix.units = append(ix.units, nu)
		for t, n := range tf[u] {
			ix.postings[t] = append(ix.postings[t], posting{unit: id, tf: n})
		}
		total += lengths[u]
	}
	return total
}

// Current reports whether the index was built from the documents as
// they are: the same documents, of the same versions. It walks the
// collection and opens no document: a version is of what the directory
// tells of a file.
func (ix *Index) Current(ctx context.Context, cat *catalog.Catalog, k *catalog.Collection) bool {
	seen := 0
	current := true
	_ = cat.Walk(ctx, catalog.Doc{Coll: k, Rel: "."}, nil, func(d catalog.Doc, e fs.DirEntry) bool {
		if e.IsDir() {
			return true
		}
		seen++
		fi, err := e.Info()
		if err != nil || ix.versions[d.Address()] != catalog.Version(fi) {
			current = false
			return false
		}
		return true
	})
	ix.mu.Lock()
	stale := ix.stale
	ix.mu.Unlock()
	return current && !stale && seen == len(ix.versions) && ctx.Err() == nil
}

// Hit is a section and its score.
type Hit struct {
	Doc     catalog.Doc
	Entry   *catalog.Entry
	Heading int // -1: the text before the first heading
	Score   float64
	// Sum is of the section's own text: the same in two documents that
	// hold the same section (a topic two collections share).
	Sum uint64
}

// ErrNoTerms is a query of stopwords and punctuation only.
var ErrNoTerms = errors.New("query: no words to look for (only common words and punctuation)")

// Search returns the n sections that best match the query, best first.
func (ix *Index) Search(query string, n int) ([]Hit, error) {
	return ix.search(query, n, nil)
}

// Under returns the n sections of a document, or of the documents below
// a directory ("." for all), that best match the query, best first.
func (ix *Index) Under(query string, d catalog.Doc, n int) ([]Hit, error) {
	return ix.search(query, n, under(d))
}

// under keeps the units of a document, or of the documents below a
// directory; nil for a collection's root: all.
func under(d catalog.Doc) func(*unit) bool {
	if d.Rel == "." || d.Rel == "" {
		return nil
	}
	return func(u *unit) bool {
		return u.doc.doc.Rel == d.Rel || strings.HasPrefix(u.doc.doc.Rel, d.Rel+"/")
	}
}

// Within returns the n subsections of a document's section (heading
// index; at any depth, not the section itself) that best match the
// query, best first: where in a large section the answer is.
func (ix *Index) Within(query string, d catalog.Doc, heading, n int) ([]Hit, error) {
	return ix.search(query, n, func(u *unit) bool {
		if u.doc.doc.Rel != d.Rel {
			return false
		}
		e, h, ok := ix.locate(u)
		if !ok || h <= heading {
			return false
		}
		hs := e.Outline.Headings
		for p := hs[h].Parent; p >= 0; p = hs[p].Parent {
			if p == heading {
				return true
			}
		}
		return false
	})
}

func (ix *Index) search(query string, n int, keep func(*unit) bool) ([]Hit, error) {
	terms, also := queryTerms(query, ix)
	if len(terms) == 0 {
		return nil, ErrNoTerms
	}
	return hits(ix.score(terms, statsOf(terms, also, ix), keep), n), nil
}

// QueryTerms are the words a query is ranked by in the indexes: its own
// (Tokens) and those of its other spellings.
func QueryTerms(query string, ixs ...*Index) []string {
	terms, _ := queryTerms(query, ixs...)
	return terms
}

// spellingWeight is what a word of another spelling counts, of a word
// of the query's own.
const spellingWeight = 0.6

// particles end a word that is also written as two ("login", "setup",
// "timeout"), and are too common to be looked for.
var particles = map[string]bool{"in": true, "up": true, "on": true, "out": true, "off": true, "down": true}

// queryTerms returns the query's words and, in also, those among them
// that are of another spelling of what the query says: a word the
// documents write as two, and two they write as one ("login" and "log
// in", "live patch" and "livepatch"). The index has no meanings, but it
// has the documents' words: two of the query's words are also asked as
// one where the documents have that word, and a word as two where they
// have both and each more often than the word. So a question finds what
// is written the other way, which it missed.
func queryTerms(query string, ixs ...*Index) (terms []string, also map[string]bool) {
	terms = unique(Tokens(query))
	df := func(t string) int {
		n := 0
		for _, ix := range ixs {
			n += len(ix.postings[t])
		}
		return n
	}
	also = map[string]bool{}
	add := func(t string) {
		if !slices.Contains(terms, t) {
			terms = append(terms, t)
			also[t] = true
		}
	}
	words := strings.FieldsFunc(strings.ToLower(query), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
	for i, w := range words {
		// Two words as one.
		if i+1 < len(words) {
			if j := stem(w + words[i+1]); df(j) > 0 {
				add(j)
			}
		}
		// One word as two.
		if stopwords[w] || len(w) < 5 {
			continue
		}
		own := df(stem(w))
		for k := 3; k <= len(w)-2; k++ {
			if !utf8.RuneStart(w[k]) {
				continue
			}
			a, b := stem(w[:k]), w[k:]
			if stopwords[w[:k]] || df(a) <= own {
				continue
			}
			switch {
			case particles[b]:
				add(a)
			case len(b) >= 3 && !stopwords[b] && df(stem(b)) > own:
				add(a)
				add(stem(b))
			}
		}
	}
	return terms, also
}

// scored is a unit of an index and its score.
type scored struct {
	ix    *Index
	unit  int
	score float64
}

// hits are the n best of the scored units, best first, keeping the
// order of equal ones, as Hits: only their documents' outlines are
// needed.
func hits(sc []scored, n int) []Hit {
	sort.SliceStable(sc, func(i, j int) bool { return sc[i].score > sc[j].score })
	out := make([]Hit, 0, min(n, len(sc)))
	for _, c := range sc {
		if len(out) == n {
			break
		}
		u := &c.ix.units[c.unit]
		e, h, ok := c.ix.locate(u)
		if !ok {
			continue
		}
		out = append(out, Hit{Doc: u.doc.doc, Entry: e, Heading: h, Score: c.score, Sum: u.sum})
	}
	return out
}

// locate returns the outline of a unit's document and the unit's
// heading in it. In an index read from a file, the document is loaded
// when first asked for; a unit whose heading the outline does not have
// (the index is of another reading of the document) marks the index
// stale, to be built anew, and is not located.
func (ix *Index) locate(u *unit) (*catalog.Entry, int, bool) {
	if u.heading != unresolved {
		return u.doc.entry, u.heading, true
	}
	ix.mu.Lock()
	defer ix.mu.Unlock()
	d := u.doc
	if d.entry == nil && !d.bad {
		e, err := ix.cat.Load(context.Background(), d.doc)
		if err != nil {
			d.bad = true
		} else {
			d.entry = e
			d.heads = make(map[string]int, len(e.Outline.Headings))
			for i, h := range e.Outline.Headings {
				d.heads[h.ID] = i
			}
		}
	}
	if d.bad {
		ix.stale = true
		return nil, 0, false
	}
	if u.id == "" {
		return d.entry, -1, true
	}
	h, ok := d.heads[u.id]
	if !ok {
		ix.stale = true
	}
	return d.entry, h, ok
}

// Part is an index and the document or directory of its collection to
// rank below ("." for all of it).
type Part struct {
	Index *Index
	Under catalog.Doc
	// Boost, if not 0, multiplies the scores of the part's sections:
	// above 1 for a collection to rank before others where they answer
	// alike (the collection of the system the server runs on).
	Boost float64
}

// Search returns the n sections of the parts that best match the query,
// best first; of equal ones, those of the earlier part. The parts are
// ranked as one: a word weighs by how rare it is in all their
// collections together, so that the scores of two collections compare
// (a word rare in one and common in the other would else rank the
// first's passing mention above the second's section on it).
func Search(query string, n int, parts []Part) ([]Hit, error) {
	var ixs []*Index
	for _, p := range parts {
		if !slices.Contains(ixs, p.Index) {
			ixs = append(ixs, p.Index)
		}
	}
	terms, also := queryTerms(query, ixs...)
	if len(terms) == 0 {
		return nil, ErrNoTerms
	}
	st := statsOf(terms, also, ixs...)
	var sc []scored
	for _, p := range parts {
		part := p.Index.score(terms, st, under(p.Under))
		if p.Boost != 0 {
			for i := range part {
				part[i].score *= p.Boost
			}
		}
		sc = append(sc, part...)
	}
	return hits(sc, n), nil
}

// stats are what BM25 knows of the texts it ranks: how many units, how
// long on average, and in how many of them each word of the query is.
type stats struct {
	n, avgLen float64
	df        map[string]float64
	// also are the words of another spelling: they count less.
	also map[string]bool
}

func statsOf(terms []string, also map[string]bool, ixs ...*Index) stats {
	st := stats{df: map[string]float64{}, also: also}
	var total float64
	for _, ix := range ixs {
		st.n += float64(len(ix.units))
		total += ix.total
		for _, t := range terms {
			st.df[t] += float64(len(ix.postings[t]))
		}
	}
	if st.n > 0 {
		st.avgLen = total / st.n
	}
	return st
}

// score returns the units of the index that have a word of the query
// and that keep keeps (nil: all), each with its score, in no order.
func (ix *Index) score(terms []string, st stats, keep func(*unit) bool) []scored {
	scores := map[int]float64{}
	for _, t := range terms {
		ps := ix.postings[t]
		if len(ps) == 0 {
			continue
		}
		idf := math.Log(1 + (st.n-st.df[t]+0.5)/(st.df[t]+0.5))
		if st.also[t] {
			idf *= spellingWeight
		}
		for _, p := range ps {
			u := &ix.units[p.unit]
			if keep != nil && !keep(u) {
				continue
			}
			norm := k1 * (1 - b + b*u.length/st.avgLen)
			scores[p.unit] += idf * p.tf * (k1 + 1) / (p.tf + norm)
			if slices.Contains(u.title, t) {
				scores[p.unit] += titleBonus * idf
			}
		}
	}
	ids := make([]int, 0, len(scores))
	for id := range scores {
		ids = append(ids, id)
	}
	sort.Ints(ids) // document order
	out := make([]scored, 0, len(ids))
	for _, id := range ids {
		out = append(out, scored{ix: ix, unit: id, score: scores[id]})
	}
	return out
}

// OwnLines returns the first and last line of a hit's own text: from
// its heading (or the document's start) to before the next heading.
func OwnLines(o *markdown.Outline, heading int) (int, int) {
	first, last := 1, o.Lines
	if heading >= 0 {
		first = o.Headings[heading].Line
	}
	if heading+1 < len(o.Headings) {
		last = o.Headings[heading+1].Line - 1
	}
	return first, last
}

// Tokens are the words of a text as the index has them: lowercased,
// split at anything but letters, digits and "_", without common words,
// with a light stemming of English suffixes. An identifier with "_" is
// a token whole and in its parts (no_request_timeout: also no, request,
// timeout).
func Tokens(text string) []string {
	var out []string
	for _, w := range strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_'
	}) {
		w = strings.Trim(w, "_")
		if w == "" {
			continue
		}
		if strings.Contains(w, "_") {
			out = append(out, w)
			for _, p := range strings.Split(w, "_") {
				if p != "" && !stopwords[p] {
					out = append(out, stem(p))
				}
			}
			continue
		}
		if !stopwords[w] {
			out = append(out, stem(w))
		}
	}
	return out
}

// stem strips common English suffixes, conservatively: enough for
// "approvals" to find "approval" and "configuring" "configure".
func stem(w string) string {
	n := len(w)
	switch {
	case n > 4 && strings.HasSuffix(w, "ies"):
		return w[:n-3] + "y"
	case n > 4 && strings.HasSuffix(w, "sses"):
		return w[:n-2]
	case n > 5 && strings.HasSuffix(w, "ing"):
		return trimE(w[:n-3])
	case n > 4 && strings.HasSuffix(w, "ed") && !strings.HasSuffix(w, "eed"):
		return trimE(w[:n-2])
	case n > 3 && strings.HasSuffix(w, "s") && !strings.HasSuffix(w, "ss") &&
		!strings.HasSuffix(w, "us") && !strings.HasSuffix(w, "is"):
		return trimE(w[:n-1]) // "instances" meets "instance"
	}
	return trimE(w)
}

// trimE drops a final "e" so that "configure", "configur(ing)" and
// "configur(ed)" meet.
func trimE(w string) string {
	if len(w) > 4 && strings.HasSuffix(w, "e") {
		return w[:len(w)-1]
	}
	return w
}

func unique(ts []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, t := range ts {
		if !seen[t] {
			seen[t] = true
			out = append(out, t)
		}
	}
	return out
}

var stopwords = map[string]bool{}

func init() {
	for _, w := range strings.Fields(`a an and are as at be but by can do does for from how i if in into is it
		its my of on or so that the their them then there these this to was we what when where which who
		why will with you your should could would may must not no yes`) {
		stopwords[w] = true
	}
}
