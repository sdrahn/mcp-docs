package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sdrahn/mcp-docs/internal/catalog"
)

func TestParseCollections(t *testing.T) {
	a, b := t.TempDir(), t.TempDir()
	cs, err := parseCollections([]string{a, a}, []string{"kit=" + b + ":Kit's docs: all of them"})
	if err != nil {
		t.Fatal(err)
	}
	if len(cs) != 3 || cs[0].Name != "kit" || cs[0].Path != b || cs[0].Description != "Kit's docs: all of them" ||
		cs[1].Name != filepath.Base(a) || cs[2].Name != filepath.Base(a)+"-2" {
		t.Errorf("%+v %+v %+v", cs[0], cs[1], cs[2])
	}
	for _, bad := range [][2]string{{"", "noequals"}, {"", "a/b=" + a}, {"", "x=" + a + "/missing"}, {a + "/missing", ""}} {
		var roots, colls []string
		if bad[0] != "" {
			roots = []string{bad[0]}
		}
		if bad[1] != "" {
			colls = []string{bad[1]}
		}
		if _, err := parseCollections(roots, colls); err == nil {
			t.Errorf("%v: no error", bad)
		}
	}
	if _, err := parseCollections(nil, []string{"x=" + a, "x=" + b}); err == nil || !strings.Contains(err.Error(), "twice") {
		t.Errorf("duplicate: %v", err)
	}
}

// Collection files, then the command line, which replaces a file's
// collection of the same name; a file whose root is missing is left out.
func TestLoadCollections(t *testing.T) {
	dir, a, b := t.TempDir(), t.TempDir(), t.TempDir()
	for name, content := range map[string]string{
		"one.yaml":     "title: One\nroot: " + a + "\ndescription: first\nindex: guide.md\n",
		"two.yaml":     "root: " + a + "\n",
		"missing.yaml": "root: " + a + "/missing\n",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cs, problems, err := loadCollections([]string{dir}, nil, []string{"two=" + b}, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(cs) != 2 || cs[0].Name != "one" || cs[0].Title != "One" || cs[0].IndexName != "guide.md" ||
		cs[1].Name != "two" || cs[1].Path != b {
		t.Errorf("%+v %+v", cs[0], cs[1])
	}
	if len(problems) != 1 || !strings.Contains(problems[0].Error(), "missing.yaml: root") {
		t.Errorf("problems: %v", problems)
	}
}

// The program is executed again under Landlock and reads the collection
// files there: their directories must be readable, wherever they are.
func TestLandlockRulesHaveConfigDirs(t *testing.T) {
	k := &catalog.Collection{Name: "k", Path: "/srv/docs", Also: []string{"/opt/x/CHANGELOG.md"}}
	conf := t.TempDir()
	if err := os.Mkdir(filepath.Join(conf, "collections.d"), 0o755); err != nil {
		t.Fatal(err)
	}
	wd, _ := os.Getwd()
	rel, err := filepath.Rel(wd, filepath.Join(conf, "collections.d"))
	if err != nil {
		t.Fatal(err)
	}
	// A relative directory is made absolute; a missing one is left out.
	r := landlockRules([]*catalog.Collection{k}, []string{rel, filepath.Join(conf, "missing")}, -1)
	want := []string{"/srv/docs", "/opt/x/CHANGELOG.md", filepath.Join(conf, "collections.d")}
	if strings.Join(r.Read, " ") != strings.Join(want, " ") || r.Write != nil {
		t.Errorf("rules %+v, want read %v", r, want)
	}
	// TCP: none on stdin/stdout; over HTTP, binding its port only; no
	// connection either way; a port the kernel chooses cannot be named.
	if r.TCPBind == nil || len(r.TCPBind) != 0 || r.TCPConnect == nil || len(r.TCPConnect) != 0 {
		t.Errorf("stdio TCP: %+v", r)
	}
	if r := landlockRules(nil, nil, 8080); len(r.TCPBind) != 1 || r.TCPBind[0] != 8080 || len(r.TCPConnect) != 0 || r.TCPConnect == nil {
		t.Errorf("HTTP TCP: %+v", r)
	}
	if r := landlockRules(nil, nil, 0); r.TCPBind != nil {
		t.Errorf("port 0: %+v", r)
	}
}

func TestListenPort(t *testing.T) {
	for addr, want := range map[string]int{":8080": 8080, "127.0.0.1:9": 9, "[::1]:443": 443, "localhost:0": 0} {
		if got, err := listenPort(addr); err != nil || got != want {
			t.Errorf("%s: %d %v", addr, got, err)
		}
	}
	for _, bad := range []string{"8080", "host:http", ":70000", ""} {
		if _, err := listenPort(bad); err == nil {
			t.Errorf("%q: no error", bad)
		}
	}
}
