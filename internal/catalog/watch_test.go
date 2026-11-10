package catalog

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestWatcher(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(os.MkdirAll(filepath.Join(dir, "topics"), 0o755))
	must(os.WriteFile(filepath.Join(dir, "topics/a.md"), []byte("# A\n"), 0o644))
	k := &Collection{Name: "k", Path: dir}
	other := &Collection{Name: "other", Path: t.TempDir()}
	c := New([]*Collection{k, other}, []string{".md"}, 100)

	var none *Watcher
	if !none.Changed(k) || none.Watch(ctx, c, k) {
		t.Error("no watcher: every collection may have changed, none is watched")
	}
	w := NewWatcher()
	if w == nil {
		t.Skip("no inotify here")
	}
	if !w.Changed(k) {
		t.Error("a collection not yet watched has not changed")
	}
	if !w.Watch(ctx, c, k) || !w.Watch(ctx, c, other) {
		t.Fatal("not watched")
	}
	// Reading changes nothing.
	if _, err := c.Load(ctx, Doc{Coll: k, Rel: "topics/a.md"}); err != nil {
		t.Fatal(err)
	}
	if w.Changed(k) {
		t.Error("changed by being read")
	}
	steps := map[string]func(){
		"a document written": func() { must(os.WriteFile(filepath.Join(dir, "topics/a.md"), []byte("# A\n\nmore\n"), 0o644)) },
		"a document added":   func() { must(os.WriteFile(filepath.Join(dir, "topics/b.md"), []byte("# B\n"), 0o644)) },
		"a document renamed": func() { must(os.Rename(filepath.Join(dir, "topics/b.md"), filepath.Join(dir, "topics/c.md"))) },
		"a document removed": func() { must(os.Remove(filepath.Join(dir, "topics/c.md"))) },
		"a directory added":  func() { must(os.MkdirAll(filepath.Join(dir, "more"), 0o755)) },
		"a document in the directory added since": func() {
			must(os.WriteFile(filepath.Join(dir, "more/d.md"), []byte("# D\n"), 0o644))
		},
	}
	for _, name := range []string{"a document written", "a document added", "a document renamed", "a document removed",
		"a directory added", "a document in the directory added since"} {
		steps[name]()
		if !w.Changed(k) {
			t.Errorf("%s: not seen", name)
		}
		if w.Changed(other) {
			t.Errorf("%s: the other collection changed", name)
		}
		// Watched anew, as the server does before it looks: nothing
		// changed since.
		if !w.Watch(ctx, c, k) || w.Changed(k) {
			t.Errorf("%s: changed right after watching anew", name)
		}
	}
	// A collection with documents elsewhere is looked at, not watched.
	also := &Collection{Name: "also", Path: t.TempDir(), Also: []string{filepath.Join(dir, "topics/a.md")}}
	if w.Watch(ctx, c, also) || !w.Changed(also) {
		t.Error("a collection with also documents is watched")
	}
}
