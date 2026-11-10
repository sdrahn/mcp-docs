package rank

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/sdrahn/mcp-docs/internal/catalog"
)

func fileCollection(t testing.TB) (*catalog.Catalog, *catalog.Collection) {
	dir := t.TempDir()
	files := map[string]string{
		"README.md":       "---\ntitle: Frontmatter Title\nrank: false\n---\n\n# Index\n\nA list of everything.\n",
		"guide.md":        "---\ntitle: Operating Guide\n---\n\nIntroduction to operating the cluster.\n\n# Guide\n\n## Timeouts\n\nSet `no_request_timeout` in the file.\n\n### Approvals\n\nApprovals expire.\n\n## Timeouts\n\nA second section of the title.\n",
		"topics/nodes.md": "# Nodes\n\nPut a node into maintenance mode.\n\n## Removing a node\n\nRemove the node from the cluster.\n",
		"binary.md":       "\x00\x01\x02",
		"empty.md":        "",
	}
	for name, text := range files {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	k := &catalog.Collection{Name: "k", Path: dir}
	return catalog.New([]*catalog.Collection{k}, []string{".md"}, 1000), k
}

// same compares what two indexes answer: sections, scores and sums.
func same(t *testing.T, a, b *Index, queries []string) {
	t.Helper()
	type row struct {
		doc     string
		heading int
		score   float64
		sum     uint64
	}
	rows := func(ix *Index, q string) []row {
		hs, err := ix.Search(q, 20)
		if err != nil {
			t.Fatal(err)
		}
		var out []row
		for _, h := range hs {
			out = append(out, row{h.Doc.Address(), h.Heading, h.Score, h.Sum})
		}
		return out
	}
	for _, q := range queries {
		if x, y := rows(a, q), rows(b, q); !reflect.DeepEqual(x, y) {
			t.Errorf("%q:\n built %v\n read  %v", q, x, y)
		} else if len(x) == 0 {
			t.Errorf("%q finds nothing: the comparison says nothing", q)
		}
	}
}

func TestIndexFile(t *testing.T) {
	ctx := context.Background()
	cat, k := fileCollection(t)
	if _, err := LoadFile(ctx, cat, k); err != ErrNoFile {
		t.Fatalf("without a file: %v", err)
	}
	if err := WriteFile(ctx, cat, k, 1<<20); err != nil {
		t.Fatal(err)
	}
	built, err := Build(ctx, cat, k, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	read, err := LoadFile(ctx, cat, k)
	if err != nil {
		t.Fatal(err)
	}
	same(t, built, read, []string{"timeouts", "operating guide", "node maintenance mode", "approvals expire", "cluster", "remove"})
	if !read.Current(ctx, cat, k) {
		t.Error("the index read is not current")
	}
	// Within, which needs a document's outline.
	g := catalog.Doc{Coll: k, Rel: "guide.md"}
	x, _ := built.Within("approvals", g, 0, 5)
	y, _ := read.Within("approvals", g, 0, 5)
	if len(x) != 1 || len(y) != 1 || x[0].Heading != y[0].Heading {
		t.Errorf("Within: built %v, read %v", x, y)
	}

	// The file is of the documents as they were: any change, and it is
	// not used.
	changes := map[string]func(){
		"a document changed, same size": func() {
			_ = os.WriteFile(filepath.Join(k.Path, "topics/nodes.md"), []byte("# Nodes\n\nPut a node into maintenance mood.\n\n## Removing a node\n\nRemove the node from the cluster.\n"), 0o644)
		},
		"a document added":   func() { _ = os.WriteFile(filepath.Join(k.Path, "new.md"), []byte("# New\n"), 0o644) },
		"a document removed": func() { _ = os.Remove(filepath.Join(k.Path, "empty.md")) },
		"list_items set":     func() { k.ListItems = true },
	}
	for name, change := range changes {
		cat, k = fileCollection(t)
		if err := WriteFile(ctx, cat, k, 1<<20); err != nil {
			t.Fatal(err)
		}
		change()
		if _, err := LoadFile(ctx, cat, k); err == nil || err == ErrNoFile {
			t.Errorf("%s: the file is used (%v)", name, err)
		}
	}
}

// A damaged file is refused, whatever the damage: no byte of it can be
// changed, and no end cut, without that.
func TestIndexFileDamaged(t *testing.T) {
	ctx := context.Background()
	cat, k := fileCollection(t)
	if err := WriteFile(ctx, cat, k, 1<<20); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(k.Path, FileName))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := decode(b, k); err != nil {
		t.Fatal(err)
	}
	for i := range b {
		c := append([]byte{}, b...)
		c[i] ^= 0x40
		if _, _, err := decode(c, k); err == nil {
			t.Fatalf("byte %d changed, and the file is read", i)
		}
		if _, _, err := decode(b[:i], k); err == nil {
			t.Fatalf("cut to %d bytes, and the file is read", i)
		}
	}
}

// FuzzDecode: no file makes the reader fail otherwise than with an
// error, also one whose checksum is right.
func FuzzDecode(f *testing.F) {
	cat, k := fileCollection(f)
	if err := WriteFile(context.Background(), cat, k, 1<<20); err != nil {
		f.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(k.Path, FileName))
	f.Add(b[len(magic) : len(b)-4])
	f.Add([]byte{})
	f.Fuzz(func(t *testing.T, body []byte) {
		file := append([]byte(magic), body...)
		file = appendCRC(file)
		ix, _, err := decode(file, k)
		if err != nil {
			return
		}
		ix.cat = cat
		_, _ = ix.Search("timeouts node guide", 5)
	})
}

// TestTokensVersion: what Tokens makes of a text is part of the index
// file's format. When this fails, Tokens has changed: set TokensVersion
// to the next number, and the sum here to the one reported.
func TestTokensVersion(t *testing.T) {
	text := `Configuring approvals: the agents' no_request_timeout, "instances" and policies; it is running,
		stopped or restarted. Über-größe naïve café 16.1 SLES-for-SAP x86_64 /etc/selinux/config --max-read
		addresses classes buses analysis status BOXES Tried trying dies the of and`
	sum := sha256.Sum256([]byte(strings.Join(Tokens(text), " ")))
	const want = "d62de0b5d1eb4e81 (version 1)"
	if got := fmt.Sprintf("%x (version %d)", sum[:8], TokensVersion); got != want {
		t.Errorf("Tokens: %s, want %s", got, want)
	}
}
