package docbook

import (
	"flag"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "rewrite the golden files from the converter's output")

// Entities: parameter entities followed to their files, the first
// declaration of a name winning, references expanded recursively,
// markup kept, unresolved names reported.
func TestEntities(t *testing.T) {
	dir := t.TempDir()
	write := func(name, content string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("a.ent", `<!ENTITY % b SYSTEM "b.ent"> <!-- <!ENTITY commented "no"> -->
%b;
<!ENTITY x "first">
<!ENTITY prompt.root "<prompt role='root'># </prompt>">
<!ENTITY nested "&x; and &prompt.root;">`)
	write("b.ent", `<!ENTITY x "from b"> <!ENTITY y 'single'>`)
	src := []byte(`<?xml version="1.0"?>
<!DOCTYPE topic [ <!ENTITY % entities SYSTEM "a.ent"> %entities; ]>
<topic>&x; &y; &nested; &amp; &missing;</topic>`)
	ents, err := EntitiesOf(src, dir)
	if err != nil {
		t.Fatal(err)
	}
	if ents["x"] != "from b" || ents["y"] != "single" || ents["commented"] != "" {
		t.Errorf("entities: %v", ents)
	}
	out, unresolved := Expand(src, ents)
	if got := string(out); !strings.Contains(got, "<topic>from b single from b and <prompt role='root'># </prompt> &amp; &amp;missing;</topic>") ||
		strings.Contains(got, "DOCTYPE") {
		t.Errorf("expanded: %s", got)
	}
	if strings.Join(unresolved, ",") != "missing" {
		t.Errorf("unresolved: %v", unresolved)
	}
	if _, err := Parse(out); err != nil {
		t.Errorf("the expanded text does not parse: %v", err)
	}
}

// Profiling keeps an element whose attribute names a selected value,
// or that has no such attribute.
func TestProfile(t *testing.T) {
	root, err := Parse([]byte(`<r><p os="sles">a</p><p os="slmicro">b</p><p os="sles;sles4sap">c</p><p>d</p><p arch="s390x">e</p></r>`))
	if err != nil {
		t.Fatal(err)
	}
	Profile(root, map[string][]string{"os": {"sles"}})
	if got := plain(root); got != "acde" {
		t.Errorf("profiled: %q", got)
	}
}

func convert(t *testing.T) (string, *Report) {
	t.Helper()
	out := t.TempDir()
	rep, err := Convert(Options{Src: "testdata/doc-modular", DCs: []string{"DC-SLES-SELinux"}, Out: out, Name: "sles-16.0",
		Title: "SUSE Linux Enterprise Server 16.0", Description: "SELinux on SLES, converted from SUSE's sources.",
		SourceURL: "https://github.com/SUSE/doc-modular/blob/maintenance/SLE16.0/%s",
		SiteURL:   "https://documentation.suse.com/sles/16.0/html/%s/", Revision: "a9f7084"})
	if err != nil {
		t.Fatal(err)
	}
	return out, rep
}

// The converted documents are as reviewed: the golden files.
func TestGolden(t *testing.T) {
	out, _ := convert(t)
	for _, f := range []string{"topics/selinux-switching-modes.md", "topics/selinux-enable-disable.md",
		"articles/SLES-SELinux.md", "README.md"} {
		got, err := os.ReadFile(filepath.Join(out, f))
		if err != nil {
			t.Fatal(err)
		}
		golden := filepath.Join("testdata", "golden", f)
		if *update {
			if err := os.MkdirAll(filepath.Dir(golden), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(golden, got, 0o644); err != nil {
				t.Fatal(err)
			}
			continue
		}
		want, err := os.ReadFile(golden)
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != string(want) {
			t.Errorf("%s differs from %s (go test -update rewrites it):\n%s", f, golden, got)
		}
	}
}

var (
	anchor    = regexp.MustCompile(`\{#([^}]+)\}`)
	xmlID     = regexp.MustCompile(`xml:id="([^"]+)"`)
	leftovers = regexp.MustCompile(`&[A-Za-z_][\w.\-]*;`)
	mdLink    = regexp.MustCompile(`\]\(([^)#]*)#([^)]+)\)`)
)

// The whole article: nothing lost, every id an anchor, every link
// resolving, the product's text only.
func TestWholeArticle(t *testing.T) {
	out, rep := convert(t)
	if !rep.OK() || rep.Documents != 28 {
		t.Fatalf("report: %s", rep)
	}
	docs := map[string]string{}
	anchors := map[string]map[string]bool{}
	err := filepath.Walk(out, func(p string, fi os.FileInfo, err error) error {
		if err != nil || fi.IsDir() {
			return err
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(out, p)
		docs[rel] = string(b)
		anchors[rel] = map[string]bool{}
		for _, m := range anchor.FindAllStringSubmatch(string(b), -1) {
			anchors[rel][m[1]] = true
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	all := map[string]bool{}
	for _, a := range anchors {
		for id := range a {
			all[id] = true
		}
	}
	// Every xml:id of the converted topics is an anchor, or a link to
	// it leads to the section that holds it (an example, a list). Topics
	// the article does not use are not converted.
	topics, _ := filepath.Glob("testdata/doc-modular/*/*.xml")
	converted := 0
	for _, f := range topics {
		b, _ := os.ReadFile(f)
		ids := xmlID.FindAllStringSubmatch(string(b), -1)
		if strings.Contains(f, "/articles/") || len(ids) == 0 || docs["topics/"+ids[0][1]+".md"] == "" {
			continue
		}
		converted++
		for _, m := range ids {
			if !all[m[1]] && !strings.Contains(string(b), `<section xml:id="`+m[1]) && !strings.Contains(string(b), `<topic xml:id="`+m[1]) {
				continue // not a heading: links to it go to its section (checked below)
			}
			if !all[m[1]] {
				t.Errorf("%s: xml:id %s is no anchor", f, m[1])
			}
		}
	}
	if converted != 26 {
		t.Errorf("%d topics converted, want the article's 26", converted)
	}
	// xref to the list "Common options" (list-common-options): its title,
	// its section's anchor.
	if !strings.Contains(docs["topics/selinux-tools.md"], "[Common options](#sec-semanage-command)") {
		t.Errorf("a link to a list's id does not lead to its section")
	}
	for name, d := range docs {
		if l := leftovers.FindString(d); l != "" {
			t.Errorf("%s: entity left: %s", name, l)
		}
		// Every link to an anchor resolves.
		for _, m := range mdLink.FindAllStringSubmatch(d, -1) {
			target := name
			if m[1] != "" {
				target = filepath.Clean(filepath.Join(filepath.Dir(name), m[1]))
			}
			if !anchors[target][m[2]] {
				t.Errorf("%s: link to %s#%s does not resolve", name, m[1], m[2])
			}
		}
	}
	// PROFOS="sles": SLE Micro's and SAP's text is left out, the text for
	// both SLES and SAP kept.
	pk := docs["topics/selinux-packages.md"]
	if strings.Contains(pk, "transactional-update") || !strings.Contains(pk, "The SELinux man pages contain comprehensive") {
		t.Errorf("profiling, packages:\n%s", pk)
	}
	if strings.Contains(docs["topics/selinux-modes.md"], "patterns are installed") {
		t.Error("profiling: SAP's text in selinux-modes")
	}
	if strings.Contains(docs["topics/task-verification.md"], "sle-micro") {
		t.Error("profiling: SLE Micro's link in task-verification")
	}
}
