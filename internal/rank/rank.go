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
	"io/fs"
	"math"
	"sort"
	"strings"
	"unicode"

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
	doc     catalog.Doc
	heading int
	entry   *catalog.Entry
	length  float64
	title   map[string]bool // the words of its own title
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
}

// Build indexes the documents of a collection; one larger than maxFile
// is left out.
func Build(ctx context.Context, cat *catalog.Catalog, k *catalog.Collection, maxFile int64) (*Index, error) {
	ix := &Index{versions: map[string]string{}, postings: map[string][]posting{}}
	var total float64
	err := cat.Walk(ctx, catalog.Doc{Coll: k, Rel: "."}, nil, func(d catalog.Doc, e fs.DirEntry) bool {
		if e.IsDir() {
			return true
		}
		en, err := cat.Load(ctx, d)
		if err != nil {
			return ctx.Err() == nil
		}
		ix.versions[d.Address()] = en.Version
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
	for i := range tf {
		tf[i] = map[string]float64{}
	}
	for n, l := range lines {
		if n < o.FrontMatter {
			continue // metadata (its title and description are in list_docs), not text
		}
		u := o.At(n+1) + 1
		for _, t := range Tokens(l) {
			tf[u][t]++
			lengths[u]++
		}
	}
	titles := make([]map[string]bool, len(o.Headings)+1)
	titles[0] = map[string]bool{}
	for i, h := range o.Headings {
		titles[i+1] = map[string]bool{}
		for _, t := range Tokens(h.Title) {
			titles[i+1][t] = true
		}
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
		for _, t := range Tokens(o.Title()) {
			titles[0][t] = true
		}
	}
	var total float64
	for u := range tf {
		if lengths[u] == 0 {
			continue
		}
		id := len(ix.units)
		ix.units = append(ix.units, unit{doc: d, heading: u - 1, entry: en, length: lengths[u], title: titles[u]})
		for t, n := range tf[u] {
			ix.postings[t] = append(ix.postings[t], posting{unit: id, tf: n})
		}
		total += lengths[u]
	}
	return total
}

// Current reports whether the index was built from the documents as
// they are: the same documents, of the same versions.
func (ix *Index) Current(ctx context.Context, cat *catalog.Catalog, k *catalog.Collection) bool {
	seen := 0
	current := true
	_ = cat.Walk(ctx, catalog.Doc{Coll: k, Rel: "."}, nil, func(d catalog.Doc, e fs.DirEntry) bool {
		if e.IsDir() {
			return true
		}
		seen++
		en, err := cat.Load(ctx, d)
		if err != nil || ix.versions[d.Address()] != en.Version {
			current = false
			return false
		}
		return true
	})
	return current && seen == len(ix.versions) && ctx.Err() == nil
}

// Hit is a section and its score.
type Hit struct {
	Doc     catalog.Doc
	Entry   *catalog.Entry
	Heading int // -1: the text before the first heading
	Score   float64
}

// ErrNoTerms is a query of stopwords and punctuation only.
var ErrNoTerms = errors.New("query: no words to look for (only common words and punctuation)")

// Search returns the n sections that best match the query, best first.
func (ix *Index) Search(query string, n int) ([]Hit, error) {
	terms := unique(Tokens(query))
	if len(terms) == 0 {
		return nil, ErrNoTerms
	}
	scores := map[int]float64{}
	N := float64(len(ix.units))
	for _, t := range terms {
		ps := ix.postings[t]
		if len(ps) == 0 {
			continue
		}
		idf := math.Log(1 + (N-float64(len(ps))+0.5)/(float64(len(ps))+0.5))
		for _, p := range ps {
			u := ix.units[p.unit]
			norm := k1 * (1 - b + b*u.length/ix.avgLen)
			scores[p.unit] += idf * p.tf * (k1 + 1) / (p.tf + norm)
			if u.title[t] {
				scores[p.unit] += titleBonus * idf
			}
		}
	}
	ids := make([]int, 0, len(scores))
	for id := range scores {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool {
		if scores[ids[i]] != scores[ids[j]] {
			return scores[ids[i]] > scores[ids[j]]
		}
		return ids[i] < ids[j] // document order
	})
	if len(ids) > n {
		ids = ids[:n]
	}
	hits := make([]Hit, 0, len(ids))
	for _, id := range ids {
		u := ix.units[id]
		hits = append(hits, Hit{Doc: u.doc, Entry: u.entry, Heading: u.heading, Score: scores[id]})
	}
	return hits, nil
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
