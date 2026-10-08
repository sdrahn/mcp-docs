package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func write(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// /usr then /etc: a file of the same name replaces, an empty one
// disables; broken files are reported and left out.
func TestLoad(t *testing.T) {
	usr, etc := t.TempDir(), t.TempDir()
	write(t, usr, "gateway.yaml", "title: Gateway\nroot: /usr/share/gw/docs\ndescription: >\n  The\n  gateway.\n"+
		"also: [/usr/share/doc/gw/CHANGELOG.md]\nexclude: [\"drafts/**\"]\ninstructions: |\n  Ask gateway-admin.\n")
	write(t, usr, "kit.yaml", "root: /usr/share/doc/kit\n")
	write(t, usr, "named.yml", "name: other\nroot: /x\n")
	write(t, usr, "renamed.yaml", "name: gone\nroot: /x\n")
	write(t, etc, "renamed.yaml", "")
	write(t, usr, "off.yaml", "root: /off\n")
	write(t, usr, "notes.txt", "ignored")
	write(t, etc, "kit.yaml", "root: /srv/kit-docs\ndescription: local\n")
	write(t, etc, "off.yaml", "# disabled\n")
	write(t, etc, "bad.yaml", "root: relative\n")
	write(t, etc, "typo.yaml", "root: /x\ndescripton: y\n")
	write(t, etc, "slash.yaml", "name: a/b\nroot: /x\n")

	colls, problems := Load([]string{usr, filepath.Join(usr, "missing"), etc})
	var got []string
	for _, c := range colls {
		got = append(got, c.Name+"="+c.Root+":"+c.Description)
	}
	if strings.Join(got, " ") != "gateway=/usr/share/gw/docs:The gateway. kit=/srv/kit-docs:local other=/x:" {
		t.Errorf("collections: %v", got)
	}
	gw := colls[0]
	if gw.Title != "Gateway" || gw.Instructions != "Ask gateway-admin." || gw.Also[0] != "/usr/share/doc/gw/CHANGELOG.md" ||
		gw.Exclude[0] != "drafts/**" || gw.File != filepath.Join(usr, "gateway.yaml") {
		t.Errorf("gateway: %+v", gw)
	}
	var msgs []string
	for _, p := range problems {
		msgs = append(msgs, p.Error())
	}
	all := strings.Join(msgs, "\n")
	for _, want := range []string{"bad.yaml: root \"relative\": give an absolute path", "typo.yaml", "descripton",
		"slash.yaml: name \"a/b\""} {
		if !strings.Contains(all, want) {
			t.Errorf("no problem %q in:\n%s", want, all)
		}
	}
	if len(problems) != 3 {
		t.Errorf("problems: %v", msgs)
	}
}

func TestCheck(t *testing.T) {
	for _, c := range []Collection{
		{Name: "x", Root: "/r", Also: []string{"rel.md"}},
		{Name: "x", Root: "/r", Include: []string{"[bad"}},
		{Name: "x", Root: "/r", Index: "../up.md"},
		{Name: "x", Root: "/r", Index: "/abs.md"},
		{Name: " x", Root: "/r"},
	} {
		if err := c.Check(); err == nil {
			t.Errorf("%+v: no error", c)
		}
	}
	c := Collection{Name: "x", Root: "/r/../s/", Index: "guide/README.md"}
	if err := c.Check(); err != nil || c.Root != "/s" {
		t.Errorf("%+v %v", c, err)
	}
}
