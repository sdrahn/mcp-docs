package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sdrahn/mcp-docs/internal/mcpserver"
)

// handle calls the server as a client would and returns the result as
// JSON-decoded maps.
func handle(t *testing.T, s *Server, method string, params any) (map[string]any, error) {
	t.Helper()
	raw, _ := json.Marshal(params)
	res, err := s.Handle(context.Background(), method, raw)
	if err != nil {
		return nil, err
	}
	b, _ := json.Marshal(res)
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	return m, nil
}

// list_docs gives titles and descriptions from front matter or the
// first paragraph; the instructions carry each collection's title,
// index (or how to list it) and instructions.
func TestDescriptionsAndInstructions(t *testing.T) {
	s, dirs := newServer(t, 32<<10, "app", "bare")
	k := s.Cat.Collections[0]
	k.Title, k.Instructions = "The App", "The configuration is\n  not here: ask app-admin."
	write(t, filepath.Join(dirs["app"], "README.md"), "# App docs\n\nWhich file answers what.\n")
	write(t, filepath.Join(dirs["app"], "guide.md"), "---\ntitle: The guide\ndescription: How to set it up.\ntags: [setup]\n---\n# Guide\nText.\n")
	write(t, filepath.Join(dirs["bare"], "x.md"), "# X\n")

	r := call(t, s, "list_docs", map[string]any{"collection": "app"})
	got := mustOK(t, r)
	if !strings.Contains(got, "app/README.md — App docs (index: read it first)  [3 lines, 37 B]\n      Which file answers what.\n") ||
		!strings.Contains(got, "app/guide.md — The guide  [7 lines, 84 B]\n      How to set it up.\n") {
		t.Errorf("list_docs:\n%s", got)
	}
	if d := r.Structured["documents"].([]any)[1].(map[string]any); d["description"] != "How to set it up." ||
		fmt.Sprint(d["tags"]) != "[setup]" {
		t.Errorf("structured: %v", d)
	}
	got = mustOK(t, call(t, s, "list_docs", map[string]any{}))
	if !strings.Contains(got, "app (The App): the app docs") {
		t.Errorf("collections:\n%s", got)
	}

	ins := s.Instructions()
	for _, want := range []string{
		"- app (The App): the app docs. Index: app/README.md. The configuration is not here: ask app-admin.\n",
		"- bare: the bare docs. No index: list_docs lists its documents.\n",
	} {
		if !strings.Contains(ins, want) {
			t.Errorf("instructions lack %q:\n%s", want, ins)
		}
	}
}

func TestResources(t *testing.T) {
	s, dirs := newServer(t, 4096, "app")
	write(t, filepath.Join(dirs["app"], "guide.md"), guide)
	write(t, filepath.Join(dirs["app"], "a b.md"), "# Spaced\n")
	big := "# Big\n" + strings.Repeat("a line of text\n", 1000)
	write(t, filepath.Join(dirs["app"], "big.md"), big)

	m, err := handle(t, s, "resources/list", map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	rs := m["resources"].([]any)
	var uris []string
	for _, r := range rs {
		uris = append(uris, r.(map[string]any)["uri"].(string))
	}
	if strings.Join(uris, " ") != "docs://app/a%20b.md docs://app/big.md docs://app/guide.md" || m["nextCursor"] != nil {
		t.Errorf("resources: %v %v", uris, m)
	}
	if g := rs[2].(map[string]any); g["title"] != "Guide" || g["description"] != "Intro text." || g["mimeType"] != "text/markdown" {
		t.Errorf("guide: %v", g)
	}

	read := func(uri string) string {
		t.Helper()
		m, err := handle(t, s, "resources/read", map[string]any{"uri": uri})
		if err != nil {
			t.Fatalf("%s: %v", uri, err)
		}
		return m["contents"].([]any)[0].(map[string]any)["text"].(string)
	}
	if got := read("docs://app/guide.md"); got != guide {
		t.Errorf("whole: %q", got)
	}
	if got := read("docs://app/a%20b.md"); got != "# Spaced\n" {
		t.Errorf("escaped: %q", got)
	}
	if got := read("docs://app/guide.md#configure"); !strings.HasPrefix(got, "## Configure\n") || !strings.Contains(got, "[§ #configure") {
		t.Errorf("section: %q", got)
	}
	if got := read("docs://app/big.md"); len(got) > 4096 || !strings.Contains(got, "go on with offset") {
		t.Errorf("big: %d bytes", len(got))
	}
	for _, bad := range []string{"file:///etc/passwd", "docs://nope/x.md", "docs://app/missing.md", "docs://app/guide.md#nope", "docs://app/"} {
		_, err := handle(t, s, "resources/read", map[string]any{"uri": bad})
		var e *mcpserver.Error
		if !errors.As(err, &e) || e.Code != mcpserver.CodeNoResource {
			t.Errorf("%s: %v", bad, err)
		}
	}
	m, _ = handle(t, s, "resources/templates/list", nil)
	if len(m["resourceTemplates"].([]any)) != 2 {
		t.Errorf("templates: %v", m)
	}
}

func TestResourcePages(t *testing.T) {
	s, dirs := newServer(t, 32<<10, "app")
	for i := 0; i < resourcePage+5; i++ {
		write(t, filepath.Join(dirs["app"], fmt.Sprintf("d%03d.md", i)), "# x\n")
	}
	m, _ := handle(t, s, "resources/list", map[string]any{})
	if len(m["resources"].([]any)) != resourcePage || m["nextCursor"] != fmt.Sprint(resourcePage) {
		t.Fatalf("page 1: %d %v", len(m["resources"].([]any)), m["nextCursor"])
	}
	m, _ = handle(t, s, "resources/list", map[string]any{"cursor": m["nextCursor"]})
	if rs := m["resources"].([]any); len(rs) != 5 || m["nextCursor"] != nil ||
		rs[0].(map[string]any)["uri"] != fmt.Sprintf("docs://app/d%03d.md", resourcePage) {
		t.Errorf("page 2: %v", m)
	}
	if _, err := handle(t, s, "resources/list", map[string]any{"cursor": "x"}); err == nil {
		t.Error("a bad cursor was taken")
	}
}

func TestPrompt(t *testing.T) {
	s, dirs := newServer(t, 32<<10, "app")
	write(t, filepath.Join(dirs["app"], "README.md"), "# x\n")
	m, _ := handle(t, s, "prompts/list", nil)
	if p := m["prompts"].([]any); len(p) != 1 || p[0].(map[string]any)["name"] != "answer_from_docs" {
		t.Errorf("prompts: %v", m)
	}
	m, err := handle(t, s, "prompts/get", map[string]any{"name": "answer_from_docs",
		"arguments": map[string]string{"question": "How do I set a timeout?", "collection": "app"}})
	if err != nil {
		t.Fatal(err)
	}
	text := m["messages"].([]any)[0].(map[string]any)["content"].(map[string]any)["text"].(string)
	for _, want := range []string{"the collection app", "its index, app/README.md (read_lines)", "read_section",
		"Question: How do I set a timeout?"} {
		if !strings.Contains(text, want) {
			t.Errorf("prompt lacks %q: %s", want, text)
		}
	}
	for _, args := range []map[string]any{
		{"name": "nope", "arguments": map[string]string{"question": "q"}},
		{"name": "answer_from_docs", "arguments": map[string]string{}},
		{"name": "answer_from_docs", "arguments": map[string]string{"question": "q", "collection": "nope"}},
	} {
		if _, err := handle(t, s, "prompts/get", args); err == nil {
			t.Errorf("%v: no error", args)
		}
	}
}
