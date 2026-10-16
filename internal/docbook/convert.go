package docbook

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

// Options of a conversion.
type Options struct {
	// Src is the checkout of the sources (SUSE/doc-modular); DC the build
	// definition, relative to Src ("DC-SLES-SELinux").
	Src, DC string
	// Out is the directory the documents are written to.
	Out string
	// Name, Title and Description are the collection's.
	Name, Title, Description string
	// SourceURL is the URL of a source file, "%s" its path relative to
	// Src (default: on GitHub, branch main).
	SourceURL string
	// Revision names the sources' revision in the collection's
	// instructions ("c66ed55").
	Revision string
}

// Report says what a conversion did and what it could not.
type Report struct {
	Articles, Documents int
	// Unknown are elements the converter does not know (rendered as their
	// content), by name and count; Unresolved entity names; Broken link
	// targets (ids not in the collection).
	Unknown    map[string]int
	Unresolved []string
	Broken     []string
}

// OK reports whether nothing was lost.
func (r *Report) OK() bool {
	return len(r.Unknown) == 0 && len(r.Unresolved) == 0 && len(r.Broken) == 0
}

func (r *Report) String() string {
	s := fmt.Sprintf("%d article(s), %d document(s)", r.Articles, r.Documents)
	if len(r.Unknown) > 0 {
		var parts []string
		for _, k := range sortedKeys(r.Unknown) {
			parts = append(parts, fmt.Sprintf("%s (%d)", k, r.Unknown[k]))
		}
		s += "; unknown elements, rendered as their content: " + strings.Join(parts, ", ")
	}
	if len(r.Unresolved) > 0 {
		s += "; unresolved entities: " + strings.Join(r.Unresolved, ", ")
	}
	if len(r.Broken) > 0 {
		s += "; links to ids not in the collection: " + strings.Join(r.Broken, ", ")
	}
	return s
}

// dc reads a build definition: KEY="value" lines.
func dc(file string) (map[string]string, error) {
	b, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	sc := bufio.NewScanner(bytes.NewReader(b))
	for sc.Scan() {
		l := strings.TrimSpace(sc.Text())
		if l == "" || strings.HasPrefix(l, "#") {
			continue
		}
		k, v, ok := strings.Cut(l, "=")
		if !ok {
			continue
		}
		out[strings.TrimSpace(k)] = strings.Trim(strings.TrimSpace(v), `"'`)
	}
	return out, sc.Err()
}

// profiling maps a build definition's PROF* values to the attributes
// they select on: PROFOS="sles" keeps elements whose os names sles.
func profiling(def map[string]string) map[string][]string {
	out := map[string][]string{}
	for k, v := range def {
		if !strings.HasPrefix(k, "PROF") || v == "" {
			continue
		}
		attr := strings.ToLower(strings.TrimPrefix(k, "PROF"))
		out[attr] = strings.FieldsFunc(v, func(r rune) bool { return r == ';' || r == ' ' })
	}
	return out
}

// source is a source file read, entities expanded, parsed and profiled.
type source struct {
	rel  string // relative to Src
	root *Node
}

func (c *conversion) read(rel string) (*source, error) {
	file := filepath.Join(c.o.Src, rel)
	raw, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}
	ents, err := EntitiesOf(raw, filepath.Dir(file))
	if err != nil {
		return nil, fmt.Errorf("%s: %v", rel, err)
	}
	text, unresolved := Expand(raw, ents)
	for _, u := range unresolved {
		c.unresolved[u] = true
	}
	root, err := Parse(text)
	if err != nil {
		return nil, fmt.Errorf("%s: %v", rel, err)
	}
	Profile(root, c.prof)
	return &source{rel: rel, root: root}, nil
}

type conversion struct {
	o          Options
	prof       map[string][]string
	unresolved map[string]bool
}

// module is a topic of an article, in its order and nesting.
type module struct {
	resource string
	title    string // the article's title for it, if it overrides the topic's
	children []*module
}

// Convert converts the article of a build definition into documents in
// Out: one per topic (topics/ID.md), one index per article
// (articles/ID.md), the collection's index (README.md).
func Convert(o Options) (*Report, error) {
	if o.SourceURL == "" {
		o.SourceURL = "https://github.com/SUSE/doc-modular/blob/main/%s"
	}
	def, err := dc(filepath.Join(o.Src, o.DC))
	if err != nil {
		return nil, err
	}
	if def["MAIN"] == "" {
		return nil, fmt.Errorf("%s: no MAIN", o.DC)
	}
	c := &conversion{o: o, prof: profiling(def), unresolved: map[string]bool{}}
	asmRel := path.Join(def["SRC_DIR"], def["MAIN"])
	asm, err := c.read(asmRel)
	if err != nil {
		return nil, err
	}

	// Resources: id → file, relative to Src.
	resources := map[string]string{}
	for _, rs := range asm.root.Elements("resources") {
		for _, r := range rs.Elements("resource") {
			resources[r.Attr["id"]] = path.Clean(path.Join(path.Dir(asmRel), r.Attr["href"]))
		}
	}
	st := asm.root.Child("structure")
	if st == nil {
		return nil, fmt.Errorf("%s: no structure", asmRel)
	}
	var modules func(n *Node) []*module
	modules = func(n *Node) []*module {
		var out []*module
		for _, m := range n.Elements("module") {
			mod := &module{resource: m.Attr["resourceref"], children: modules(m)}
			if mg := m.Child("merge"); mg != nil {
				if t := mg.Child("title"); t != nil {
					mod.title = strings.TrimSpace(spaces.ReplaceAllString(plain(t), " "))
				}
			}
			out = append(out, mod)
		}
		return out
	}
	tree := modules(st)

	// First pass: read every topic and note where each id is, with its
	// title, so that links between topics resolve.
	r := &renderer{links: map[string]Target{}, unknown: map[string]int{}, broken: map[string]bool{}}
	type topic struct {
		src  *source
		id   string
		file string
	}
	var topics []topic
	seen := map[string]bool{}
	var collect func([]*module) error
	collect = func(ms []*module) error {
		for _, m := range ms {
			rel, ok := resources[m.resource]
			if !ok {
				return fmt.Errorf("%s: module %q names no resource", asmRel, m.resource)
			}
			if !seen[rel] {
				seen[rel] = true
				src, err := c.read(rel)
				if err != nil {
					return err
				}
				id := src.root.Attr["id"]
				if id == "" {
					return fmt.Errorf("%s: the topic has no xml:id", rel)
				}
				file := "topics/" + id + ".md"
				topics = append(topics, topic{src, id, file})
				r.note(src.root, file, "")
			}
			if err := collect(m.children); err != nil {
				return err
			}
		}
		return nil
	}
	if err := collect(tree); err != nil {
		return nil, err
	}
	artID := st.Attr["id"]
	artFile := "articles/" + artID + ".md"
	artTitle, artDesc, artAbstract, tasks := articleMeta(r, st)
	r.links[artID] = Target{File: artFile, Title: artTitle, Anchor: artID}

	// Second pass: write the documents.
	for _, d := range []string{"topics", "articles"} {
		if err := os.MkdirAll(filepath.Join(o.Out, d), 0o755); err != nil {
			return nil, err
		}
	}
	for _, t := range topics {
		r.file = t.file
		body := r.section(t.src.root, 1)
		desc := ""
		if info := t.src.root.Child("info"); info != nil {
			if a := info.Child("abstract"); a != nil {
				desc = strings.Join(r.paragraphText(a.Children), " ")
			}
		}
		fields := [][2]any{{"title", r.title(t.src.root)}}
		if desc != "" {
			fields = append(fields, [2]any{"description", desc})
		}
		var tags []string
		if role := t.src.root.Attr["role"]; role != "" {
			tags = append(tags, role)
		}
		if len(tags) > 0 {
			fields = append(fields, [2]any{"tags", tags})
		}
		fields = append(fields, [2]any{"source", fmt.Sprintf(o.SourceURL, t.src.rel)})
		if err := os.WriteFile(filepath.Join(o.Out, t.file), []byte(frontMatter(fields)+join(body)), 0o644); err != nil {
			return nil, err
		}
	}

	// The article's index: its topics in order and nesting.
	r.file = artFile
	var idx strings.Builder
	fields := [][2]any{{"title", artTitle}}
	if artDesc != "" {
		fields = append(fields, [2]any{"description", artDesc})
	}
	fields = append(fields, [2]any{"tags", append([]string{"article"}, tasks...)},
		[2]any{"source", fmt.Sprintf(o.SourceURL, asmRel)})
	idx.WriteString(frontMatter(fields))
	fmt.Fprintf(&idx, "# %s {#%s}\n\n", artTitle, artID)
	if artAbstract != "" {
		idx.WriteString(artAbstract + "\n\n")
	}
	idx.WriteString("The article's topics, in order:\n\n")
	var list func([]*module, string)
	list = func(ms []*module, indent string) {
		for _, m := range ms {
			rel := resources[m.resource]
			var id string
			for _, t := range topics {
				if t.src.rel == rel {
					id = t.id
				}
			}
			title := m.title
			if title == "" {
				title = r.links[id].Title
			}
			fmt.Fprintf(&idx, "%s- [%s](%s)\n", indent, title, relPath("articles", r.links[id].File))
			list(m.children, indent+"  ")
		}
	}
	list(tree, "")
	if err := os.WriteFile(filepath.Join(o.Out, artFile), []byte(idx.String()), 0o644); err != nil {
		return nil, err
	}

	// The collection's index.
	var readme strings.Builder
	readme.WriteString(frontMatter([][2]any{{"title", o.Title}, {"description", o.Description}}))
	fmt.Fprintf(&readme, "# %s\n\n%s\n\n## Articles\n\n", o.Title, o.Description)
	fmt.Fprintf(&readme, "- [%s](%s)", artTitle, artFile)
	if artDesc != "" {
		fmt.Fprintf(&readme, ": %s", artDesc)
	}
	readme.WriteString("\n\n## Source and license\n\n")
	fmt.Fprintf(&readme, "Converted by mcp-docs-convert from SUSE's DocBook sources (%s, %s",
		strings.TrimSuffix(fmt.Sprintf(o.SourceURL, ""), "/"), o.DC)
	if o.Revision != "" {
		fmt.Fprintf(&readme, ", revision %s", o.Revision)
	}
	readme.WriteString("). Section ids are the sources' xml:ids. Copyright SUSE LLC and contributors; " +
		"licensed under the GNU Free Documentation License 1.2 or (at your option) 1.3, with the copyright " +
		"notice and license as the Invariant Section")
	// The legal notices are the sources' shared files (common/).
	var legal []string
	for _, t := range topics {
		if strings.HasPrefix(t.src.rel, "common/") {
			legal = append(legal, fmt.Sprintf("[%s](%s)", r.links[t.id].Title, t.file))
		}
	}
	if len(legal) > 0 {
		readme.WriteString(": " + strings.Join(legal, ", "))
	}
	readme.WriteString(".\n")
	if err := os.WriteFile(filepath.Join(o.Out, "README.md"), []byte(readme.String()), 0o644); err != nil {
		return nil, err
	}

	rep := &Report{Articles: 1, Documents: len(topics) + 2, Unknown: r.unknown}
	for u := range c.unresolved {
		rep.Unresolved = append(rep.Unresolved, u)
	}
	for b := range r.broken {
		rep.Broken = append(rep.Broken, b)
	}
	sort.Strings(rep.Unresolved)
	sort.Strings(rep.Broken)
	return rep, nil
}

// headings are the elements rendered as headings, whose ids are
// anchors.
var headings = set("section", "topic", "sect1", "sect2", "sect3", "simplesect", "chapter", "appendix",
	"article", "legalnotice")

// note records where the ids of an element and those below it are: a
// heading's id is its own anchor; another element's id leads to the
// heading that holds it, under its own title if it has one.
func (r *renderer) note(n *Node, file, section string) {
	if n.Name == "" {
		return
	}
	id := n.Attr["id"]
	if headings[n.Name] && id != "" {
		section = id
	}
	if id != "" {
		if _, dup := r.links[id]; !dup {
			t := Target{File: file, Title: r.title(n), Anchor: section}
			if t.Title == "" && section != "" {
				t.Title = r.links[section].Title
			}
			if t.Anchor == "" {
				t.Anchor = id
			}
			r.links[id] = t
		}
	}
	for _, c := range n.Children {
		r.note(c, file, section)
	}
}

// articleMeta reads the article's title, description, abstract and
// task tags from its structure's merge.
func articleMeta(r *renderer, st *Node) (title, desc, abstract string, tasks []string) {
	mg := st.Child("merge")
	if mg == nil {
		return st.Attr["id"], "", "", nil
	}
	if t := mg.Child("title"); t != nil {
		title = r.inline(t.Children)
	}
	for _, m := range mg.Elements("meta") {
		switch m.Attr["name"] {
		case "description":
			desc = strings.TrimSpace(spaces.ReplaceAllString(plain(m), " "))
		case "task":
			for _, p := range m.Elements("phrase") {
				tasks = append(tasks, strings.TrimSpace(plain(p)))
			}
		}
	}
	if a := mg.Child("abstract"); a != nil {
		abstract = strings.Join(r.paragraphText(a.Children), "\n\n")
	}
	if title == "" {
		title = st.Attr["id"]
	}
	return title, desc, abstract, tasks
}
