package glob

import "testing"

func TestMatch(t *testing.T) {
	for _, tc := range []struct {
		pattern, name string
		want          bool
	}{
		{"*.md", "a/b/c.md", true},
		{"*.md", "c.txt", false},
		{"drafts", "guide/drafts", true},
		{"guide/**", "guide/a/b.md", true},
		{"guide/**/*.md", "guide/x.md", true},
		{"guide/*.md", "guide/a/x.md", false},
		{"**/x.md", "a/b/x.md", true},
		{"[ab].md", "c.md", false},
	} {
		if got := Match(tc.pattern, tc.name); got != tc.want {
			t.Errorf("Match(%q, %q) = %v", tc.pattern, tc.name, got)
		}
	}
	if Valid("[bad") || Valid("") || !Valid("a/**/b*") {
		t.Error("Valid")
	}
}
