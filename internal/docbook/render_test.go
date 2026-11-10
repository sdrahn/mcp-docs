package docbook

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// render is the Markdown of a fragment's blocks, and the elements the
// renderer did not know.
func render(t *testing.T, xml string) (string, map[string]int) {
	t.Helper()
	root, err := Parse([]byte("<root>" + xml + "</root>"))
	if err != nil {
		t.Fatal(err)
	}
	r := &renderer{links: map[string]Target{}, unknown: map[string]int{}, broken: map[string]bool{}}
	return strings.TrimSuffix(join(r.blocks(root.Children, 1)), "\n"), r.unknown
}

func TestRenderBlocks(t *testing.T) {
	for _, c := range []struct{ name, xml, want string }{
		{"a paragraph", `<para>Text with <command>ls -l</command> and <emphasis>stress</emphasis>.</para>`,
			"Text with `ls -l` and *stress*."},
		// Metadata in a paragraph does not end it.
		{"indexterm in a paragraph", `<para>Before <indexterm><primary>x</primary></indexterm>after <remark>fix me</remark>end.</para>`,
			"Before after end."},
		{"remark in a title", `<formalpara><title>Name <remark>todo</remark></title><para>Body.</para></formalpara>`,
			"**Name**\n\nBody."},
		{"an image in the text", `<para>Click <inlinemediaobject><textobject><phrase>the gear icon</phrase></textobject></inlinemediaobject> now.</para>`,
			"Click *the gear icon* now."},
		{"an image without a description", `<para>Click <inlinemediaobject><imageobject><imagedata fileref="x.png"/></imageobject></inlinemediaobject> now.</para>`,
			"Click now."},
		// Text that starts like Markdown structure is text.
		{"a paragraph like a heading", `<para># of CPUs is the number.</para>`, `\# of CPUs is the number.`},
		{"a paragraph like a list item", `<para>- is the default.</para>`, `\- is the default.`},
		{"a paragraph like a numbered item", `<para>1. is the first.</para>`, `1\. is the first.`},
		{"a paragraph like a quote", `<para>> is the prompt.</para>`, `\> is the prompt.`},
		{"no heading, no escape", `<para>#hash and -dash and 1.5 stay.</para>`, "#hash and -dash and 1.5 stay."},
		{"a list", `<itemizedlist><listitem><para>One</para></listitem><listitem><para># two</para></listitem></itemizedlist>`,
			"- One\n\n- \\# two"},
		{"a procedure", `<procedure><step><para>First.</para></step><step><para>Second.</para><screen>ls</screen></step></procedure>`,
			"1. First.\n\n2. Second.\n\n   ```\n   ls\n   ```"},
		{"a note", `<note><title>Mind</title><para>The gap.</para></note>`, "> **Note: Mind**\n>\n> The gap."},
		{"a question and its answer", `<qandaset><qandaentry><question><para>Why?</para></question><answer><para>Because.</para></answer></qandaentry></qandaset>`,
			"- **Why?**\n\n  Because."},
		{"a footnote", `<para>Text<footnote><para>An aside.</para></footnote>.</para>`, "Text (An aside.)."},
		{"a table", `<informaltable><tgroup cols="2"><thead><row><entry>A</entry><entry>B</entry></row></thead>
			<tbody><row><entry>1</entry><entry>a | b</entry></row></tbody></tgroup></informaltable>`,
			"| A | B |\n| --- | --- |\n| 1 | a \\| b |"},
		// A cell that spans columns or rows keeps the later cells in
		// their columns.
		{"a cell over two columns", `<informaltable><tgroup cols="3"><colspec colname="c1"/><colspec colname="c2"/><colspec colname="c3"/>
			<thead><row><entry>A</entry><entry>B</entry><entry>C</entry></row></thead>
			<tbody><row><entry namest="c1" nameend="c2">wide</entry><entry>c</entry></row>
			<row><entry>a</entry><entry>b</entry><entry>c</entry></row></tbody></tgroup></informaltable>`,
			"| A | B | C |\n| --- | --- | --- |\n| wide |  | c |\n| a | b | c |"},
		{"a cell over two rows", `<informaltable><tgroup cols="3"><thead><row><entry>A</entry><entry>B</entry><entry>C</entry></row></thead>
			<tbody><row><entry morerows="1">tall</entry><entry>b1</entry><entry>c1</entry></row>
			<row><entry>b2</entry><entry>c2</entry></row>
			<row><entry>a3</entry><entry>b3</entry><entry>c3</entry></row></tbody></tgroup></informaltable>`,
			"| A | B | C |\n| --- | --- | --- |\n| tall | b1 | c1 |\n|  | b2 | c2 |\n| a3 | b3 | c3 |"},
		{"a last cell over two rows", `<informaltable><tgroup cols="2"><tbody><row><entry>a1</entry><entry morerows="1">tall</entry></row>
			<row><entry>a2</entry></row><row><entry>a3</entry><entry>b3</entry></row></tbody></tgroup></informaltable>`,
			"|  |  |\n| --- | --- |\n| a1 | tall |\n| a2 |  |\n| a3 | b3 |"},
		{"an HTML table with spans", `<informaltable><tr><th>A</th><th>B</th></tr><tr><td colspan="2">wide</td></tr><tr><td rowspan="2">tall</td><td>b</td></tr><tr><td>b2</td></tr></informaltable>`,
			"|  |  |\n| --- | --- |\n| A | B |\n| wide |  |\n| tall | b |\n|  | b2 |"},
	} {
		got, unknown := render(t, c.xml)
		if got != c.want {
			t.Errorf("%s:\n got %q\nwant %q", c.name, got, c.want)
		}
		if len(unknown) > 0 {
			t.Errorf("%s: unknown elements %v", c.name, unknown)
		}
	}
}

// A table in a cell stays in the cell: its rows are not the outer
// table's.
func TestRenderNestedTable(t *testing.T) {
	got, _ := render(t, `<informaltable><tgroup cols="2"><tbody>
		<row><entry>a</entry><entry><informaltable><tgroup cols="1"><tbody><row><entry>inner</entry></row></tbody></tgroup></informaltable></entry></row>
		<row><entry>c</entry><entry>d</entry></row></tbody></tgroup></informaltable>`)
	lines := strings.Split(got, "\n")
	if len(lines) != 4 || !strings.HasPrefix(lines[2], "| a | ") || !strings.Contains(lines[2], "inner") || lines[3] != "| c | d |" {
		t.Errorf("%q", got)
	}
}

func TestExpandLeavesCDATA(t *testing.T) {
	ents := Entities{"known": "KNOWN"}
	out, unresolved := Expand([]byte(`<p>&known; <![CDATA[echo &known; &nbsp;]]> <!-- &other; --> &missing;</p>`), ents)
	want := `<p>KNOWN <![CDATA[echo &known; &nbsp;]]> <!-- &other; --> &amp;missing;</p>`
	if string(out) != want || len(unresolved) != 1 || unresolved[0] != "missing" {
		t.Errorf("%s\nunresolved %v", out, unresolved)
	}
}

// Entities of entities do not grow a document without bound.
func TestExpandBounded(t *testing.T) {
	ents := Entities{"a0": strings.Repeat("x", 1<<10)}
	name := "a0"
	for i := 1; i <= 9; i++ {
		next := "a" + string(rune('0'+i))
		ents[next] = strings.Repeat("&"+name+";", 64)
		name = next
	}
	out, unresolved := Expand([]byte("<p>&"+name+";</p>"), ents)
	if len(out) > 2*maxExpanded+(1<<20)*64 || len(unresolved) == 0 || !strings.Contains(strings.Join(unresolved, " "), "expand to more than") {
		t.Errorf("%d bytes, unresolved %v", len(out), unresolved)
	}
}

func TestURLOf(t *testing.T) {
	if got := urlOf("https://x/a%20b/%s/", "NAME"); got != "https://x/a%20b/NAME/" {
		t.Error(got)
	}
}

// What a source names is not trusted: an xml:id is no path, and an
// include, a resource or a file of entities is below the sources.
func TestSourcesAreConfined(t *testing.T) {
	base := t.TempDir()
	write := func(rel, text string) {
		p := filepath.Join(base, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	const head = `<?xml version="1.0"?>`
	asm := func(href string) string {
		return head + `<assembly xmlns="http://docbook.org/ns/docbook"><resources><resource xml:id="r" href="` + href + `"/></resources>
			<structure xml:id="s"><merge><title>T</title></merge><module resourceref="r" renderas="section"/></structure></assembly>`
	}
	topic := func(id, body string) string {
		return head + `<topic xmlns="http://docbook.org/ns/docbook" xmlns:xi="http://www.w3.org/2001/XInclude" xml:id="` + id +
			`"><title>Topic</title>` + body + `</topic>`
	}
	write("secret.txt", "SECRET")
	write("secret.ent", `<!ENTITY leaked "SECRET">`)
	for name, c := range map[string]struct{ href, topic, want string }{
		"an id that is a path":           {"t.xml", topic("../../escaped", "<para>x</para>"), "cannot name its document"},
		"an id with a slash":             {"t.xml", topic("a/b", "<para>x</para>"), "cannot name its document"},
		"an include from outside":        {"t.xml", topic("t", `<screen><xi:include href="../secret.txt" parse="text"/></screen>`), "outside the sources"},
		"a resource from outside":        {"../outside.xml", topic("t", "<para>x</para>"), "outside the sources"},
		"entities from outside":          {"t.xml", `<!DOCTYPE topic [<!ENTITY % e SYSTEM "../secret.ent"> %e;]>` + topic("t", "<para>&leaked;</para>"), "outside the sources"},
		"a symbolic link to the outside": {"t.xml", topic("t", `<screen><xi:include href="link.txt" parse="text"/></screen>`), "link.txt"},
	} {
		src := filepath.Join(base, "src-"+strings.ReplaceAll(name, " ", "-"))
		rel := func(p string) string { r, _ := filepath.Rel(base, filepath.Join(src, p)); return r }
		write(rel("DC-a"), "MAIN=a.asm.xml\n")
		write(rel("a.asm.xml"), asm(c.href))
		write(rel("t.xml"), c.topic)
		write(rel("../outside.xml"), topic("t", "<para>x</para>"))
		_ = os.Symlink(filepath.Join(base, "secret.txt"), filepath.Join(src, "link.txt"))
		out := filepath.Join(base, "out-"+strings.ReplaceAll(name, " ", "-"), "deep", "out")
		_, err := Convert(Options{Src: src, DCs: []string{"DC-a"}, Out: out})
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v, want an error with %q", name, err, c.want)
		}
		_ = filepath.WalkDir(filepath.Join(base, "out-"+strings.ReplaceAll(name, " ", "-")), func(p string, d os.DirEntry, err error) error {
			if err == nil && !d.IsDir() {
				if b, _ := os.ReadFile(p); strings.Contains(string(b), "SECRET") {
					t.Errorf("%s: %s holds what is outside the sources", name, p)
				}
			}
			return nil
		})
	}
	if _, err := os.Stat(filepath.Join(base, "escaped.md")); err == nil {
		t.Error("a document was written outside the output directory")
	}
}
