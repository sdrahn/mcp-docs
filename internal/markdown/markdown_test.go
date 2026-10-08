package markdown

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func scan(t *testing.T, doc string) *Outline {
	t.Helper()
	o, err := Scan(context.Background(), strings.NewReader(doc))
	if err != nil {
		t.Fatal(err)
	}
	return o
}

type brief struct {
	Level       int
	Title, ID   string
	Line, Lines int
	Bytes       int64
}

func briefs(o *Outline) []brief {
	var out []brief
	for _, h := range o.Headings {
		out = append(out, brief{h.Level, h.Title, h.ID, h.Line, h.Lines, h.Bytes})
	}
	return out
}

// mcp-gateway's outline_file test document: ATX headings with their
// sections; headings in fenced code, #tags, setext underlines and
// indented code are not headings.
func TestScanHeadings(t *testing.T) {
	doc := "# Title\n" + // 1
		"intro\n" + // 2
		"## One ##\n" + // 3
		"```sh\n" + // 4
		"# not a heading\n" + // 5
		"~~~\n" + // 6 (inside the backtick fence)
		"```\n" + // 7
		"#tag is not one\n" + // 8
		"   ### Deep\n" + // 9
		"text\n" + // 10
		"## Two\n" + // 11
		"Setext\n" + // 12
		"------\n" + // 13
		"    # indented code\n" + // 14
		"~~~~\n" + // 15
		"## in tildes\n" + // 16
		"~~~~\n" + // 17
		"end" // 18, no newline
	if len(doc) != 154 {
		t.Fatalf("test document is %d bytes", len(doc))
	}
	o := scan(t, doc)
	want := []brief{
		{1, "Title", "title", 1, 18, 154},
		{2, "One", "one", 3, 8, 73},
		{3, "Deep", "deep", 9, 2, 17},
		{2, "Two", "two", 11, 8, 67},
	}
	if got := briefs(o); !reflect.DeepEqual(got, want) {
		t.Errorf("headings:\n%v\nwant\n%v", got, want)
	}
	if o.Lines != 18 || o.Bytes != 154 {
		t.Errorf("lines %d bytes %d", o.Lines, o.Bytes)
	}
	if p := o.Path(2); !reflect.DeepEqual(p, []string{"Title", "One", "Deep"}) {
		t.Errorf("path: %v", p)
	}
	for line, want := range map[int]int{1: 0, 2: 0, 3: 1, 10: 2, 11: 3, 18: 3} {
		if got := o.At(line); got != want {
			t.Errorf("At(%d) = %d, want %d", line, got, want)
		}
	}
	if c := o.Children(0); !reflect.DeepEqual(c, []int{1, 3}) {
		t.Errorf("children: %v", c)
	}
	if !o.Within(2, 0) || o.Within(3, 1) {
		t.Error("Within")
	}
}

func TestScanNoHeadings(t *testing.T) {
	o := scan(t, "no headings\n#here\n")
	if len(o.Headings) != 0 || o.Lines != 2 || o.Bytes != 18 || o.At(1) != -1 {
		t.Errorf("%+v", o)
	}
	if o := scan(t, ""); o.Lines != 0 || len(o.Offsets) != 0 {
		t.Errorf("empty: %+v", o)
	}
}

// Ids are GitHub's anchors: punctuation removed, spaces to dashes,
// repeats numbered; explicit ids win.
func TestIDs(t *testing.T) {
	doc := strings.Join([]string{
		"# Getting started",
		"## Setup",
		"## Setup",
		"## Setup-1",
		"## Setup",
		"## 6.6 Policy lifecycle",
		"## `search_text` and *outline*",
		"## [Linked](http://x/y) title",
		"## Ünïcode Straße",
		"## C++ & Go: why?",
		"## Custom {#my-id}",
		`## <a id="anchor"></a>Anchored`,
		"## !!!",
		"## Two  spaces",
	}, "\n")
	var got []string
	for _, h := range scan(t, doc).Headings {
		got = append(got, h.ID+"|"+h.Title)
	}
	want := []string{
		"getting-started|Getting started",
		"setup|Setup",
		"setup-1|Setup",
		"setup-1-1|Setup-1",
		"setup-2|Setup",
		"66-policy-lifecycle|6.6 Policy lifecycle",
		"search_text-and-outline|`search_text` and *outline*",
		"linked-title|[Linked](http://x/y) title",
		"ünïcode-straße|Ünïcode Straße",
		"c--go-why|C++ & Go: why?",
		"my-id|Custom",
		"anchor|Anchored",
		"section|!!!",
		"two--spaces|Two  spaces",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ids:\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

// A front matter block is not content: a "---" in it is no rule, a "#"
// no heading; an unclosed one is text.
func TestFrontMatter(t *testing.T) {
	o := scan(t, "---\ntitle: x\n# comment\n---\n# Real\n")
	if o.FrontMatter != 4 || len(o.Headings) != 1 || o.Headings[0].Line != 5 || o.Lines != 5 {
		t.Errorf("%+v", o)
	}
	o = scan(t, "---\n# Not front matter\ntext\n")
	if o.FrontMatter != 0 || len(o.Headings) != 1 || o.Headings[0].Line != 2 || o.Headings[0].Lines != 2 {
		t.Errorf("unclosed: %+v", o)
	}
	o = scan(t, "text\n---\n# H\n---\n")
	if o.FrontMatter != 0 || len(o.Headings) != 1 {
		t.Errorf("not at the start: %+v", o)
	}
	long := "---\n" + strings.Repeat("x\n", maxFrontMatter+5) + "---\n# H\n"
	if o := scan(t, long); o.FrontMatter != 0 || len(o.Headings) != 1 || o.Headings[0].Line != maxFrontMatter+8 {
		t.Errorf("too long: %+v", o)
	}
}

// Every OffsetEvery-th line's offset is stored.
func TestOffsets(t *testing.T) {
	var b strings.Builder
	for i := 1; i <= 3*OffsetEvery+1; i++ {
		fmt.Fprintf(&b, "line %d\n", i)
	}
	doc := b.String()
	o := scan(t, doc)
	if len(o.Offsets) != 4 {
		t.Fatalf("offsets: %v", o.Offsets)
	}
	for k, off := range o.Offsets {
		if want := fmt.Sprintf("line %d\n", k*OffsetEvery+1); !strings.HasPrefix(doc[off:], want) {
			t.Errorf("offset %d: %q", k, doc[off:off+10])
		}
	}
}

func TestScanCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Scan(ctx, strings.NewReader("# x\n")); err == nil {
		t.Error("a cancelled scan succeeded")
	}
}
