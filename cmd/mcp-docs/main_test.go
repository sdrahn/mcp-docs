package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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
	cs, problems, err := loadCollections([]string{dir}, nil, []string{"two=" + b})
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
