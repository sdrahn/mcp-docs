package asciidoc

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// render is a page's Markdown, without a component around it: links
// lead to "x" (pageLinks).
func render(t *testing.T, adoc string, attrs map[string]string) string {
	t.Helper()
	if attrs == nil {
		attrs = map[string]string{}
	}
	pre := &preprocessor{attrs: attrs, include: func(target, from string) ([]string, string, error) {
		if target == "partial$note.adoc" {
			return []string{"Included {who}.", "", "== Included section"}, "note.adoc", nil
		}
		return nil, "", os.ErrNotExist
	}}
	lines := pre.run(strings.Split(adoc, "\n"), "page.adoc", 0)
	r := &renderer{in: &inliner{links: pageLinks{}, page: "page.adoc", anchors: map[string]string{"_known": "Known section"},
		broken: map[string]bool{}}, ids: map[string]int{}, unknown: map[string]int{}, anchors: map[string]string{}}
	return strings.TrimSuffix(join(r.blocks(lines)), "\n")
}

type pageLinks struct{}

func (pageLinks) xref(target, from string) (string, string, bool) {
	page, fragment, _ := strings.Cut(target, "#")
	if strings.HasPrefix(page, "missing") {
		return "", "", false
	}
	href := strings.TrimSuffix(page, ".adoc") + ".md"
	if fragment != "" {
		href += "#" + fragment
	}
	return href, "Title of " + page, true
}

func TestBlocks(t *testing.T) {
	for _, c := range []struct{ name, adoc, want string }{
		{"title and sections", "= Page title\n\n== First section\n\ntext\n\n=== Sub: a/b (x)\n\n[#given]\n== Named\n\n## As Markdown has it",
			"# Page title\n\n## First section {#_first_section}\n\ntext\n\n### Sub: a/b (x) {#_sub_ab_x}\n\n## Named {#given}\n\n## As Markdown has it {#_as_markdown_has_it}"},
		{"two sections of one title", "== Same\n\n== Same", "## Same {#_same}\n\n## Same {#_same_2}"},
		{"a paragraph of lines", "one\ntwo +\nthree", "one two<br>three"},
		{"inline markup", "*bold* and _italic_, `code` and `+{literal}+`, **b**old, a_b_c, 2 * 3 * 4.",
			"**bold** and *italic*, `code` and `{literal}`, **b**old, a_b_c, 2 \\* 3 \\* 4."},
		{"links", "See https://example.com/a_b[the page^], https://example.com/bare, link:https://x.example[], xref:other/page.adoc#frag[there] and xref:other/page.adoc[].",
			"See [the page](https://example.com/a_b), <https://example.com/bare>, <https://x.example>, [there](other/page.md#frag) and [Title of other/page.adoc](other/page.md)."},
		{"a link that leads nowhere is its text", "See xref:missing.adoc[the lost page] and xref:[no target].", "See the lost page and no target."},
		{"a section of this page", "See <<_known>> and <<_known,that one>>.", "See [Known section](#_known) and [that one](#_known)."},
		{"keys, buttons, menus, footnotes", "Press kbd:[Ctrl+C], click btn:[OK], menu:File[Save As], donefootnote:[A note.].",
			"Press `Ctrl+C`, click **OK**, **File > Save As**, done (A note.)."},
		{"an image in the text", "Click image:gear.png[the gear icon] now, or image:x.png[].", "Click *the gear icon* now, or ."},
		{"a listing", "[,shell]\n----\nls -l *\n{not} an attribute\n----", "```shell\nls -l *\n{not} an attribute\n```"},
		{"a listing with a title, and a literal", ".Example file\n[source,yaml]\n----\na: 1\n----\n\n....\nplain\n....", "**Example file**\n\n```yaml\na: 1\n```\n\n```\nplain\n```"},
		{"a fenced block", "```json\n{}\n```", "```json\n{}\n```"},
		{"an indented paragraph", " $ run this", "```\n$ run this\n```"},
		{"an admonition paragraph", "NOTE: Mind *this*.", "> **Note**\n>\n> Mind **this**."},
		{"an admonition block", ".Read first\n[WARNING]\n====\nOne.\n\n* a\n* b\n====", "> **Warning: Read first**\n>\n> One.\n>\n> - a\n> - b"},
		{"a collapsible block", ".Details\n[%collapsible]\n====\nHidden text.\n====", "**Details**\n\nHidden text."},
		{"an example", ".A case\n====\nText.\n====", "**Example: A case**\n\nText."},
		{"a passthrough is left out", "before\n\n++++\n<div>html</div>\n++++\n\nafter", "before\n\nafter"},
		{"lists", "* one\n* two\n** nested\n* three\n\ntext\n\n. first\n. second", "- one\n- two\n  - nested\n- three\n\ntext\n\n1. first\n2. second"},
		// As AsciiDoc has it: a list of another kind right after an item is that item's.
		{"a list below an item", "* one\n\n. first\n. second", "- one\n  1. first\n  2. second"},
		{"a list with attached blocks", ". Step one\n+\n----\ncmd\n----\n. Step two\n+\nMore text.\n+\nNOTE: Careful.\n. Step three",
			"1. Step one\n\n   ```\n   cmd\n   ```\n\n2. Step two\n\n   More text.\n\n   > **Note**\n   >\n   > Careful.\n\n3. Step three"},
		{"a list ends at a block", "* item\n[NOTE]\n====\nApart.\n====", "- item\n\n> **Note**\n>\n> Apart."},
		{"a description list", "Term one:: Its text.\nTerm two::\nText below.", "- **Term one**: Its text.\n- **Term two**: Text below."},
		{"tabs", "[tabs]\n======\nLinux::\n+\n--\nUse `apt`.\n\n* a\n--\nmacOS:: Use brew.\n======\n\nAfter.",
			"**Linux**\n\nUse `apt`.\n\n- a\n\n**macOS**\n\nUse brew.\n\nAfter."},
		{"a table", "|===\n| Name | Value\n\n| a\n| 1\n\n| b | x \\| y\n|===", "| Name | Value |\n| --- | --- |\n| a | 1 |\n| b | x \\| y |"},
		{"a table without a header", "[cols=\"1,1\"]\n|===\n| a | 1\n| b | 2\n|===", "|  |  |\n| --- | --- |\n| a | 1 |\n| b | 2 |"},
		{"a table with spans", "[cols=3,options=header]\n|===\n| A | B | C\n2+| wide | c\n.2+| tall | b1 | c1\n| b2 | c2\n|===",
			"| A | B | C |\n| --- | --- | --- |\n| wide |  | c |\n| tall | b1 | c1 |\n|  | b2 | c2 |"},
		{"cells with blocks", "[cols=\"1,1a\"]\n|===\n| Key | Notes\n\n| k\na| First.\n\n* x\n* y\n|===", "| Key | Notes |\n| --- | --- |\n| k | First. - x - y |"},
		{"a block image", ".The layout\nimage::layout.png[Three nodes in a row]\n\nimage::plain.png[]", "*Figure: The layout. Three nodes in a row*"},
		{"text that starts like Markdown", "> quoted in Markdown, text here\n\n+ so is this\n\n1) and this", "\\> quoted in Markdown, text here\n\n\\+ so is this\n\n1\\) and this"},
	} {
		if got := render(t, c.adoc, nil); got != c.want {
			t.Errorf("%s:\n got %q\nwant %q", c.name, got, c.want)
		}
	}
}

func TestPreprocessor(t *testing.T) {
	attrs := map[string]string{"build-type": "product", "who": "the reader", "name": "Product@"}
	for _, c := range []struct{ name, adoc, want string }{
		{"attributes", ":local: here\nSet {local}, given {who}, unknown {other}, escaped \\{who}.", "Set here, given the reader, unknown {other}, escaped {who}."},
		{"an attribute unset", ":!who:\nNow {who}.", "Now {who}."},
		{"ifeval", "ifeval::[\"{build-type}\" == \"product\"]\nfor the product\nendif::[]\nifeval::[\"{build-type}\" == \"community\"]\nfor the community\nendif::[]\nfor all",
			"for the product for all"},
		{"ifdef and ifndef", "ifdef::who[]\nset\nendif::[]\nifndef::who[]\nnot set\nendif::[]\nifdef::nobody,who[one of them]\nifdef::nobody+who[both]", "set one of them"},
		{"nested conditionals", "ifdef::nobody[]\nifdef::who[]\ninner\nendif::[]\nouter\nendif::[]\nafter", "after"},
		{"comments", "// a comment\ntext\n////\na block\nof comments\n////\nmore", "text more"},
		{"an include", "Before.\n\ninclude::partial$note.adoc[]\n\ninclude::partial$note.adoc[leveloffset=+1]",
			"Before.\n\nIncluded the reader.\n\n## Included section {#_included_section}\n\nIncluded the reader.\n\n### Included section {#_included_section_2}"},
		{"a listing is as it is, conditionals apart", "----\n:not: an entry\n// no comment\n{who}\nifdef::nobody[]\nleft out\nendif::[]\n----", "```\n:not: an entry\n// no comment\n{who}\n```"},
		{"a listing with attributes", "[subs=\"attributes\"]\n----\nhello {who}\n----", "```\nhello the reader\n```"},
	} {
		a := map[string]string{}
		for k, v := range attrs {
			a[k] = v
		}
		if got := render(t, c.adoc, a); got != c.want {
			t.Errorf("%s:\n got %q\nwant %q", c.name, got, c.want)
		}
	}
	// What cannot be included is a problem, not a silence.
	pre := &preprocessor{attrs: map[string]string{}, include: func(string, string) ([]string, string, error) { return nil, "", os.ErrNotExist }}
	pre.run([]string{"include::partial$gone.adoc[]"}, "page.adoc", 0)
	if len(pre.problems) != 1 || !strings.Contains(pre.problems[0], "gone.adoc") {
		t.Errorf("problems: %v", pre.problems)
	}
}

// A component as Antora has it, converted: pages, a shared partial, a
// navigation, attributes of the site, the component and the command.
func TestConvert(t *testing.T) {
	src, out := t.TempDir(), t.TempDir()
	files := map[string]string{
		"global.yml":                  "product-name: The Product@\nunused: true\n",
		"docs/v1/antora.yml":          "name: prod\ntitle: Product\nversion: v1\nnav:\n- modules/en/nav.adoc\n- modules/de/nav.adoc\nasciidoc:\n  attributes:\n    release: \"1.2\"\n",
		"shared/antora.yml":           "name: shared\nversion: latest\n",
		"docs/v1/modules/en/nav.adoc": "* Guides\n** xref:install/install.adoc[]\n*** xref:install/requirements.adoc[Requirements]\nifeval::[\"{build-type}\" == \"community\"]\n** xref:community-only.adoc[]\nendif::[]\n* xref:README.adoc[]\n",
		"docs/v1/modules/de/nav.adoc": "* xref:install/install.adoc[]\n",
		"docs/v1/modules/en/pages/install/install.adoc": "= Installing {product-name}\n:description: How to install it.\n:revdate: 2026-05-04\n\n" +
			"Release {release}. See xref:install/requirements.adoc[], xref:install/requirements.adoc#_memory[memory], " +
			"xref:install/requirements.adoc#disk-table[the disk table], xref:/install/requirements.adoc#_gone[a lost section] and xref:requirements.adoc[beside].\n\n" +
			"include::shared:ROOT:partial$en/warning.adoc[]\n\n== Steps\n\n. Run it.\n",
		"docs/v1/modules/en/pages/install/requirements.adoc": "= Requirements\n\n== Memory\n\n4 GB.\n\n[#storage]\n== Disk space\n\n[#disk-table]\n|===\n| Size\n\n| 10 GB\n|===\n",
		"docs/v1/modules/en/pages/community-only.adoc":       "= Not published\n",
		"docs/v1/modules/en/pages/README.adoc":               "= Read me\n\nA page of the name the index has.\n",
		"docs/v1/modules/de/pages/install/install.adoc":      "= Installieren\n",
		"shared/modules/ROOT/partials/en/warning.adoc":       "WARNING: Back up first, {product-name} users.\n",
	}
	for name, text := range files {
		p := filepath.Join(src, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	rep, err := Convert(Options{Src: src, Start: "docs/v1", Shared: []string{"shared"}, AttributeFiles: []string{"global.yml"},
		Attributes: map[string]string{"build-type": "product"}, NavOnly: true, Out: out, Title: "Product 1", Description: "Its documentation.",
		SiteURL: "https://docs.example/prod/v1/en/%s.html", SourceURL: "https://git.example/blob/abc/%s", Revision: "abc",
		Product: "Product", Version: "1", License: "Licensed freely."})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Pages != 3 || !rep.OK() || len(rep.Broken) != 1 || !strings.Contains(rep.Broken[0], "requirements#_gone") {
		t.Errorf("report: %s", rep)
	}
	read := func(name string) string {
		b, err := os.ReadFile(filepath.Join(out, name))
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	install := read("install/install.md")
	for _, want := range []string{
		"title: \"Installing The Product\"", "description: \"How to install it.\"", "product: \"Product\"", "version: \"1\"", "date: \"2026-05-04\"",
		"source: \"https://docs.example/prod/v1/en/install/install.html\"",
		"# Installing The Product\n",
		"Release 1.2. See [Requirements](requirements.md), [memory](requirements.md#_memory), [the disk table](requirements.md#storage), " +
			"[a lost section](requirements.md) and [beside](requirements.md).",
		"> **Warning**\n>\n> Back up first, The Product users.",
		"## Steps {#_steps}\n\n1. Run it.",
	} {
		if !strings.Contains(install, want) {
			t.Errorf("install.md has no %q:\n%s", want, install)
		}
	}
	if _, err := os.Stat(filepath.Join(out, "community-only.md")); err == nil {
		t.Error("a page the navigation does not list was converted")
	}
	// The collection's index, and the page of its name beside it.
	index := read("README.md")
	for _, want := range []string{"title: \"Product 1\"", "rank: false", "- Guides\n  - [Installing The Product](install/install.md) (and 1 below)\n- [Read me](README-page.md)",
		"https://git.example/blob/abc/docs/v1, revision abc", "Licensed freely."} {
		if !strings.Contains(index, want) {
			t.Errorf("README.md has no %q:\n%s", want, index)
		}
	}
	if !strings.Contains(read("README-page.md"), "# Read me") {
		t.Error("the page README is not README-page.md")
	}

	// A source that names what is outside the sources is not read.
	if err := os.WriteFile(filepath.Join(filepath.Dir(src), "secret.adoc"), []byte("SECRET\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Remove(filepath.Join(filepath.Dir(src), "secret.adoc")) }()
	bad := files["docs/v1/modules/en/pages/install/requirements.adoc"] + "\ninclude::../../../../../../secret.adoc[]\n"
	if err := os.WriteFile(filepath.Join(src, "docs/v1/modules/en/pages/install/requirements.adoc"), []byte(bad), 0o644); err != nil {
		t.Fatal(err)
	}
	rep, err = Convert(Options{Src: src, Start: "docs/v1", Shared: []string{"shared"}, AttributeFiles: []string{"global.yml"}, NavOnly: true, Out: out})
	if err != nil {
		t.Fatal(err)
	}
	if rep.OK() || len(rep.Problems) != 1 || strings.Contains(read("install/requirements.md"), "SECRET") {
		t.Errorf("an include from outside the sources: %s", rep)
	}
}
