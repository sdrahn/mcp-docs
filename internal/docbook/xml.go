// Package docbook converts SUSE's DocBook documentation sources to
// Markdown documents that mcp-docs serves (docs/architecture.md, §14):
// entities resolved, profiled text selected, every xml:id kept as an
// anchor.
//
// This file reads the sources: entity declarations (followed through
// parameter entities to the files they name), their expansion before
// parsing (entities hold markup, which profiling must see), and a
// small element tree.
package docbook

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

var (
	comment    = regexp.MustCompile(`(?s)<!--.*?-->`)
	entityDecl = regexp.MustCompile(`<!ENTITY\s+(%\s+)?([A-Za-z_][\w.\-:]*)\s+(SYSTEM\s+)?("[^"]*"|'[^']*')\s*>`)
	peRef      = regexp.MustCompile(`%([A-Za-z_][\w.\-:]*);`)
	entityRef  = regexp.MustCompile(`&([A-Za-z_][\w.\-:]*);`)
	doctype    = regexp.MustCompile(`(?s)<!DOCTYPE\s+\w+\s*\[(.*?)\]\s*>`)
)

// predefined are XML's own entities, left to the parser.
var predefined = map[string]bool{"amp": true, "lt": true, "gt": true, "quot": true, "apos": true}

// Entities are general entities by name: their replacement text, markup
// included.
type Entities map[string]string

// entityLoader reads declarations in document order, as XML does: the
// first declaration of a name wins, and a parameter entity reference
// reads the file its declaration names, at that point.
type entityLoader struct {
	ents  Entities
	files map[string]bool
	pes   map[string]string // parameter entity → absolute file
}

func newEntityLoader() *entityLoader {
	return &entityLoader{ents: Entities{}, files: map[string]bool{}, pes: map[string]string{}}
}

// declarations reads a DTD fragment whose relative paths are relative
// to dir.
func (l *entityLoader) declarations(text, dir string) error {
	text = comment.ReplaceAllString(text, "")
	type event struct {
		at   int
		decl []string // a declaration's submatches
		ref  string   // a parameter entity reference
	}
	var events []event
	for _, m := range entityDecl.FindAllStringSubmatchIndex(text, -1) {
		sub := make([]string, 5)
		for i := 0; i < 5; i++ {
			if m[2*i] >= 0 {
				sub[i] = text[m[2*i]:m[2*i+1]]
			}
		}
		events = append(events, event{at: m[0], decl: sub})
	}
	// References outside declarations (a declaration's own text can hold
	// a "%" that is no reference only in quoted values, which the
	// declaration pattern consumed).
	masked := entityDecl.ReplaceAllStringFunc(text, func(s string) string { return strings.Repeat(" ", len(s)) })
	for _, m := range peRef.FindAllStringSubmatchIndex(masked, -1) {
		events = append(events, event{at: m[0], ref: masked[m[2]:m[3]]})
	}
	sort.Slice(events, func(i, j int) bool { return events[i].at < events[j].at })
	for _, e := range events {
		if e.ref != "" {
			file, ok := l.pes[e.ref]
			if !ok {
				return fmt.Errorf("parameter entity %%%s; is not declared", e.ref)
			}
			if err := l.file(file); err != nil {
				return err
			}
			continue
		}
		isPE, name, system, value := e.decl[1] != "", e.decl[2], e.decl[3] != "", e.decl[4]
		value = value[1 : len(value)-1]
		switch {
		case isPE && system:
			if _, ok := l.pes[name]; !ok {
				l.pes[name] = filepath.Join(dir, value)
			}
		case isPE, system:
			// Internal parameter entities and external general ones are
			// not used by the sources.
		default:
			if _, ok := l.ents[name]; !ok {
				l.ents[name] = value
			}
		}
	}
	return nil
}

func (l *entityLoader) file(path string) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	if l.files[abs] {
		return nil
	}
	l.files[abs] = true
	b, err := os.ReadFile(abs)
	if err != nil {
		return err
	}
	return l.declarations(string(b), filepath.Dir(abs))
}

// EntitiesOf returns the entities a source file declares in its
// DOCTYPE, following the files it names.
func EntitiesOf(src []byte, dir string) (Entities, error) {
	l := newEntityLoader()
	if m := doctype.FindSubmatch(src); m != nil {
		if err := l.declarations(string(m[1]), dir); err != nil {
			return nil, err
		}
	}
	return l.ents, nil
}

// Expand replaces entity references in a document's text (its DOCTYPE
// left out) with their replacement text, recursively; it returns the
// names it could not resolve.
func Expand(src []byte, ents Entities) ([]byte, []string) {
	text := doctype.ReplaceAllString(string(src), "")
	unresolved := map[string]bool{}
	for depth := 0; depth < 10; depth++ {
		changed := false
		text = entityRef.ReplaceAllStringFunc(text, func(ref string) string {
			name := ref[1 : len(ref)-1]
			if predefined[name] {
				return ref
			}
			v, ok := ents[name]
			if !ok {
				unresolved[name] = true
				return "&amp;" + name + ";" // kept visible, and parsable
			}
			changed = true
			return v
		})
		if !changed {
			break
		}
	}
	var names []string
	for n := range unresolved {
		names = append(names, n)
	}
	sort.Strings(names)
	return []byte(text), names
}

// Node is an element (Name set) or a run of text.
type Node struct {
	Name     string // local name; "" for text
	Attr     map[string]string
	Children []*Node
	Text     string
}

// Parse reads an element tree; names are local (namespaces dropped),
// xml:id is Attr["id"].
func Parse(src []byte) (*Node, error) {
	d := xml.NewDecoder(bytes.NewReader(src))
	root := &Node{Name: "#document"}
	stack := []*Node{root}
	for {
		tok, err := d.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		top := stack[len(stack)-1]
		switch t := tok.(type) {
		case xml.StartElement:
			n := &Node{Name: t.Name.Local, Attr: map[string]string{}}
			for _, a := range t.Attr {
				if a.Name.Space == "xmlns" || a.Name.Local == "xmlns" {
					continue
				}
				n.Attr[a.Name.Local] = a.Value
			}
			top.Children = append(top.Children, n)
			stack = append(stack, n)
		case xml.EndElement:
			stack = stack[:len(stack)-1]
		case xml.CharData:
			top.Children = append(top.Children, &Node{Text: string(t)})
		}
	}
	for _, c := range root.Children {
		if c.Name != "" {
			return c, nil
		}
	}
	return nil, fmt.Errorf("no root element")
}

// Child returns the first child element of that name, or nil.
func (n *Node) Child(name string) *Node {
	for _, c := range n.Children {
		if c.Name == name {
			return c
		}
	}
	return nil
}

// Elements returns the child elements of that name.
func (n *Node) Elements(name string) []*Node {
	var out []*Node
	for _, c := range n.Children {
		if c.Name == name {
			out = append(out, c)
		}
	}
	return out
}

// String serializes the tree: names, attributes in order, text; equal
// trees, equal strings.
func (n *Node) String() string {
	var b strings.Builder
	var w func(*Node)
	w = func(n *Node) {
		if n.Name == "" {
			b.WriteString(n.Text)
			return
		}
		b.WriteString("<" + n.Name)
		keys := make([]string, 0, len(n.Attr))
		for k := range n.Attr {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			fmt.Fprintf(&b, " %s=%q", k, n.Attr[k])
		}
		b.WriteString(">")
		for _, c := range n.Children {
			w(c)
		}
		b.WriteString("</" + n.Name + ">")
	}
	w(n)
	return b.String()
}

// Walk calls fn for n and every element below it.
func (n *Node) Walk(fn func(*Node)) {
	if n.Name == "" {
		return
	}
	fn(n)
	for _, c := range n.Children {
		c.Walk(fn)
	}
}

// Profile removes the elements whose profiling attributes (os, arch,
// audience, …) name none of the values selected for them; values are
// separated by ";". An attribute not selected for keeps its element.
func Profile(n *Node, selected map[string][]string) {
	kept := n.Children[:0]
	for _, c := range n.Children {
		if c.Name != "" && !matches(c, selected) {
			continue
		}
		Profile(c, selected)
		kept = append(kept, c)
	}
	n.Children = kept
}

func matches(n *Node, selected map[string][]string) bool {
	for attr, want := range selected {
		v, ok := n.Attr[attr]
		if !ok {
			continue
		}
		found := false
		for _, x := range strings.Split(v, ";") {
			for _, w := range want {
				if strings.TrimSpace(x) == w {
					found = true
				}
			}
		}
		if !found {
			return false
		}
	}
	return true
}
