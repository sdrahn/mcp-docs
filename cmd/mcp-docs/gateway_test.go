package main

import (
	"bufio"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

// TestMain runs the program itself when a test executes the test binary
// with MCP_DOCS_RUN_MAIN set: the arguments are the program's.
func TestMain(m *testing.M) {
	if os.Getenv("MCP_DOCS_RUN_MAIN") == "1" {
		var args []string
		json.Unmarshal([]byte(os.Getenv("MCP_DOCS_ARGS")), &args)
		os.Args = append([]string{"mcp-docs"}, args...)
		main()
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// The server suse-docs for mcp-gateway (packaging/gateway): a read-only
// server without network in the gateway's domain for mcp-docs, Landlock
// over where documentation and collection files live, and a command
// line mcp-docs serves with: its arguments, with the collection files of
// a test directory, answer an MCP session.
func TestGatewayDefinition(t *testing.T) {
	b, err := os.ReadFile("../../packaging/gateway/suse-docs.yaml.in")
	if err != nil {
		t.Fatal(err)
	}
	var d struct {
		Version     int      `yaml:"version"`
		Name        string   `yaml:"name"`
		Command     []string `yaml:"command"`
		SELinuxType string   `yaml:"selinux_type"`
		RunAs       string   `yaml:"run_as"`
		Network     *bool    `yaml:"network"`
		Landlock    struct {
			Read []string `yaml:"read"`
		} `yaml:"landlock"`
	}
	dec := yaml.NewDecoder(strings.NewReader(string(b)))
	dec.KnownFields(true)
	if err := dec.Decode(&d); err != nil {
		t.Fatal(err)
	}
	if d.Version != 1 || d.Name != "suse-docs" || d.SELinuxType != "mcpsrv_docs_t" || d.RunAs != "dynamic" ||
		d.Network == nil || *d.Network || len(d.Command) < 1 || d.Command[0] != "@BINDIR@/mcp-docs" {
		t.Fatalf("definition %+v", d)
	}
	if !slices.Contains(d.Landlock.Read, "@DATADIR@") || !slices.Contains(d.Landlock.Read, "/etc/mcp-docs") {
		t.Errorf("landlock.read %v: the collections' roots and collection files", d.Landlock.Read)
	}
	// No collection on the command line: the installed collection files.
	for _, a := range d.Command[1:] {
		if a == "--root" || a == "--collection" || a == "--config-dirs" || a == "--listen" {
			t.Errorf("command %v: %s", d.Command, a)
		}
	}

	dir := t.TempDir()
	docs := filepath.Join(dir, "docs")
	os.MkdirAll(filepath.Join(dir, "collections.d"), 0o755)
	os.MkdirAll(docs, 0o755)
	os.WriteFile(filepath.Join(docs, "guide.md"), []byte("# Guide\n\n## Zypper\n\nzypper refresh updates the repositories.\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "collections.d", "product.yaml"),
		[]byte("title: Product\nroot: "+docs+"\ndescription: The product's guide.\n"), 0o644)
	args := append(append([]string{}, d.Command[1:]...), "--config-dirs", filepath.Join(dir, "collections.d"))
	c := exec.Command(os.Args[0], "-test.run=^$")
	ja, _ := json.Marshal(args)
	c.Env = append(os.Environ(), "MCP_DOCS_RUN_MAIN=1", "MCP_DOCS_ARGS="+string(ja))
	var stderr strings.Builder
	c.Stderr = &stderr
	in, _ := c.StdinPipe()
	outp, _ := c.StdoutPipe()
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}
	// Answers come as they are ready: stdin stays open until the last
	// one, as a client's does (at its end, the server stops).
	in.Write([]byte(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}
{"jsonrpc":"2.0","method":"notifications/initialized"}
{"jsonrpc":"2.0","id":2,"method":"tools/list"}
{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"find_sections","arguments":{"query":"refresh the repositories"}}}
`))
	var out strings.Builder
	done := make(chan struct{})
	go func() {
		defer close(done)
		sc := bufio.NewScanner(outp)
		sc.Buffer(make([]byte, 1<<20), 1<<20)
		for n := 0; n < 3 && sc.Scan(); {
			out.WriteString(sc.Text() + "\n")
			if strings.Contains(sc.Text(), `"id":`) {
				n++
			}
		}
	}()
	select {
	case <-done:
	case <-time.After(20 * time.Second):
		t.Error("no three answers within 20 s")
	}
	in.Close()
	c.Wait()
	if t.Failed() {
		t.Fatalf("stderr: %s\nstdout: %s", stderr.String(), out.String())
	}
	for _, want := range []string{`"name":"find_sections"`, `"name":"read_section"`, "The product documentation installed on this system", "zypper refresh"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("no %q in\n%s", want, out.String())
		}
	}
	// The four tools Custos's agent templates are allowed, and the
	// instructions name no other.
	if n := strings.Count(out.String(), `"inputSchema"`); n != 4 || !strings.Contains(out.String(), `"name":"search"`) ||
		!strings.Contains(out.String(), `"name":"outline"`) {
		t.Errorf("%d tools, want find_sections, search, outline and read_section:\n%s", n, out.String())
	}
	for _, no := range []string{"list_docs", "read_lines"} {
		if strings.Contains(out.String(), no) {
			t.Errorf("%s is named, and not offered:\n%s", no, out.String())
		}
	}
}
