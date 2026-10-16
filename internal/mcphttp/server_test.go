package mcphttp

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sdrahn/mcp-docs/internal/catalog"
	"github.com/sdrahn/mcp-docs/internal/server"
)

// The documentation server over HTTP, as a client uses it: initialize,
// list the tools, read a section; each POST on its own.
func TestDocsServer(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "guide.md"), []byte("# Guide\n## Setup\nRun the installer.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := &server.Server{Cat: catalog.New([]*catalog.Collection{{Name: "app", Path: dir}}, []string{".md"}, 100),
		MaxRead: 32 << 10, MaxFile: 1 << 20, Version: "test"}
	srv := httptest.NewServer(Handler(s.Handle, Options{}))
	defer srv.Close()

	r := post(t, srv, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"t","version":"1"}}}`, nil)
	var init struct {
		Result struct {
			ProtocolVersion string `json:"protocolVersion"`
			ServerInfo      struct{ Name string }
		}
	}
	if err := json.Unmarshal([]byte(r.body), &init); err != nil || init.Result.ProtocolVersion != "2025-11-25" ||
		init.Result.ServerInfo.Name != "mcp-docs" || r.header.Get("Mcp-Session-Id") != "" {
		t.Fatalf("initialize: %d %q", r.status, r.body)
	}
	v := map[string]string{"MCP-Protocol-Version": "2025-11-25"}
	if r := post(t, srv, `{"jsonrpc":"2.0","method":"notifications/initialized"}`, v); r.status != 202 {
		t.Errorf("initialized: %d", r.status)
	}
	if r := post(t, srv, `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`, v); !strings.Contains(r.body, `"find_sections"`) {
		t.Errorf("tools/list: %q", r.body)
	}
	r = post(t, srv, `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"read_section","arguments":{"doc":"app/guide.md#setup"}}}`, v)
	if !strings.Contains(r.body, `## Setup\nRun the installer.\n[§ #setup, lines 2-3 of 3]`) {
		t.Errorf("read_section: %q", r.body)
	}
}
