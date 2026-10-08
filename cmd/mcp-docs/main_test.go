package main

import (
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
