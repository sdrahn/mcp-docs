package server

import (
	"fmt"
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

	// By default the best section's text comes with it: one call answers.
	r := call(t, s, "find_sections", map[string]any{"query": "how do I back up the keys?", "collection": "app"})
	got := mustOK(t, r)
	if got != "1 section(s) for \"how do I back up the keys?\", best first:\n"+
		"1. app/ops.md#backup  Backup  (45 B)\n## Backup\nBack up /var/lib/app and its keys.\n[§ #backup, lines 2-3 of 5]\n"+
		"(read one: read_section doc#id)" {
		t.Errorf("find_sections:\n%s", got)
	}
	if rd := r.Structured["read"].(map[string]any); rd["section"] != "backup" || !strings.Contains(rd["text"].(string), "its keys") {
		t.Errorf("structured read: %v", rd)
	}
	// read: false, the list: each section with the words that matched.
	r = call(t, s, "find_sections", map[string]any{"query": "how do I back up the keys?", "collection": "app", "read": false})
	got = mustOK(t, r)
	if !strings.HasPrefix(got, "1 section(s) for \"how do I back up the keys?\", best first:\n"+
		"1. app/ops.md#backup  Backup  (45 B)\n   3: Back up /var/lib/app and its keys.\n") || r.Structured["read"] != nil {
		t.Errorf("find_sections, read false:\n%s", got)
	}

	// A best section larger than find_sections reads comes as its
	// introduction and the outline of its subsections.
	var big strings.Builder
	big.WriteString("# Restore\n## Restore from a backup {#restore-backup}\nRestore the keys first.\n")
	for i := 0; i < 6; i++ {
		fmt.Fprintf(&big, "### Step %d\n%s\n", i, strings.Repeat("Copy the files back and check them. ", 30))
	}
	write(t, filepath.Join(dirs["app"], "restore.md"), big.String())
	got = mustOK(t, call(t, s, "find_sections", map[string]any{"query": "restore from a backup", "collection": "app"}))
	if !strings.Contains(got, "1. app/restore.md#restore-backup  Restore from a backup") || !strings.Contains(got, "Restore the keys first.") ||
		!strings.Contains(got, "… ### Step 5  #step-5") || strings.Contains(got, "Copy the files back") {
		t.Errorf("a large best section:\n%s", got)
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
