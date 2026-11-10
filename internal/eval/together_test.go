package eval

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/sdrahn/mcp-docs/internal/catalog"
	"github.com/sdrahn/mcp-docs/internal/server"
)

// The eval of collections served together (docs/architecture.md, §17):
// the questions of SUSE's documentation asked of all its collections at
// once, without naming one, as an agent asks that does not know where
// the answer is. It counts the questions whose answering section is the
// first result (its text is in the answer), the results that repeat a
// topic of the same answer, and the bytes.
//
// MCP_DOCS_TOGETHER names a directory whose subdirectories are the
// collections (/usr/share/mcp-docs/docs/suse with the suse-docs-mcp-*
// packages installed, as CI has them; or the output directories of
// tools/suse-docs/convert under one directory). Without it the test is
// skipped: the collections are not in the repository.
//
//	MCP_DOCS_TOGETHER=/usr/share/mcp-docs/docs/suse make eval-together
//
// With all the collections of togetherAll, the counts must not fall
// below togetherFloors: a change to ranking is measured here.

// families are the questions by the product whose collections answer
// them: a collection's name is the family and a version ("sles-16.1"),
// or the family ("sle-micro").
var families = map[string][]Question{"sles": SLES, "sle-ha": SLEHA, "sles-sap": SLESSAP, "sle-micro": SLEMicro}

var versionSuffix = regexp.MustCompile(`-\d+(\.\d+)*$`)

func familyOf(collection string) string { return versionSuffix.ReplaceAllString(collection, "") }

// togetherAll are the collections the floors are of: those of the
// packages in packaging/suse-docs, at their pinned revisions.
var togetherAll = []string{"sle-ha-16.0", "sle-ha-16.1", "sle-micro", "sles-16.0", "sles-16.1", "sles-sap-16.0", "sles-sap-16.1"}

// tally is what a run of questions came to.
type tally struct {
	questions, first, repeats int
	bytes                     int
	missed                    []string
}

func (a *tally) add(b tally) {
	a.questions += b.questions
	a.first += b.first
	a.repeats += b.repeats
	a.bytes += b.bytes
	a.missed = append(a.missed, b.missed...)
}

func (a tally) row(name string) string {
	return fmt.Sprintf("| %s | %d of %d | %d | %s | %d B |", name, a.first, a.questions, a.repeats, size(int64(a.bytes)),
		a.bytes/max(a.questions, 1))
}

func togetherServer(t *testing.T) (*server.Server, []string) {
	root := os.Getenv("MCP_DOCS_TOGETHER")
	if root == "" {
		t.Skip("MCP_DOCS_TOGETHER is not set: the directory of the collections served together")
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	var colls []*catalog.Collection
	var names []string
	for _, e := range entries {
		dir := filepath.Join(root, e.Name())
		if _, err := os.Stat(filepath.Join(dir, "README.md")); err != nil || !e.IsDir() {
			continue
		}
		colls = append(colls, &catalog.Collection{Name: e.Name(), Path: dir})
		names = append(names, e.Name())
	}
	if len(colls) == 0 {
		t.Skipf("%s: no collections", root)
	}
	sort.Strings(names)
	return &server.Server{Cat: catalog.New(colls, []string{".md"}, 100000), MaxRead: 16 << 10, MaxFile: 16 << 20,
		Structured: true, Version: "eval"}, names
}

// ask asks find_sections a family's questions, of all collections.
func ask(t *testing.T, s *server.Server, questions []Question) tally {
	t.Helper()
	var out tally
	for _, q := range questions {
		p, _ := json.Marshal(map[string]any{"name": "find_sections", "arguments": map[string]any{"query": q.Ask}})
		res, err := s.Handle(context.Background(), "tools/call", p)
		if err != nil {
			t.Fatal(err)
		}
		b, _ := json.Marshal(res)
		var r struct {
			Content []struct{ Text string }
			IsError bool
			S       struct {
				Sections []struct{ Doc, Section string }
				Read     struct{ Section string }
			} `json:"structuredContent"`
		}
		if err := json.Unmarshal(b, &r); err != nil || r.IsError || len(r.Content) == 0 {
			t.Fatalf("%q: %s", q.Ask, b)
		}
		out.questions++
		out.bytes += len(r.Content[0].Text)
		// Answered: the first result is an answering section, or the
		// part of it whose text the answer holds is.
		if len(r.S.Sections) > 0 && (slices.Contains(q.Want, r.S.Sections[0].Section) || slices.Contains(q.Want, r.S.Read.Section)) {
			out.first++
		} else {
			out.missed = append(out.missed, q.Ask)
		}
		// A repeat: a result of the document and section an earlier one
		// of the answer has, in another collection or version.
		seen := map[string]bool{}
		for _, sec := range r.S.Sections {
			k := path.Base(sec.Doc) + "#" + sec.Section
			if seen[k] {
				out.repeats++
			}
			seen[k] = true
		}
	}
	return out
}

// togetherFloors are what the eval came to when it was last looked at,
// with the collections of togetherAll: a run must not answer fewer
// questions first, nor repeat more. When a change does better, raise
// them: they are what the next change is measured against.
var togetherFloors = struct{ first, repeats int }{first: 53, repeats: 38}

func TestTogether(t *testing.T) {
	s, names := togetherServer(t)
	var total tally
	rows := []string{}
	var fams []string
	for f := range families {
		fams = append(fams, f)
	}
	sort.Strings(fams)
	for _, f := range fams {
		if !slices.ContainsFunc(names, func(n string) bool { return familyOf(n) == f }) {
			continue
		}
		a := ask(t, s, families[f])
		total.add(a)
		rows = append(rows, a.row(f))
	}
	report := fmt.Sprintf("Collections: %s. No collection named.\n\n", strings.Join(names, ", ")) +
		"| Questions of | Answered first | Results repeating a topic | Bytes | A question |\n|---|---|---|---|---|\n" +
		strings.Join(rows, "\n") + "\n" + total.row("**all**") + "\n"
	t.Log("\n" + report)
	for _, m := range total.missed {
		t.Logf("not first: %s", m)
	}
	if p := os.Getenv("EVAL_REPORT"); p != "" {
		f, err := os.OpenFile(p, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = f.Close() }()
		_, _ = fmt.Fprintf(f, "## Collections served together (mcp-docs eval)\n\n%s\n", report)
	}
	if !slices.Equal(names, togetherAll) {
		// CI installs them all: fewer there is a package that was not
		// built, not a smaller run.
		if os.Getenv("CI") != "" {
			t.Errorf("the collections are not those of the floors (%s)", strings.Join(togetherAll, ", "))
		}
		t.Logf("the floors are of %s: not checked", strings.Join(togetherAll, ", "))
		return
	}
	if total.first < togetherFloors.first {
		t.Errorf("%d questions answered first, fewer than the %d before", total.first, togetherFloors.first)
	}
	if total.repeats > togetherFloors.repeats {
		t.Errorf("%d results repeat a topic, more than the %d before", total.repeats, togetherFloors.repeats)
	}
	if total.first > togetherFloors.first || total.repeats < togetherFloors.repeats {
		t.Logf("better than the floors (%d first, %d repeats): raise them", togetherFloors.first, togetherFloors.repeats)
	}
}
