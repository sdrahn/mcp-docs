package rank

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/sdrahn/mcp-docs/internal/catalog"
)

func TestTokens(t *testing.T) {
	got := Tokens("How do I set agents.no_request_timeout? Instances, approvals; configuring the Proxies!")
	want := []string{"set", "agent", "no_request_timeout", "request", "timeout", "instanc", "approval", "configur", "proxy"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Tokens = %q, want %q", got, want)
	}
	for a, b := range map[string]string{"instance": "instances", "configure": "configured", "approval": "approvals",
		"policy": "policies", "start": "starting", "process": "processes"} {
		if Tokens(a)[0] != Tokens(b)[0] {
			t.Errorf("%s and %s do not meet: %v %v", a, b, Tokens(a), Tokens(b))
		}
	}
	for _, w := range []string{"status", "access", "analysis", "need"} {
		if got := Tokens(w)[0]; len(got) < len(w)-1 {
			t.Errorf("%s stemmed too far: %s", w, got)
		}
	}
	if len(Tokens("how is it, and what?")) != 0 {
		t.Error("stopwords")
	}
}

func write(t *testing.T, p, s string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
		t.Fatal(err)
	}
}

func ids(hits []Hit) []string {
	var out []string
	for _, h := range hits {
		if h.Heading < 0 {
			out = append(out, h.Doc.Rel+"#")
			continue
		}
		out = append(out, h.Doc.Rel+"#"+h.Entry.Outline.Headings[h.Heading].ID)
	}
	return out
}

// A section whose title names the topic ranks above one that mentions
// it often; each section counts its own text only, not its subsections'.
func TestSearch(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "ops.md"), "# Operations\nIntro.\n## Lifecycle\nAn instance starts; the instance stops; "+
		"an instance restarts; each instance; instance instance.\n## Troubleshooting\n"+strings.Repeat("Look at the logs first.\n", 3)+
		"### Instances do not start\nCheck the unit's journal and the SELinux denials.\n"+strings.Repeat("More words here.\n", 20))
	write(t, filepath.Join(dir, "backup.md"), "No headings: back up /var/lib/app and the keys.\n")
	k := &catalog.Collection{Name: "c", Path: dir}
	cat := catalog.New([]*catalog.Collection{k}, []string{".md"}, 100)
	ix, err := Build(context.Background(), cat, k, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	hits, err := ix.Search("an instance does not start", 3)
	if err != nil || len(hits) == 0 || ids(hits)[0] != "ops.md#instances-do-not-start" {
		t.Errorf("instances: %v %v", ids(hits), err)
	}
	if hits, _ := ix.Search("logs", 5); len(hits) != 1 || ids(hits)[0] != "ops.md#troubleshooting" {
		t.Errorf("own text: %v", ids(hits))
	}
	if hits, _ := ix.Search("back up keys", 5); len(hits) != 1 || ids(hits)[0] != "backup.md#" {
		t.Errorf("no headings: %v", ids(hits))
	}
	if hits, _ := ix.Search("zebra", 5); len(hits) != 0 {
		t.Errorf("no match: %v", ids(hits))
	}
	// Front matter is no text: a document whose only text before its
	// first heading is front matter has no such unit.
	write(t, filepath.Join(dir, "fm.md"), "---\ntitle: Quartz\ndescription: Quartz clocks and their care.\n---\n\n# Quartz\n## Care\nWind it.\n")
	ix, _ = Build(context.Background(), cat, k, 1<<20)
	if hits, _ := ix.Search("quartz clocks care", 5); len(hits) == 0 || ids(hits)[0] == "fm.md#" {
		t.Errorf("front matter ranked: %v", ids(hits))
	} else {
		for _, id := range ids(hits) {
			if id == "fm.md#" {
				t.Errorf("front matter ranked: %v", ids(hits))
			}
		}
	}
	if _, err := ix.Search("how is it?", 5); err != ErrNoTerms {
		t.Errorf("stopwords only: %v", err)
	}
	// OwnLines: from the heading to before the next one.
	o := hits0(t, ix, "lifecycle").Entry.Outline
	if f, l := OwnLines(o, 1); f != 3 || l != 4 {
		t.Errorf("OwnLines: %d-%d", f, l)
	}

	// The index is current until a document changes, is added or goes.
	if !ix.Current(context.Background(), cat, k) {
		t.Error("not current")
	}
	p := filepath.Join(dir, "backup.md")
	write(t, p, "No headings: back up /var/lib/app and the certificates.\n")
	_ = os.Chtimes(p, time.Now().Add(time.Minute), time.Now().Add(time.Minute))
	if ix.Current(context.Background(), cat, k) {
		t.Error("current after a change")
	}
	ix, _ = Build(context.Background(), cat, k, 1<<20)
	write(t, filepath.Join(dir, "new.md"), "# New\n")
	if ix.Current(context.Background(), cat, k) {
		t.Error("current after an addition")
	}
	ix, _ = Build(context.Background(), cat, k, 1<<20)
	_ = os.Remove(filepath.Join(dir, "new.md"))
	if ix.Current(context.Background(), cat, k) {
		t.Error("current after a removal")
	}
}

func hits0(t *testing.T, ix *Index, q string) Hit {
	t.Helper()
	h, err := ix.Search(q, 1)
	if err != nil || len(h) == 0 {
		t.Fatalf("%s: %v", q, err)
	}
	return h[0]
}
