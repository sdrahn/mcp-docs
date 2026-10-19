package server

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestFindSections(t *testing.T) {
	s, dirs := newServer(t, 32<<10, "app", "other")
	write(t, filepath.Join(dirs["app"], "guide.md"), guide)
	write(t, filepath.Join(dirs["app"], "ops.md"), "# Operations\n## Backup\nBack up /var/lib/app and its keys.\n## Logs\nSee journalctl.\n")
	write(t, filepath.Join(dirs["other"], "x.md"), "# X\n## Timeouts elsewhere\ntimeout timeout\n")

	r := call(t, s, "find_sections", map[string]any{"query": "how do I back up the keys?", "collection": "app"})
	got := mustOK(t, r)
	if !strings.HasPrefix(got, "1 section(s) for \"how do I back up the keys?\", best first:\n"+
		"1. app/ops.md#backup  Backup  (45 B)\n   3: Back up /var/lib/app and its keys.\n") ||
		!strings.Contains(got, "read_section") {
		t.Errorf("find_sections:\n%s", got)
	}
	sec := r.Structured["sections"].([]any)[0].(map[string]any)
	if sec["doc"] != "app/ops.md" || sec["section"] != "backup" || sec["matchLine"].(float64) != 3 || sec["version"] == "" {
		t.Errorf("structured: %v", sec)
	}

	// Across collections, the section titled with the words first.
	r = call(t, s, "find_sections", map[string]any{"query": "timeouts", "maxResults": 3})
	ss := r.Structured["sections"].([]any)
	if len(ss) < 2 || ss[0].(map[string]any)["section"] != "timeouts" {
		t.Errorf("across collections: %s", mustOK(t, r))
	}

	if got := mustOK(t, call(t, s, "find_sections", map[string]any{"query": "zebra"})); !strings.HasPrefix(got, "no sections") {
		t.Errorf("no match: %q", got)
	}
	mustFail(t, call(t, s, "find_sections", map[string]any{"query": "how is it"}), "no words")
	mustFail(t, call(t, s, "find_sections", map[string]any{"query": ""}), "query")
	mustFail(t, call(t, s, "find_sections", map[string]any{"query": "x", "collection": "nope"}), "no such collection")
	mustFail(t, call(t, s, "find_sections", map[string]any{"query": "x", "maxResults": 51}), "maxResults")

	// A changed document is found as it is now.
	write(t, filepath.Join(dirs["app"], "ops.md"), "# Operations\n## Restore\nRestore the keys from the backup.\n")
	bumpTime(t, filepath.Join(dirs["app"], "ops.md"))
	r = call(t, s, "find_sections", map[string]any{"query": "restore keys", "collection": "app"})
	if ss := r.Structured["sections"].([]any); len(ss) == 0 || ss[0].(map[string]any)["section"] != "restore" {
		t.Errorf("after a change: %s", mustOK(t, r))
	}
}

// With list items, entries are sections: in outline (marked), search
// and read_section; mcp-server-fs's outline_file keeps to headings.
func TestListItemSections(t *testing.T) {
	s, dirs := newServer(t, 32<<10, "app")
	s.Cat.Collections[0].ListItems = true
	write(t, filepath.Join(dirs["app"], "arch.md"), "# Arch\n## Roadmap\n"+
		"1. **Design** (done):\n   - the problem: x\n"+
		"2. **Landlock as a second wall** (0.18):\n   - the problem: y\n   - tests: z\n## Other\n")
	got := mustOK(t, call(t, s, "outline", map[string]any{"doc": "app/arch.md"}))
	if !strings.Contains(got, "### 2. Landlock as a second wall (item)  #2-landlock-as-a-second-wall  [3 lines") {
		t.Errorf("outline:\n%s", got)
	}
	got = mustOK(t, call(t, s, "search", map[string]any{"query": "tests: z"}))
	if !strings.Contains(got, "§ Roadmap > 2. Landlock as a second wall  #2-landlock-as-a-second-wall") {
		t.Errorf("search:\n%s", got)
	}
	got = mustOK(t, call(t, s, "read_section", map[string]any{"doc": "app/arch.md", "section": "Roadmap > Landlock as a second wall"}))
	if !strings.HasPrefix(got, "2. **Landlock as a second wall** (0.18):\n") || !strings.Contains(got, "lines 5-7 of 8") {
		t.Errorf("read_section:\n%s", got)
	}
	got = mustOK(t, call(t, s, "outline_file", map[string]any{"path": "arch.md"}))
	if strings.Contains(got, "Landlock") || !strings.Contains(got, "## Roadmap") {
		t.Errorf("outline_file:\n%s", got)
	}
}

// bumpTime moves a file's modification time on, so that a rewrite in the
// same instant is a new version.
func bumpTime(t *testing.T, p string) {
	t.Helper()
	later := time.Now().Add(time.Minute)
	if err := os.Chtimes(p, later, later); err != nil {
		t.Fatal(err)
	}
}
