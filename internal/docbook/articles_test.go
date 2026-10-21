package docbook

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Several articles in one collection: a topic that two articles profile
// differently is one document per variant, named by its article, and so
// is a topic that links to them; one they profile alike is shared.
// STRUCTID picks the structure; groups, XIncludes, callouts and
// glossaries are converted; sources are pages of the site.
func TestArticles(t *testing.T) {
	src := t.TempDir()
	write := func(name, content string) {
		t.Helper()
		p := filepath.Join(src, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	const doctype = `<!DOCTYPE topic [ <!ENTITY % e SYSTEM "../common/e.ent"> %e; ]>`
	write("common/e.ent", `<!ENTITY prod "SLES">`)
	write("DC-X", "MAIN=\"a.asm.xml\"\nSRC_DIR=\"articles\"\nPROFARCH=\"x86_64\"\nSTRUCTID=\"second\"\n")
	write("DC-Y", "MAIN=\"a.asm.xml\"\nSRC_DIR=\"articles\"\nSTRUCTID=\"first\"\n")
	write("DC-Z", "MAIN=\"a.asm.xml\"\nSRC_DIR=\"articles\"\nPROFARCH=\"zseries\"\nSTRUCTID=\"second\"\n")
	write("articles/a.asm.xml", doctype+`<assembly>
<resources>
  <resource xml:id="_install" href="../tasks/install.xml"/>
  <resource xml:id="_links" href="../tasks/links.xml"/>
  <resource xml:id="_about" href="../concepts/about.xml"/>
  <resource xml:id="_terms" href="../references/terms.xml"/>
  <resource xml:id="_legal" href="../common/legal.xml"/>
</resources>
<structure renderas="article" xml:id="first"><merge><title>Not this one</title></merge>
  <module resourceref="_about" renderas="section"/>
</structure>
<structure renderas="article" xml:id="second">
  <merge><title>Installing &prod;</title><meta name="description">How to install</meta>
    <meta name="task"><phrase>Installation</phrase></meta>
    <revhistory><revision><date>2026-09-18</date></revision><revision><date>2026-11-04</date></revision></revhistory></merge>
  <module resourceref="_about" renderas="section"/>
  <module renderas="section" xml:id="grp">
    <merge><title>Doing it</title><abstract><para>The steps.</para></abstract></merge>
    <module resourceref="_install" renderas="section"/>
    <module resourceref="_links" renderas="section"/>
  </module>
  <module resourceref="_terms"><output renderas="glossary"/></module>
  <module resourceref="_legal" renderas="section"/>
</structure>
</assembly>`)
	write("common/legal.xml", doctype+`<topic xml:id="legal"><title>Legal Notice</title><para>No warranty.</para></topic>`)
	write("concepts/about.xml", doctype+`<topic xml:id="about" role="concept"><title>About &prod;</title>
<para>The same on every architecture.</para></topic>`)
	write("tasks/install.xml", doctype+`<topic xml:id="install" role="task"><title>Install</title>
<para arch="x86_64">Boot from the DVD.</para><para arch="zseries">IPL the reader.</para>
<screen>zypper in foo <co xml:id="co-in"/>
reboot <co xml:id="co-reboot"/></screen>
<calloutlist><callout arearefs="co-in"><para>Installs foo.</para></callout><callout arearefs="co-reboot"><para>Restarts.</para></callout></calloutlist>
<xi:include xmlns:xi="http://www.w3.org/2001/XInclude" href="../snippets/note.xml"/>
<para>See <xref linkend="grp"/>.</para></topic>`)
	write("snippets/note.xml", doctype+`<note><title>Included</title><para>From a snippet about &prod;.</para></note>`)
	write("tasks/links.xml", doctype+`<topic xml:id="links" role="task"><title>Next</title>
<para>Read <xref linkend="install"/> and <xref linkend="gl-dvd"/>.</para></topic>`)
	write("references/terms.xml", doctype+`<topic xml:id="terms" role="reference"><title>Glossary</title>
<glossdiv xml:id="gl-media"><title>Media</title>
<glossentry xml:id="gl-dvd"><glossterm>DVD</glossterm><glossdef><para>A disc.</para></glossdef></glossentry>
<glossentry xml:id="gl-disc"><glossterm>Disc</glossterm><glosssee otherterm="gl-dvd"/></glossentry>
</glossdiv></topic>`)

	out := t.TempDir()
	rep, err := Convert(Options{Src: src, DCs: []string{"DC-X", "DC-Y", "DC-Z"}, Out: out, Name: "t", Title: "T", Description: "D",
		SiteURL: "https://documentation.suse.com/sles/16.0/html/%s/", MaxSection: 200})
	if err != nil {
		t.Fatal(err)
	}
	if !rep.OK() || rep.Articles != 3 {
		t.Fatalf("report: %s", rep)
	}
	read := func(name string) string {
		t.Helper()
		b, err := os.ReadFile(filepath.Join(out, name))
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	files, _ := filepath.Glob(filepath.Join(out, "topics", "*.md"))
	var names []string
	for _, f := range files {
		names = append(names, filepath.Base(f))
	}
	if got := strings.Join(names, " "); got != "about.md install--X.md install--Z.md legal.md links--X.md links--Z.md terms.md" {
		t.Errorf("topics: %s", got)
	}
	x, z := read("topics/install--X.md"), read("topics/install--Z.md")
	if !strings.Contains(x, "Boot from the DVD.") || strings.Contains(x, "IPL") || !strings.Contains(z, "IPL the reader.") {
		t.Errorf("profiled variants:\n%s\n%s", x, z)
	}
	for _, want := range []string{
		`source: "https://documentation.suse.com/sles/16.0/html/X/index.html#install"`,
		"zypper in foo (1)\nreboot (2)\n```",
		"(1) Installs foo.\n\n(2) Restarts.",
		"> **Note: Included**\n>\n> From a snippet about SLES.",
		"See [Doing it](../articles/X.md#second).",
	} {
		if !strings.Contains(x, want) {
			t.Errorf("install--X.md lacks %q:\n%s", want, x)
		}
	}
	if l := read("topics/links--Z.md"); !strings.Contains(l, "Read [Install](install--Z.md#install) and [DVD](terms.md#gl-media).") {
		t.Errorf("links--Z.md:\n%s", l)
	}
	terms := read("topics/terms.md")
	for _, want := range []string{
		`source: "https://documentation.suse.com/sles/16.0/html/X/terms.html"`,
		"## Media {#gl-media}",
		"- **DVD**\n\n  A disc.",
		"- **Disc**\n\n  See [DVD](#gl-media)",
	} {
		if !strings.Contains(terms, want) {
			t.Errorf("terms.md lacks %q:\n%s", want, terms)
		}
	}
	art := read("articles/Z.md")
	for _, want := range []string{
		"\nrank: false\n",
		// The latest revision is the first listed, a later date below it
		// a typo.
		"\ndate: \"2026-09-18\"\n",
		"# Installing SLES {#second}\n\nLast revised 2026-09-18.\n",
		"- [About SLES](../topics/about.md)\n- **Doing it**: The steps.\n  - [Install](../topics/install--Z.md)\n  - [Next](../topics/links--Z.md)\n- [Glossary](../topics/terms.md)",
	} {
		if !strings.Contains(art, want) {
			t.Errorf("articles/Z.md lacks %q:\n%s", want, art)
		}
	}
	if y := read("articles/Y.md"); strings.Contains(y, "date:") || strings.Contains(y, "Last revised") {
		t.Errorf("an article without revisions has a date:\n%s", y)
	}
	// The index: titles by the articles' first task, without
	// descriptions.
	if readme := read("README.md"); !strings.Contains(readme, "## Articles\n\n### Installation\n\n"+
		"- [Installing SLES](articles/X.md)\n- [Installing SLES](articles/Z.md)\n\n### Other\n\n- [Not this one](articles/Y.md)\n") {
		t.Errorf("README.md:\n%s", readme)
	}
	// Legal notices are out of the ranking; nothing else is.
	if !strings.Contains(read("topics/legal.md"), "\nrank: false\n") || strings.Contains(read("topics/about.md"), "rank:") {
		t.Errorf("rank: %s", read("topics/legal.md"))
	}
	// Sections over MaxSection are reported, by their own text.
	if strings.Join(rep.Large, " ") != "topics/install--X.md#install (0.2 KiB) topics/install--Z.md#install (0.2 KiB)" ||
		!strings.Contains(rep.String(), "sections over 0.2 KiB of their own text: topics/install--X.md#install") {
		t.Errorf("large: %q\n%s", rep.Large, rep)
	}
}
