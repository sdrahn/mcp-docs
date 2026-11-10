package server

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sdrahn/mcp-docs/internal/catalog"
	"github.com/sdrahn/mcp-docs/internal/rank"
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

	// Of a best section larger than find_sections reads, its subsection
	// that matches best.
	var big strings.Builder
	big.WriteString("# Restore\n## Restore from a backup {#restore-backup}\nRestore the keys first.\n")
	for i := 0; i < 6; i++ {
		fmt.Fprintf(&big, "### Step %d\n%s\n", i, strings.Repeat("Copy the files back and check them. ", 30))
		if i == 3 {
			big.WriteString("Then load the database dump with pg_restore.\n")
		}
	}
	write(t, filepath.Join(dirs["app"], "restore.md"), big.String())
	r = call(t, s, "find_sections", map[string]any{"query": "restore the database dump from a backup", "collection": "app"})
	got = mustOK(t, r)
	if !strings.Contains(got, "1. app/restore.md#restore-backup  Restore from a backup  (6.5 KiB)\n"+
		"   its best part: app/restore.md#step-3  Restore from a backup > Step 3  (1.1 KiB)\n### Step 3\n") ||
		!strings.Contains(got, "pg_restore") || strings.Contains(got, "### Step 4") ||
		!strings.Contains(got, "Its other parts (app/restore.md#id):\n   #step-0  Step 0  (1.1 KiB)\n") ||
		strings.Contains(got, "#step-3  Step 3") || strings.Contains(got, "2. app/restore.md#step-3") {
		t.Errorf("a large best section:\n%s", got)
	}
	if rd := r.Structured["read"].(map[string]any); rd["section"] != "step-3" {
		t.Errorf("structured read of a part: %v", rd)
	}
	// Of a large section without subsections, the passage that matches
	// best, and where it is.
	var long strings.Builder
	long.WriteString("# Storage\n## Key care\n")
	for i := 0; i < 40; i++ {
		fmt.Fprintf(&long, "Paragraph %d about storing things safely and well, in places that are dry, dark, cool and locked, away from the light.\n\n", i)
		if i == 25 {
			long.WriteString("To rotate the encryption keys, run keyctl rotate.\n\n```\nkeyctl rotate --all\n```\n\n")
		}
	}
	write(t, filepath.Join(dirs["other"], "keys.md"), long.String())
	got = mustOK(t, call(t, s, "find_sections", map[string]any{"query": "rotate the encryption keys", "collection": "other"}))
	if !strings.Contains(got, "1. other/keys.md#key-care") || !strings.Contains(got, "## Key care\n…\n") ||
		!strings.Contains(got, "To rotate the encryption keys") || !strings.Contains(got, "keyctl rotate --all\n```") ||
		strings.Contains(got, "Paragraph 0 ") || !strings.Contains(got, ": a passage of 4.8 KiB; read_lines for more") {
		t.Errorf("a passage:\n%s", got)
	}
	r = call(t, s, "find_sections", map[string]any{"query": "how do I back up the keys?", "collection": "app", "read": false})
	sec := r.Structured["sections"].([]any)[0].(map[string]any)
	if sec["doc"] != "app/ops.md" || sec["section"] != "backup" || sec["matchLine"].(float64) != 3 || sec["version"] == "" {
		t.Errorf("structured: %v", sec)
	}

	// doc: only the sections of a document or a directory, a version's
	// in a collection of several.
	for _, v := range []string{"1", "2"} {
		write(t, filepath.Join(dirs["other"], "v"+v, "upgrade.md"), "# Upgrade\n## Upgrading the cluster\nRun upgrade-"+v+".\n")
	}
	for _, doc := range []string{"other/v2", "other/v2/upgrade.md"} {
		r = call(t, s, "find_sections", map[string]any{"query": "upgrading the cluster", "doc": doc})
		for _, sec := range r.Structured["sections"].([]any) {
			if sec.(map[string]any)["doc"] != "other/v2/upgrade.md" {
				t.Errorf("doc %s: %s", doc, mustOK(t, r))
			}
		}
		if !strings.Contains(mustOK(t, r), "upgrade-2") {
			t.Errorf("doc %s: %s", doc, mustOK(t, r))
		}
	}
	docs := map[any]bool{}
	for _, sec := range call(t, s, "find_sections", map[string]any{"query": "upgrading the cluster", "doc": "other"}).Structured["sections"].([]any) {
		docs[sec.(map[string]any)["doc"]] = true
	}
	if !docs["other/v1/upgrade.md"] || !docs["other/v2/upgrade.md"] {
		t.Errorf("doc of a whole collection: %v", docs)
	}
	mustFail(t, call(t, s, "find_sections", map[string]any{"query": "x", "doc": "other/v3"}), "")
	mustFail(t, call(t, s, "find_sections", map[string]any{"query": "x", "doc": "other/v2", "collection": "other"}), "not both")

	// Across collections, the sections titled with the word first: the
	// collections are ranked as one, so the one that says it most.
	r = call(t, s, "find_sections", map[string]any{"query": "timeouts", "maxResults": 3})
	ss := r.Structured["sections"].([]any)
	if len(ss) < 2 || ss[0].(map[string]any)["section"] != "timeouts-elsewhere" || ss[1].(map[string]any)["section"] != "timeouts" {
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

// TestFindAcrossCollections: collections that share topics and differ
// in vocabulary are ranked as one, and a shared topic is one result.
func TestFindAcrossCollections(t *testing.T) {
	s, dirs := newServer(t, 16<<10, "base", "cluster", "sap")
	// "node" is rare in base and sap (a passing mention), and common in
	// cluster, where one section is on the question.
	shared := "# Node.js update strategy\n\nUpdates for a node come with maintenance updates.\n"
	for _, c := range []string{"base", "sap"} {
		write(t, filepath.Join(dirs[c], "topics/nodejs.md"), shared)
		for i := range 30 {
			write(t, filepath.Join(dirs[c], fmt.Sprintf("topics/t%d.md", i)),
				fmt.Sprintf("# Package %d\n\nInstall the package and restart the service.\n", i))
		}
	}
	write(t, filepath.Join(dirs["sap"], "topics/only-sap.md"), "# Tuning\n\nApply the solution.\n")
	write(t, filepath.Join(dirs["cluster"], "topics/maintenance.md"),
		"# Putting a node into maintenance mode\n\nNode maintenance mode puts all resources of the node into maintenance mode.\n")
	for i := range 30 {
		write(t, filepath.Join(dirs["cluster"], fmt.Sprintf("topics/c%d.md", i)),
			fmt.Sprintf("# Resource %d\n\nThe node runs the resource; put the cluster into maintenance mode first.\n", i))
	}
	r := call(t, s, "find_sections", map[string]any{"query": "put a node into maintenance mode"})
	text := mustOK(t, r)
	ss := r.Structured["sections"].([]any)
	if got := ss[0].(map[string]any)["doc"]; got != "cluster/topics/maintenance.md" {
		t.Errorf("first: %v, want the cluster's section on it\n%s", got, text)
	}
	seen := map[string]int{}
	for _, x := range ss {
		m := x.(map[string]any)
		seen[m["section"].(string)]++
		if m["section"] == "nodejs-update-strategy" {
			if also, _ := m["alsoIn"].([]any); m["doc"] != "base/topics/nodejs.md" || len(also) != 1 || also[0] != "sap/topics/nodejs.md" {
				t.Errorf("the shared topic: %v, also in %v", m["doc"], m["alsoIn"])
			}
		}
	}
	if seen["nodejs-update-strategy"] > 1 {
		t.Errorf("the shared topic is %d results:\n%s", seen["nodejs-update-strategy"], text)
	}
	// One collection alone ranks as before, and names no copies.
	r = call(t, s, "find_sections", map[string]any{"query": "node maintenance updates", "collection": "sap", "read": false})
	if text := mustOK(t, r); !strings.HasPrefix(strings.SplitN(text, "\n", 3)[1], "1. sap/topics/nodejs.md#nodejs-update-strategy") || strings.Contains(text, "also in") {
		t.Errorf("one collection: %s", text)
	}
	// The text names the copy by its collection.
	r = call(t, s, "find_sections", map[string]any{"query": "node.js update strategy", "read": false})
	if text := mustOK(t, r); !strings.Contains(text, "1. base/topics/nodejs.md#nodejs-update-strategy") || !strings.Contains(text, "; also in sap)") {
		t.Errorf("copy named: %s", text)
	}
}

// TestIndexFileUsed: a collection's index file is read where the index
// would be built, answers the same, and gives way when a document
// changes.
func TestIndexFileUsed(t *testing.T) {
	s, dirs := newServer(t, 16<<10, "app")
	write(t, filepath.Join(dirs["app"], "guide.md"), "# Guide\n\n## Timeouts\n\nSet the timeout in the file.\n\n## Approvals\n\nApprovals expire.\n")
	want := mustOK(t, call(t, s, "find_sections", map[string]any{"query": "timeouts"}))

	if err := rank.WriteFile(context.Background(), s.Cat, s.Cat.Collections[0], s.MaxFile); err != nil {
		t.Fatal(err)
	}
	var log []string
	s2 := &Server{Cat: catalog.New(s.Cat.Collections, s.Cat.Exts, s.Cat.MaxEntries), MaxRead: s.MaxRead, MaxFile: s.MaxFile,
		Logf: func(f string, a ...any) { log = append(log, fmt.Sprintf(f, a...)) }}
	if got := mustOK(t, call(t, s2, "find_sections", map[string]any{"query": "timeouts"})); got != want {
		t.Errorf("with the index file:\n%s\nwant:\n%s", got, want)
	}
	if len(log) != 1 || !strings.Contains(log[0], "app: index read from mcp-docs.index") {
		t.Errorf("log: %q", log)
	}
	// The index file is no document.
	if got := mustOK(t, call(t, s2, "list_docs", map[string]any{})); strings.Contains(got, "mcp-docs.index") {
		t.Errorf("the index file is listed: %s", got)
	}
	// A document changes: the index is built, and the new text found.
	write(t, filepath.Join(dirs["app"], "guide.md"), "# Guide\n\n## Deadlines\n\nSet the deadline in the file.\n")
	if got := mustOK(t, call(t, s2, "find_sections", map[string]any{"query": "deadlines"})); !strings.Contains(got, "app/guide.md#deadlines") {
		t.Errorf("after a change: %s", got)
	}
	// A server that starts now finds the file stale, says so, and builds.
	log = nil
	s3 := &Server{Cat: catalog.New(s.Cat.Collections, s.Cat.Exts, s.Cat.MaxEntries), MaxRead: s.MaxRead, MaxFile: s.MaxFile,
		Logf: func(f string, a ...any) { log = append(log, fmt.Sprintf(f, a...)) }}
	if got := mustOK(t, call(t, s3, "find_sections", map[string]any{"query": "deadlines"})); !strings.Contains(got, "app/guide.md#deadlines") {
		t.Errorf("a stale file: %s", got)
	}
	if len(log) != 1 || !strings.Contains(log[0], "not used") {
		t.Errorf("log of a stale file: %q", log)
	}
}

// TestSystemCollectionsFirst: the collection whose index names the
// system the server runs on is ranked first where collections answer
// alike, and is the result of a section others hold too.
func TestSystemCollectionsFirst(t *testing.T) {
	s, dirs := newServer(t, 16<<10, "aaa-1.0", "prod-1.1", "addon-1.1")
	prod := filepath.Join(t.TempDir(), "addon.prod")
	write(t, prod, "")
	index := func(dir, system string) {
		write(t, filepath.Join(dir, "README.md"), "---\ntitle: Index\nrank: false\n"+system+"---\n\n# Index\n")
	}
	index(dirs["aaa-1.0"], `system: {"os_release": {"ID": "prod", "VERSION_ID": "1.0"}}`+"\n")
	index(dirs["prod-1.1"], `system: {"os_release": {"ID": "prod", "VERSION_ID": "1.1"}}`+"\n")
	index(dirs["addon-1.1"], `system: {"os_release": {"VERSION_ID": "1.1"}, "file": "`+prod+`"}`+"\n")
	// The same topic in both versions, and one that differs in a word.
	for _, c := range []string{"aaa-1.0", "prod-1.1"} {
		write(t, filepath.Join(dirs[c], "topics/shared.md"), "# Rotating the keys\n\nRotate the keys every month.\n")
		write(t, filepath.Join(dirs[c], "topics/differs.md"), "# Mounting the volume\n\nMount the volume of version "+c+" first.\n")
	}
	write(t, filepath.Join(dirs["addon-1.1"], "topics/fence.md"), "# Fencing a node\n\nFence the node.\n")
	first := func(s *Server, q string) string {
		t.Helper()
		r := call(t, s, "find_sections", map[string]any{"query": q, "read": false})
		mustOK(t, r)
		ss := r.Structured["sections"].([]any)
		if len(ss) == 0 {
			t.Fatalf("%q: nothing", q)
		}
		return ss[0].(map[string]any)["doc"].(string)
	}
	on := func(osr map[string]string, prefer []string) *Server {
		return &Server{Cat: catalog.New(s.Cat.Collections, s.Cat.Exts, s.Cat.MaxEntries), MaxRead: s.MaxRead, MaxFile: s.MaxFile,
			Structured: true, OSRelease: osr, Prefer: prefer}
	}
	// No system known: the first collection's, as before.
	if got := first(on(nil, nil), "rotate the keys"); got != "aaa-1.0/topics/shared.md" {
		t.Errorf("no system: %s", got)
	}
	// On prod 1.1: its collection's copy is the result, and its variant first.
	sys := on(map[string]string{"ID": "prod", "VERSION_ID": "1.1"}, nil)
	if got := first(sys, "rotate the keys"); got != "prod-1.1/topics/shared.md" {
		t.Errorf("the shared topic on prod 1.1: %s", got)
	}
	if got := first(sys, "mount the volume"); got != "prod-1.1/topics/differs.md" {
		t.Errorf("the differing topic on prod 1.1: %s", got)
	}
	// The addon is this system's too (its version, and its file is
	// there); another product's question is still answered by its own
	// collection.
	if !sys.prefers(s.Cat.Collection("addon-1.1")) || sys.prefers(s.Cat.Collection("aaa-1.0")) {
		t.Error("the addon of 1.1 is preferred on 1.1, and 1.0 is not")
	}
	if got := mustOK(t, call(t, sys, "find_sections", map[string]any{"query": "rotate the keys", "read": false})); !strings.Contains(got, "also in aaa-1.0") {
		t.Errorf("the other version is named: %s", got)
	}
	if text := sys.Instructions(); !strings.Contains(text, "- prod-1.1 (this system's)") || strings.Contains(text, "- aaa-1.0 (this system's)") {
		t.Errorf("instructions: %s", text)
	}
	// A system whose file is missing is not the addon's.
	if err := os.Remove(prod); err != nil {
		t.Fatal(err)
	}
	if on(map[string]string{"ID": "prod", "VERSION_ID": "1.1"}, nil).prefers(s.Cat.Collection("addon-1.1")) {
		t.Error("the addon is preferred without its file")
	}
	// Named collections replace the system's; none is none.
	if got := first(on(map[string]string{"ID": "prod", "VERSION_ID": "1.1"}, []string{"aaa-1.0"}), "mount the volume"); got != "aaa-1.0/topics/differs.md" {
		t.Errorf("--prefer aaa-1.0: %s", got)
	}
	if on(map[string]string{"ID": "prod", "VERSION_ID": "1.1"}, []string{}).prefers(s.Cat.Collection("prod-1.1")) {
		t.Error("--prefer none prefers")
	}
}
