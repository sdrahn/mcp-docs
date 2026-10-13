package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/url"
	"strconv"
	"strings"

	"github.com/sdrahn/mcp-docs/internal/catalog"
	"github.com/sdrahn/mcp-docs/internal/mcpserver"
)

// For clients that browse resources rather than call tools (§9): each
// document is a resource docs://collection/path, and a template reads
// one section, docs://collection/path#section. Tools stay the main way
// in: a resource read cannot carry depth, part or a version.

// resourcePage is how many resources one resources/list returns.
const resourcePage = 100

func resourceTemplates() []map[string]any {
	return []map[string]any{
		{"uriTemplate": "docs://{collection}/{+path}", "name": "document", "title": "A document",
			"description": "A document of a collection, whole when it fits one read, else its first part " +
				"(the outline and read_section tools read the rest).", "mimeType": "text/markdown"},
		{"uriTemplate": "docs://{collection}/{+path}{#section}", "name": "section", "title": "A section of a document",
			"description": "One section of a document, by its id (the heading's anchor); a large one's first part.",
			"mimeType":    "text/markdown"},
	}
}

// resourceURI is a document's resource URI.
func resourceURI(d catalog.Doc) string {
	return (&url.URL{Scheme: "docs", Host: d.Coll.Name, Path: "/" + d.Rel}).String()
}

func (s *Server) resourceList(ctx context.Context, cursor string) (any, error) {
	start := 0
	if cursor != "" {
		n, err := strconv.Atoi(cursor)
		if err != nil || n < 0 {
			return nil, &mcpserver.Error{Code: mcpserver.CodeInvalidParams, Message: "invalid cursor"}
		}
		start = n
	}
	resources := []map[string]any{}
	i, more := 0, false
	for _, k := range s.Cat.Collections {
		err := s.Cat.Walk(ctx, catalog.Doc{Coll: k, Rel: "."}, nil, func(d catalog.Doc, e fs.DirEntry) bool {
			if e.IsDir() {
				return true
			}
			i++
			if i <= start {
				return true
			}
			if len(resources) == resourcePage {
				more = true
				return false
			}
			r := map[string]any{"uri": resourceURI(d), "name": d.Address(), "mimeType": "text/markdown"}
			if en, err := s.Cat.Load(ctx, d); err == nil {
				if t := en.Outline.Title(); t != "" {
					r["title"] = t
				}
				if desc := en.Outline.Description(); desc != "" {
					r["description"] = desc
				}
				r["size"] = en.Outline.Bytes
			}
			resources = append(resources, r)
			return true
		})
		if err != nil && ctx.Err() != nil {
			return nil, err
		}
		if more {
			break
		}
	}
	out := map[string]any{"resources": resources}
	if more {
		out["nextCursor"] = strconv.Itoa(start + len(resources))
	}
	return out, nil
}

func (s *Server) readResource(ctx context.Context, uri string) (any, error) {
	noResource := func(msg string) error {
		return &mcpserver.Error{Code: mcpserver.CodeNoResource, Message: msg}
	}
	u, err := url.Parse(uri)
	if err != nil || u.Scheme != "docs" || u.Host == "" {
		return nil, noResource("not a docs:// resource: " + uri)
	}
	k := s.Cat.Collection(u.Host)
	if k == nil {
		return nil, noResource(fmt.Sprintf("no collection %q", u.Host))
	}
	rel := strings.TrimPrefix(u.Path, "/")
	if rel == "" {
		return nil, noResource("give a document: docs://" + k.Name + "/path")
	}
	addr := k.Name + "/" + rel
	var text string
	if u.Fragment != "" {
		args, _ := json.Marshal(map[string]any{"doc": addr, "section": u.Fragment})
		res, err := readSection(s, ctx, args)
		if err != nil {
			return nil, noResource(err.Error())
		}
		text = res.text
	} else {
		d, _, e, err := s.load(ctx, addr)
		if err != nil {
			return nil, noResource(err.Error())
		}
		if e.Outline.Bytes <= s.MaxRead {
			b, err := s.Cat.ReadAll(d, s.MaxRead)
			if err != nil {
				return nil, noResource(err.Error())
			}
			text = string(b)
		} else {
			res, err := s.lines(d, e, 0, 0, 0, 0, d.Address())
			if err != nil {
				return nil, noResource(err.Error())
			}
			text = res.text
		}
	}
	return map[string]any{"contents": []map[string]any{{"uri": uri, "mimeType": "text/markdown", "text": text}}}, nil
}

func prompts() []map[string]any {
	return []map[string]any{{
		"name": "answer_from_docs", "title": "Answer from the documentation",
		"description": "Answer a question from the documentation on this system, reading only the parts needed.",
		"arguments": []map[string]any{
			{"name": "question", "description": "the question", "required": true},
			{"name": "collection", "description": "the collection to answer from (default: any)", "required": false},
		},
	}}
}

func (s *Server) getPrompt(name string, args map[string]string) (any, error) {
	if name != "answer_from_docs" {
		return nil, &mcpserver.Error{Code: mcpserver.CodeInvalidParams, Message: "unknown prompt " + strconv.Quote(name)}
	}
	q := strings.TrimSpace(args["question"])
	if q == "" {
		return nil, &mcpserver.Error{Code: mcpserver.CodeInvalidParams, Message: "argument question is required"}
	}
	where := "the documentation on this system"
	start := "list_docs"
	if c := args["collection"]; c != "" {
		k := s.Cat.Collection(c)
		if k == nil {
			return nil, &mcpserver.Error{Code: mcpserver.CodeInvalidParams, Message: "no collection " + strconv.Quote(c)}
		}
		where = "the collection " + k.Name + " of the documentation on this system"
		start = "list_docs with collection " + k.Name
		if d, ok := s.Cat.Index(k); ok {
			start = "its index, " + d.Address() + " (read_lines)"
		}
	}
	text := "Answer the question below from " + where + ", reading only the parts you need. If you do not know " +
		"which document answers it, start with " + start + ". For a precise term (a setting, an error message, a " +
		"name), search for it and read the section of the match (read_section). For a broad question, outline " +
		"the document and read the one section that answers it. Say which documents and sections the answer " +
		"comes from (as document#id), and say so if the documentation does not answer it.\n\nQuestion: " + q
	return map[string]any{"description": "Answer from the documentation", "messages": []map[string]any{{
		"role": "user", "content": map[string]any{"type": "text", "text": text},
	}}}, nil
}
