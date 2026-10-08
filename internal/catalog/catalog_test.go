package catalog

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Addresses: collection/path, a path of the first collection, an
// absolute path, a section after #; mcp-server-fs's file paths are
// relative to the first collection only.
func TestResolve(t *testing.T) {
	a, b := t.TempDir(), t.TempDir()
	for _, p := range []string{filepath.Join(a, "x.md"), filepath.Join(a, "b", "y.md"), filepath.Join(b, "z.md")} {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("# x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	c := New([]*Collection{{Name: "a", Path: a}, {Name: "b", Path: b}}, []string{".md"}, 100)
	for _, tc := range []struct {
		addr      string
		files     bool
		coll, rel string
		section   string
	}{
		{"a/x.md", false, "a", "x.md", ""},
		{"b/z.md#sec", false, "b", "z.md", "sec"},
		{"x.md", false, "a", "x.md", ""},
		{"./x.md", false, "a", "x.md", ""},
		{"b", false, "b", ".", ""},
		// "b/y.md" is not in collection b, but is a's b/y.md? No: b names
		// a collection other than the first, which wins.
		{"b/y.md", false, "b", "y.md", ""},
		{"b/y.md", true, "a", "b/y.md", ""},
		{"a/x.md", true, "a", "a/x.md", ""},
		{filepath.Join(b, "z.md"), false, "b", "z.md", ""},
		{filepath.Join(a, "b", "y.md") + "#s", false, "a", "b/y.md", "s"},
	} {
		d, sec, err := c.Resolve(tc.addr, tc.files)
		if err != nil || d.Coll.Name != tc.coll || d.Rel != tc.rel || sec != tc.section {
			t.Errorf("Resolve(%q, %v) = %s %q %q %v, want %s %q %q", tc.addr, tc.files, d.Coll.Name, d.Rel, sec, err,
				tc.coll, tc.rel, tc.section)
		}
	}
	for _, bad := range []string{"", "../x.md", "a/../../x.md", "/etc/passwd"} {
		if _, _, err := c.Resolve(bad, false); err == nil {
			t.Errorf("Resolve(%q) succeeded", bad)
		}
	}
}

// A path of the first collection that starts with the name of the first
// collection is found, when no such document is in it by the name.
func TestResolveSelfNamed(t *testing.T) {
	a := t.TempDir()
	if err := os.MkdirAll(filepath.Join(a, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(a, "docs", "x.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	c := New([]*Collection{{Name: "docs", Path: a}}, []string{".md"}, 100)
	if d, _, err := c.Resolve("docs/docs/x.md", false); err != nil || d.Rel != "docs/x.md" {
		t.Errorf("prefixed: %+v %v", d, err)
	}
	if d, _, err := c.Resolve("docs/x.md", false); err != nil || d.Rel != "docs/x.md" {
		t.Errorf("unprefixed: %+v %v", d, err)
	}
}

func TestIsBinary(t *testing.T) {
	for s, want := range map[string]bool{"text": false, "ü": false, "a\x00b": true, "\xff\xfe": true, "abc\xc3": false} {
		if IsBinary([]byte(s)) != want {
			t.Errorf("IsBinary(%q) != %v", s, want)
		}
	}
}

// Include and exclude patterns decide what is a document; an also
// document is named by its base name; an index may be named.
func TestPatternsAlsoIndex(t *testing.T) {
	root, elsewhere := t.TempDir(), t.TempDir()
	for _, p := range []string{"README.md", "guide/a.md", "guide/drafts/d.md", "api/x.md", "skip.md"} {
		mk(t, filepath.Join(root, p))
	}
	mk(t, filepath.Join(elsewhere, "CHANGELOG.md"))
	k := &Collection{Name: "k", Path: root, Include: []string{"README.md", "guide/**"}, Exclude: []string{"drafts"},
		Also: []string{filepath.Join(elsewhere, "CHANGELOG.md")}, IndexName: "guide/a.md"}
	c := New([]*Collection{k}, []string{".md"}, 100)

	var got []string
	if err := c.Walk(context.Background(), Doc{Coll: k, Rel: "."}, nil, func(d Doc, e fs.DirEntry) bool {
		if !e.IsDir() {
			got = append(got, d.Rel)
		}
		return true
	}); err != nil {
		t.Fatal(err)
	}
	if strings.Join(got, " ") != "README.md guide/a.md CHANGELOG.md" {
		t.Errorf("walk: %v", got)
	}
	for _, rel := range []string{"api/x.md", "skip.md", "guide/drafts/d.md", "guide/drafts"} {
		if _, err := c.Stat(Doc{Coll: k, Rel: rel}); err == nil || !strings.Contains(err.Error(), "not part of the collection") {
			t.Errorf("%s: %v", rel, err)
		}
		if _, err := c.Load(context.Background(), Doc{Coll: k, Rel: rel}); err == nil {
			t.Errorf("%s loaded", rel)
		}
	}
	d, _, err := c.Resolve("k/CHANGELOG.md", false)
	if err != nil {
		t.Fatal(err)
	}
	if e, err := c.Load(context.Background(), d); err != nil || e.Outline.Title() != "CHANGELOG" ||
		d.Full() != filepath.Join(elsewhere, "CHANGELOG.md") {
		t.Errorf("also: %v %v", d.Full(), err)
	}
	if d, _, err := c.Resolve(filepath.Join(elsewhere, "CHANGELOG.md"), false); err != nil || d.Rel != "CHANGELOG.md" {
		t.Errorf("also by its path: %+v %v", d, err)
	}
	if idx, ok := c.Index(k); !ok || idx.Rel != "guide/a.md" {
		t.Errorf("index: %+v %v", idx, ok)
	}
	k.IndexName = "missing.md"
	if _, ok := c.Index(k); ok {
		t.Error("a missing index found")
	}
}

func mk(t *testing.T, p string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	name := strings.TrimSuffix(filepath.Base(p), ".md")
	if err := os.WriteFile(p, []byte("# "+name+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}
