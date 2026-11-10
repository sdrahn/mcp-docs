package docbook

import (
	"context"
	"encoding/json"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/sdrahn/mcp-docs/internal/catalog"
	"github.com/sdrahn/mcp-docs/internal/eval"
	"github.com/sdrahn/mcp-docs/internal/server"
)

// A whole converted collection, served (tools/suse-docs): questions in
// words across its articles find the answering section first.
// MCP_DOCS_SLES16 names the converted SLES 16.0 collection; without it
// the test is skipped.
func TestSLES16(t *testing.T) {
	dir := os.Getenv("MCP_DOCS_SLES16")
	if dir == "" {
		t.Skip("MCP_DOCS_SLES16 is not set (tools/suse-docs/README.md)")
	}
	answeredFirst(t, "sles-16.0", dir, eval.SLES)
}

// SUSE Linux Enterprise High Availability 16.0 (tools/suse-docs/convert,
// PRODUCT=sle-ha-16.0). MCP_DOCS_SLE_HA names the converted collection.
func TestSLEHA16(t *testing.T) {
	dir := os.Getenv("MCP_DOCS_SLE_HA")
	if dir == "" {
		t.Skip("MCP_DOCS_SLE_HA is not set (tools/suse-docs/README.md)")
	}
	answeredFirst(t, "sle-ha-16.0", dir, eval.SLEHA)
}

// SUSE Linux Enterprise Server for SAP applications 16.0
// (tools/suse-docs/convert, PRODUCT=sles-sap-16.0). MCP_DOCS_SLES_SAP
// names the converted collection.
func TestSLESSAP16(t *testing.T) {
	dir := os.Getenv("MCP_DOCS_SLES_SAP")
	if dir == "" {
		t.Skip("MCP_DOCS_SLES_SAP is not set (tools/suse-docs/README.md)")
	}
	answeredFirst(t, "sles-sap-16.0", dir, eval.SLESSAP)
}

// The 16.1 family (tools/suse-docs/convert, PRODUCT=sles-16.1,
// sle-ha-16.1): the questions of 16.0, which 16.1's documentation answers
// too. MCP_DOCS_SLES161 and MCP_DOCS_SLE_HA161 name the converted
// collections. SLES for SAP 16.1 answers 12 of its 16 first: its Agama
// installation is split into steps none of which answers "install SLES
// for SAP with Agama" (docs/architecture.md §14.11).
func TestSLES161(t *testing.T) {
	dir := os.Getenv("MCP_DOCS_SLES161")
	if dir == "" {
		t.Skip("MCP_DOCS_SLES161 is not set (tools/suse-docs/README.md)")
	}
	answeredFirst(t, "sles-16.1", dir, eval.SLES)
}

func TestSLEHA161(t *testing.T) {
	dir := os.Getenv("MCP_DOCS_SLE_HA161")
	if dir == "" {
		t.Skip("MCP_DOCS_SLE_HA161 is not set (tools/suse-docs/README.md)")
	}
	answeredFirst(t, "sle-ha-16.1", dir, eval.SLEHA)
}

// SUSE Linux Micro 6.0, 6.1 and 6.2, one collection, a directory per
// version (tools/suse-docs/convert-sle-micro): questions asked of one
// version (doc: its directory) find the answering section of that
// version first. MCP_DOCS_SLE_MICRO names the converted collection.
func TestSLEMicro(t *testing.T) {
	dir := os.Getenv("MCP_DOCS_SLE_MICRO")
	if dir == "" {
		t.Skip("MCP_DOCS_SLE_MICRO is not set (tools/suse-docs/README.md)")
	}
	answeredFirst(t, "sle-micro", dir, eval.SLEMicro)
}

// answeredFirst serves a collection and asks find_sections each
// question: at least 80% must have an answering section first.
func answeredFirst(t *testing.T, name, dir string, questions []eval.Question) {
	t.Helper()
	s := &server.Server{Cat: catalog.New([]*catalog.Collection{{Name: name, Path: dir}}, []string{".md"}, 5000),
		MaxRead: 32 << 10, MaxFile: 1 << 20, Structured: true, Version: "test"}
	found := 0
	for _, c := range questions {
		args := map[string]any{"query": c.Ask}
		if c.Doc != "" {
			args["doc"] = c.Doc
		}
		p, _ := json.Marshal(map[string]any{"name": "find_sections", "arguments": args})
		res, err := s.Handle(context.Background(), "tools/call", p)
		if err != nil {
			t.Fatal(err)
		}
		b, _ := json.Marshal(res)
		var r struct {
			Structured struct {
				Sections []struct{ Doc, Section string }
			} `json:"structuredContent"`
		}
		if err := json.Unmarshal(b, &r); err != nil {
			t.Fatal(err)
		}
		var got []string
		for i, sec := range r.Structured.Sections {
			if i < 5 {
				got = append(got, sec.Doc+"#"+sec.Section)
			}
		}
		if len(got) > 0 && strings.HasPrefix(got[0], c.Doc) &&
			slices.ContainsFunc(c.Want, func(w string) bool { return strings.HasSuffix(got[0], "#"+w) }) {
			found++
			continue
		}
		t.Logf("%q: %v, want #%s first", c.Ask, got, strings.Join(c.Want, " or #"))
	}
	t.Logf("%d of %d questions answered first", found, len(questions))
	if found*10 < len(questions)*8 {
		t.Errorf("fewer than 80%% of the questions answered first")
	}
}
