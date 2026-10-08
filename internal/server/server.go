// Package server is mcp-docs' MCP server: the tools that read documents
// a part at a time, and the instructions that teach agents to use them
// (docs/architecture.md, §5 and §6).
package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/sdrahn/mcp-docs/internal/catalog"
	"github.com/sdrahn/mcp-docs/internal/mcpserver"
)

// Name is the server's name in serverInfo.
const Name = "mcp-docs"

// Server serves a catalog.
type Server struct {
	Cat *catalog.Catalog
	// About, if set, opens the instructions: what the documents are.
	About string
	// MaxRead bounds what one call returns; MaxFile the size of a file
	// search reads.
	MaxRead, MaxFile int64
	// FSCompat also offers mcp-server-fs's reading tools, with its
	// names and arguments (§5.7).
	FSCompat bool
	Version  string
}

// tool is one tool's definition and handler. Handlers return the result
// or an error that becomes a tool error.
type tool struct {
	name, title, description string
	input, output            map[string]any
	compat                   bool // one of mcp-server-fs's
	run                      func(s *Server, ctx context.Context, args json.RawMessage) (*result, error)
}

type result struct {
	text       string
	structured any
}

func obj(props map[string]any, required ...string) map[string]any {
	m := map[string]any{"type": "object", "properties": props, "additionalProperties": false}
	if len(required) > 0 {
		m["required"] = required
	}
	return m
}

func str(desc string) map[string]any { return map[string]any{"type": "string", "description": desc} }
func boolean(desc string) map[string]any {
	return map[string]any{"type": "boolean", "description": desc}
}
func integer(min, max int, desc string) map[string]any {
	m := map[string]any{"type": "integer", "minimum": min, "description": desc}
	if max > 0 {
		m["maximum"] = max
	}
	return m
}

var intType = map[string]any{"type": "integer"}
var strType = map[string]any{"type": "string"}

func (s *Server) tools() []tool {
	ts := []tool{listDocsTool(), outlineTool(), searchTool(), readSectionTool(s), readLinesTool(s)}
	if s.FSCompat {
		ts = append(ts, compatTools(s)...)
	}
	return ts
}

// allToolNames are the names of every tool the server may offer.
func allToolNames() []string {
	s := &Server{FSCompat: true, Cat: catalog.New(nil, nil, 0)}
	var out []string
	for _, t := range s.tools() {
		out = append(out, t.name)
	}
	return out
}

func (s *Server) toolList() []map[string]any {
	var out []map[string]any
	for _, t := range s.tools() {
		m := map[string]any{"name": t.name, "title": t.title, "description": t.description, "inputSchema": t.input,
			"annotations": map[string]any{"title": t.title, "readOnlyHint": true, "destructiveHint": false,
				"idempotentHint": true, "openWorldHint": false}}
		if t.output != nil {
			m["outputSchema"] = t.output
		}
		out = append(out, m)
	}
	return out
}

func (s *Server) call(ctx context.Context, name string, args json.RawMessage) (any, error) {
	var t *tool
	for _, c := range s.tools() {
		if c.name == name {
			t = &c
			break
		}
	}
	if t == nil {
		return nil, &mcpserver.Error{Code: mcpserver.CodeInvalidParams, Message: "unknown tool " + strconv.Quote(name)}
	}
	if len(args) == 0 || string(args) == "null" {
		args = json.RawMessage("{}")
	}
	res, err := t.run(s, ctx, args)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return map[string]any{"content": []map[string]any{{"type": "text", "text": err.Error()}}, "isError": true}, nil
	}
	out := map[string]any{"content": []map[string]any{{"type": "text", "text": res.text}}, "isError": false}
	if res.structured != nil {
		out["structuredContent"] = res.structured
	}
	return out, nil
}

// decode reads a tool's arguments into v, refusing unknown ones.
func decode(args json.RawMessage, v any) error {
	d := json.NewDecoder(bytes.NewReader(args))
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		return fmt.Errorf("invalid arguments: %v", err)
	}
	return nil
}

// Handle answers an MCP request (the protocol around it is
// internal/mcpserver's).
func (s *Server) Handle(ctx context.Context, method string, params json.RawMessage) (any, error) {
	switch method {
	case "initialize":
		var p struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		_ = json.Unmarshal(params, &p)
		return map[string]any{
			"protocolVersion": mcpserver.Negotiate(p.ProtocolVersion),
			"capabilities":    map[string]any{"tools": map[string]any{}, "resources": map[string]any{}, "prompts": map[string]any{}},
			"serverInfo":      map[string]any{"name": Name, "title": "Documentation", "version": s.Version},
			"instructions":    s.Instructions(),
		}, nil
	case "ping":
		return map[string]any{}, nil
	case "tools/list":
		return map[string]any{"tools": s.toolList()}, nil
	case "tools/call":
		var p struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, &mcpserver.Error{Code: mcpserver.CodeInvalidParams, Message: "invalid params"}
		}
		return s.call(ctx, p.Name, p.Arguments)
	case "resources/list":
		var p struct {
			Cursor string `json:"cursor"`
		}
		_ = json.Unmarshal(params, &p)
		return s.resourceList(ctx, p.Cursor)
	case "resources/templates/list":
		return map[string]any{"resourceTemplates": resourceTemplates()}, nil
	case "resources/read":
		var p struct {
			URI string `json:"uri"`
		}
		if err := json.Unmarshal(params, &p); err != nil || p.URI == "" {
			return nil, &mcpserver.Error{Code: mcpserver.CodeInvalidParams, Message: "invalid params"}
		}
		return s.readResource(ctx, p.URI)
	case "prompts/list":
		return map[string]any{"prompts": prompts()}, nil
	case "prompts/get":
		var p struct {
			Name      string            `json:"name"`
			Arguments map[string]string `json:"arguments"`
		}
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, &mcpserver.Error{Code: mcpserver.CodeInvalidParams, Message: "invalid params"}
		}
		return s.getPrompt(p.Name, p.Arguments)
	}
	return nil, &mcpserver.Error{Code: mcpserver.CodeMethodNotFound, Message: "method not found"}
}

// Instructions are generated from the collections (§6): what they are,
// how to read them a part at a time, and the limits of one call.
func (s *Server) Instructions() string {
	var b strings.Builder
	if s.About != "" {
		b.WriteString(s.About + "\n\n")
	}
	b.WriteString("Documentation on this system, read-only. Collections:\n")
	example := ""
	for _, k := range s.Cat.Collections {
		fmt.Fprintf(&b, "- %s", k.Name)
		if k.Title != "" {
			fmt.Fprintf(&b, " (%s)", k.Title)
		}
		if k.Description != "" {
			fmt.Fprintf(&b, ": %s", k.Description)
			if !strings.HasSuffix(k.Description, ".") {
				b.WriteString(".")
			}
		}
		if d, ok := s.Cat.Index(k); ok {
			fmt.Fprintf(&b, " Index: %s.", d.Address())
			if example == "" {
				example = d.Address()
			}
		} else {
			b.WriteString(" No index: list_docs lists its documents.")
		}
		if k.Instructions != "" {
			b.WriteString(" " + strings.Join(strings.Fields(k.Instructions), " "))
		}
		b.WriteString("\n")
	}
	if example == "" {
		example = s.Cat.Collections[0].Name + "/guide.md"
	}
	fmt.Fprintf(&b, "\nA document is named collection/path (%s), a section document#id (ids are the headings' "+
		"anchors, as links in the documents have them).\n\n", example)
	b.WriteString("Read only the part you need:\n" +
		"- which document: read the collection's index (read_lines), or list_docs (titles and descriptions);\n" +
		"- a precise term (a setting, an error message, a name): search, then read_section with the section " +
		"of the match (or read_lines around its line);\n" +
		"- a broad question: outline the document (maxLevel 2 for a large one), then read_section of the one " +
		"section that answers it;\n" +
		"- a long section: read_section with depth 0 gives its introduction and the outline of its " +
		"subsections; a section larger than one call comes in parts (part 2, …).\n" +
		"Read a document whole only when the question needs all of it.")
	fmt.Fprintf(&b, " One call returns at most %s (%s).", size(s.MaxRead), tokens(s.MaxRead))
	if s.FSCompat {
		fmt.Fprintf(&b, "\n\nThe tools outline_file, search_text, read_text_file, search_files and list_directory "+
			"take file paths, absolute or relative to %s, as mcp-server-fs's do.", s.Cat.Collections[0].Path)
	}
	return b.String()
}

func size(n int64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.1f GiB", float64(n)/(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MiB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f KiB", float64(n)/(1<<10))
	}
	return fmt.Sprintf("%d B", n)
}

// tokens estimates the tokens of n bytes of text (4 bytes a token).
func tokens(n int64) string {
	t := (n + 3) / 4
	switch {
	case t >= 10000:
		return fmt.Sprintf("~%dk tokens", (t+500)/1000)
	case t >= 1000:
		return fmt.Sprintf("~%.1fk tokens", float64(t)/1000)
	}
	return fmt.Sprintf("~%d tokens", t)
}
