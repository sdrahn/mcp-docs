package docbook

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/sdrahn/mcp-docs/internal/catalog"
	"github.com/sdrahn/mcp-docs/internal/server"
)

// The converted article served by mcp-docs: questions in words find the
// section that answers them first, and SUSE's ids address sections.
func TestServed(t *testing.T) {
	out, _ := convert(t)
	s := &server.Server{Cat: catalog.New([]*catalog.Collection{{Name: "sles-16.0", Path: out}}, []string{".md"}, 1000),
		MaxRead: 32 << 10, MaxFile: 1 << 20, Version: "test"}
	call := func(tool string, args map[string]any) (string, json.RawMessage) {
		t.Helper()
		p, _ := json.Marshal(map[string]any{"name": tool, "arguments": args})
		res, err := s.Handle(context.Background(), "tools/call", p)
		if err != nil {
			t.Fatal(err)
		}
		b, _ := json.Marshal(res)
		var r struct {
			Content    []struct{ Text string }
			IsError    bool
			Structured json.RawMessage `json:"structuredContent"`
		}
		if err := json.Unmarshal(b, &r); err != nil || r.IsError {
			t.Fatalf("%s: %s", tool, b)
		}
		return r.Content[0].Text, r.Structured
	}
	for q, want := range map[string]string{
		"switch SELinux to permissive until the next boot": "sec-getenforce-setenforce",
		"disable SELinux permanently":                      "selinux-enable-disable",
		"allow a service to use a non-standard port":       "selinux-modify-port",
		"why was access denied, read the audit log":        "selinux-search-audit-log",
		"make a domain permissive":                         "setting-permissive-domain",
		"set a boolean persistently":                       "switching-booleans",
		"which packages do I need for SELinux tools":       "selinux-packages",
	} {
		_, st := call("find_sections", map[string]any{"query": q})
		var r struct{ Sections []struct{ Section string } }
		if err := json.Unmarshal(st, &r); err != nil || len(r.Sections) == 0 || r.Sections[0].Section != want {
			t.Errorf("%q: %+v, want #%s first", q, r.Sections, want)
		}
	}
	text, _ := call("read_section", map[string]any{"doc": "sles-16.0/topics/selinux-switching-modes.md#sellinux-temporary-mode-changes"})
	if !strings.HasPrefix(text, "## Changing the SELinux mode temporarily {#sellinux-temporary-mode-changes}") ||
		!strings.Contains(text, "# setenforce MODE_ID") {
		t.Errorf("read_section: %s", text)
	}
	text, _ = call("list_docs", map[string]any{})
	if !strings.Contains(text, "sles-16.0/README.md — SUSE Linux Enterprise Server 16.0 (index: read it first)") ||
		!strings.Contains(text, "sles-16.0/topics/selinux-switching-modes.md — Changing the SELinux mode") {
		t.Errorf("list_docs: %s", text)
	}
}
