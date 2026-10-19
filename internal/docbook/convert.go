package docbook

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/sdrahn/mcp-docs/internal/markdown"
)

// Options of a conversion.
type Options struct {
	// Src is the checkout of the sources (SUSE/doc-modular); DCs the
	// build definitions of the collection's articles, relative to Src
	// ("DC-SLES-SELinux").
	Src string
	DCs []string
	// Out is the directory the documents are written to.
	Out string
	// Name, Title and Description are the collection's.
	Name, Title, Description string
	// SourceURL is the URL of a source file, "%s" its path relative to
	// Src (default: on GitHub, branch main).
	SourceURL string
	// SiteURL is the URL of an article's directory on
	// documentation.suse.com, "%s" its build definition's name without
	// "DC-" ("https://documentation.suse.com/sles/16.0/html/%s/"). When
	// set, a document's source is its page there.
	SiteURL string
	// Revision names the sources' revision in the collection's
	// instructions ("c66ed55").
	Revision string
	// MaxSection, when set, is the size in bytes of a section's own
	// text (to the next heading) over which the report names it.
	MaxSection int
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
	// Conflicts are topics whose text still differs between articles
	// that share their document.
	Conflicts []string
	// Large are the sections whose own text is over Options.MaxSection
	// ("topics/ID.md#SECTION (12.3 KiB)"): agents read them in parts. A
	// note for the sources' authors, not a loss.
	Large      []string
	MaxSection int
}

// OK reports whether nothing was lost.
func (r *Report) OK() bool {
	return len(r.Unknown) == 0 && len(r.Unresolved) == 0 && len(r.Broken) == 0 && len(r.Conflicts) == 0
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
	if len(r.Conflicts) > 0 {
		s += "; topics that differ between articles: " + strings.Join(r.Conflicts, ", ")
	}
	if len(r.Large) > 0 {
		s += fmt.Sprintf("; sections over %s of their own text: %s", kib(int64(r.MaxSection)), strings.Join(r.Large, ", "))
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

// source is a source file read, entities expanded, includes resolved,
// parsed and profiled.
type source struct {
	rel  string // relative to Src
	root *Node
}

// parse reads a file into an element tree, its entities expanded and
// its XIncludes resolved; profiling is the caller's.
func (c *conversion) parse(file string, depth int) (*Node, error) {
	if depth > 8 {
		return nil, fmt.Errorf("%s: includes nested too deep", file)
	}
	raw, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}
	ents, err := EntitiesOf(raw, filepath.Dir(file))
	if err != nil {
		return nil, fmt.Errorf("%s: %v", file, err)
	}
	text, unresolved := Expand(raw, ents)
	for _, u := range unresolved {
		c.unresolved[u] = true
	}
	root, err := Parse(text)
	if err != nil {
		return nil, fmt.Errorf("%s: %v", file, err)
	}
	return root, c.include(root, filepath.Dir(file), depth)
}

// include replaces the XIncludes below n with what they name: an
// element tree, or text with parse="text".
func (c *conversion) include(n *Node, dir string, depth int) error {
	for i, ch := range n.Children {
		if ch.Name != "include" || ch.Attr["href"] == "" {
			if err := c.include(ch, dir, depth); err != nil {
				return err
			}
			continue
		}
		file := filepath.Join(dir, ch.Attr["href"])
		if ch.Attr["parse"] == "text" {
			b, err := os.ReadFile(file)
			if err != nil {
				return err
			}
			n.Children[i] = &Node{Text: string(b)}
			continue
		}
		root, err := c.parse(file, depth+1)
		if err != nil {
			return err
		}
		n.Children[i] = root
	}
	return nil
}

func (c *conversion) read(rel string, prof map[string][]string) (*source, error) {
	root, err := c.parse(filepath.Join(c.o.Src, rel), 0)
	if err != nil {
		return nil, err
	}
	Profile(root, prof)
	return &source{rel: rel, root: root}, nil
}

type conversion struct {
	o          Options
	unresolved map[string]bool
}

// module is a topic of an article, in its order and nesting; a module
// without a resource is a heading of its own (a title, an abstract)
// over its children.
type module struct {
	resource string
	id       string // a group's id
	title    string // the article's title for it, if it overrides the topic's
	abstract string
	page     string // the page of the site it is on, "index.html" or "ID.html"
	children []*module
}

// article is a build definition's article.
type article struct {
	name     string // the build definition's name without "DC-": the site's
	asmRel   string
	st       *Node
	tree     []*module
	topics   []*topic
	byRel    map[string]*topic
	file     string
	renderer *renderer
	title    string
	desc     string
	abstract string
	tasks    []string
}

// topic is a topic as an article profiles it.
type topic struct {
	src  *source
	id   string
	page string
	key  [sha256.Size]byte // of its profiled text: articles that agree share its document
	file string
	body string // rendered
}

// assignFiles names the topics' documents by their variants.
func assignFiles(arts []*article) {
	variants := map[string]map[[sha256.Size]byte]string{}
	for _, a := range arts {
		for _, t := range a.topics {
			if variants[t.id] == nil {
				variants[t.id] = map[[sha256.Size]byte]string{}
			}
			if _, ok := variants[t.id][t.key]; !ok {
				variants[t.id][t.key] = a.name
			}
		}
	}
	for _, a := range arts {
		for _, t := range a.topics {
			t.file = "topics/" + t.id + ".md"
			if v := variants[t.id]; len(v) > 1 {
				t.file = "topics/" + t.id + "--" + v[t.key] + ".md"
			}
		}
	}
}

// links is a renderer that knows where the article's ids are.
func (a *article) links(unknown map[string]int, broken map[string]bool) *renderer {
	r := &renderer{links: map[string]Target{}, unknown: unknown, broken: broken}
	for _, t := range a.topics {
		r.note(t.src.root, t.file, "")
	}
	// The article's groups (modules without a topic) are items of its
	// index: a link to one goes to the article.
	var groups func([]*module)
	groups = func(ms []*module) {
		for _, m := range ms {
			if m.resource == "" && m.id != "" {
				r.links[m.id] = Target{File: a.file, Title: m.title, Anchor: a.st.Attr["id"]}
			}
			groups(m.children)
		}
	}
	groups(a.tree)
	return r
}

// load reads a build definition's article and its topics.
func (c *conversion) load(dcName string) (*article, error) {
	def, err := dc(filepath.Join(c.o.Src, dcName))
	if err != nil {
		return nil, err
	}
	if def["MAIN"] == "" {
		return nil, fmt.Errorf("%s: no MAIN", dcName)
	}
	prof := profiling(def)
	a := &article{name: strings.TrimPrefix(path.Base(dcName), "DC-"), byRel: map[string]*topic{}}
	a.asmRel = path.Join(def["SRC_DIR"], def["MAIN"])
	asm, err := c.read(a.asmRel, prof)
	if err != nil {
		return nil, err
	}
	// Resources: id → file, relative to Src.
	resources := map[string]string{}
	for _, rs := range asm.root.Elements("resources") {
		for _, r := range rs.Elements("resource") {
			resources[r.Attr["id"]] = path.Clean(path.Join(path.Dir(a.asmRel), r.Attr["href"]))
		}
	}
	// The article: the structure STRUCTID names, or the first.
	for _, st := range asm.root.Elements("structure") {
		if a.st == nil || st.Attr["id"] == def["STRUCTID"] {
			a.st = st
		}
		if def["STRUCTID"] == "" {
			break
		}
	}
	if a.st == nil || (def["STRUCTID"] != "" && a.st.Attr["id"] != def["STRUCTID"]) {
		return nil, fmt.Errorf("%s: no structure %s", a.asmRel, def["STRUCTID"])
	}
	a.file = "articles/" + a.name + ".md"
	var modules func(n *Node, page string) ([]*module, error)
	modules = func(n *Node, page string) ([]*module, error) {
		var out []*module
		for _, m := range n.Elements("module") {
			mod := &module{page: page, id: m.Attr["id"]}
			if ref := m.Attr["resourceref"]; ref != "" {
				rel, ok := resources[ref]
				if !ok {
					return nil, fmt.Errorf("%s: module %q names no resource", a.asmRel, ref)
				}
				mod.resource = rel
			}
			if mg := m.Child("merge"); mg != nil {
				if t := mg.Child("title"); t != nil {
					mod.title = strings.TrimSpace(spaces.ReplaceAllString(plain(t), " "))
				}
				if ab := mg.Child("abstract"); ab != nil {
					mod.abstract = strings.TrimSpace(spaces.ReplaceAllString(plain(ab), " "))
				}
			}
			// An appendix or glossary of the article is a page of its own
			// on the site, named by its id.
			renderas := m.Attr["renderas"]
			if o := m.Child("output"); o != nil && o.Attr["renderas"] != "" {
				renderas = o.Attr["renderas"]
			}
			if n == a.st && (renderas == "appendix" || renderas == "glossary") {
				mod.page = "" // the topic's id, once read
			}
			if mod.resource != "" && a.byRel[mod.resource] == nil {
				src, err := c.read(mod.resource, prof)
				if err != nil {
					return nil, err
				}
				id := src.root.Attr["id"]
				if id == "" {
					return nil, fmt.Errorf("%s: the topic has no xml:id", mod.resource)
				}
				if mod.page == "" {
					mod.page = id + ".html"
				}
				t := &topic{src: src, id: id, page: mod.page, key: sha256.Sum256([]byte(src.root.String()))}
				a.topics = append(a.topics, t)
				a.byRel[mod.resource] = t
			}
			if mod.page == "" {
				mod.page = "index.html"
			}
			if mod.children, err = modules(m, mod.page); err != nil {
				return nil, err
			}
			out = append(out, mod)
		}
		return out, nil
	}
	a.tree, err = modules(a.st, "index.html")
	return a, err
}

// Convert converts the articles of build definitions into documents in
// Out: one per topic (topics/ID.md; a topic that articles profile
// differently, one per variant, topics/ID--ARTICLE.md), one index per
// article (articles/NAME.md), the collection's index (README.md).
func Convert(o Options) (*Report, error) {
	if o.SourceURL == "" {
		o.SourceURL = "https://github.com/SUSE/doc-modular/blob/main/%s"
	}
	if len(o.DCs) == 0 {
		return nil, fmt.Errorf("no build definition")
	}
	c := &conversion{o: o, unresolved: map[string]bool{}}
	var arts []*article
	names := map[string]bool{}
	for _, d := range o.DCs {
		a, err := c.load(d)
		if err != nil {
			return nil, err
		}
		if names[a.name] {
			return nil, fmt.Errorf("%s: named twice", d)
		}
		names[a.name] = true
		arts = append(arts, a)
	}

	// Documents: a topic is topics/ID.md; one that articles profile
	// differently, topics/ID--ARTICLE.md per variant, named by the first
	// article that has it. A topic that articles profile alike but whose
	// links lead to different variants is split by its rendered text,
	// until no document differs between articles.
	var unknown map[string]int
	var broken map[string]bool
	var conflicts []string
	for round := 0; ; round++ {
		assignFiles(arts)
		unknown, broken = map[string]int{}, map[string]bool{}
		for _, a := range arts {
			a.renderer = a.links(unknown, broken)
			r := a.renderer
			a.title, a.desc, a.abstract, a.tasks = articleMeta(r, a.st)
			r.links[a.st.Attr["id"]] = Target{File: a.file, Title: a.title, Anchor: a.st.Attr["id"]}
			for _, t := range a.topics {
				r.file = t.file
				t.body = join(r.section(t.src.root, 1))
			}
		}
		bodies := map[string]string{}
		split := map[string]bool{}
		for _, a := range arts {
			for _, t := range a.topics {
				if prev, ok := bodies[t.file]; !ok {
					bodies[t.file] = t.body
				} else if prev != t.body {
					split[t.id] = true
				}
			}
		}
		if len(split) == 0 {
			break
		}
		if round == 4 {
			for id := range split {
				conflicts = append(conflicts, id)
			}
			sort.Strings(conflicts)
			break
		}
		for _, a := range arts {
			for _, t := range a.topics {
				if split[t.id] {
					t.key = sha256.Sum256([]byte(t.src.root.String() + "\x00" + t.body))
				}
			}
		}
	}

	for _, d := range []string{"topics", "articles"} {
		if err := os.MkdirAll(filepath.Join(o.Out, d), 0o755); err != nil {
			return nil, err
		}
	}
	written := map[string]bool{}
	docs := 0
	var large []string
	for _, a := range arts {
		r := a.renderer
		for _, t := range a.topics {
			if written[t.file] {
				continue
			}
			written[t.file] = true
			desc := ""
			if info := t.src.root.Child("info"); info != nil {
				if ab := info.Child("abstract"); ab != nil {
					desc = strings.Join(r.paragraphText(ab.Children), " ")
				}
			}
			fields := [][2]any{{"title", r.title(t.src.root)}}
			if desc != "" {
				fields = append(fields, [2]any{"description", desc})
			}
			if role := t.src.root.Attr["role"]; role != "" {
				fields = append(fields, [2]any{"tags", []string{role}})
			}
			if legalTopic(t) {
				// Licenses and disclaimers answer no question: out of
				// find_sections' ranking.
				fields = append(fields, [2]any{"rank", false})
			}
			fields = append(fields, [2]any{"source", c.sourceOf(a, t.src.rel, t.page, t.id)})
			text := frontMatter(fields) + t.body
			if err := os.WriteFile(filepath.Join(o.Out, t.file), []byte(text), 0o644); err != nil {
				return nil, err
			}
			docs++
			if o.MaxSection > 0 && !legalTopic(t) {
				l, err := largeSections(text, int64(o.MaxSection))
				if err != nil {
					return nil, err
				}
				for _, sec := range l {
					large = append(large, t.file+sec)
				}
			}
		}

		// The article's index: its topics in order and nesting.
		r.file = a.file
		var idx strings.Builder
		fields := [][2]any{{"title", a.title}}
		if a.desc != "" {
			fields = append(fields, [2]any{"description", a.desc})
		}
		// An article's index is the way to its topics, not an answer:
		// out of find_sections' ranking, where its titles of every topic
		// outranked the topic that answers.
		fields = append(fields, [2]any{"tags", append([]string{"article"}, a.tasks...)}, [2]any{"rank", false},
			[2]any{"source", c.sourceOf(a, a.asmRel, "index.html", "")})
		idx.WriteString(frontMatter(fields))
		fmt.Fprintf(&idx, "# %s {#%s}\n\n", a.title, a.st.Attr["id"])
		if a.abstract != "" {
			idx.WriteString(a.abstract + "\n\n")
		}
		idx.WriteString("The article's topics, in order:\n\n")
		var list func([]*module, string)
		list = func(ms []*module, indent string) {
			for _, m := range ms {
				t := a.byRel[m.resource]
				title := m.title
				switch {
				case t == nil:
					fmt.Fprintf(&idx, "%s- **%s**", indent, title)
					if m.abstract != "" {
						fmt.Fprintf(&idx, ": %s", m.abstract)
					}
					idx.WriteString("\n")
				default:
					if title == "" {
						title = r.links[t.id].Title
					}
					fmt.Fprintf(&idx, "%s- [%s](%s)\n", indent, title, relPath("articles", t.file))
				}
				list(m.children, indent+"  ")
			}
		}
		list(a.tree, "")
		if err := os.WriteFile(filepath.Join(o.Out, a.file), []byte(idx.String()), 0o644); err != nil {
			return nil, err
		}
		docs++
	}

	// The collection's index: the articles' titles, grouped by their
	// first task ("Security"), short enough to read whole; the articles'
	// descriptions are in their own front matter (list_docs).
	var readme strings.Builder
	readme.WriteString(frontMatter([][2]any{{"title", o.Title}, {"description", o.Description}}))
	fmt.Fprintf(&readme, "# %s\n\n%s\n\n## Articles\n", o.Title, o.Description)
	groups := map[string][]*article{}
	for _, a := range arts {
		g := "Other"
		if len(a.tasks) > 0 {
			g = a.tasks[0]
		}
		groups[g] = append(groups[g], a)
	}
	order := make([]string, 0, len(groups))
	for g := range groups {
		if g != "Other" {
			order = append(order, g)
		}
	}
	sort.Strings(order)
	if groups["Other"] != nil {
		order = append(order, "Other")
	}
	for _, g := range order {
		if len(groups) > 1 {
			fmt.Fprintf(&readme, "\n### %s\n", g)
		}
		readme.WriteString("\n")
		for _, a := range groups[g] {
			fmt.Fprintf(&readme, "- [%s](%s)\n", a.title, a.file)
		}
	}
	readme.WriteString("\n## Source and license\n\n")
	dcs := make([]string, len(o.DCs))
	for i, d := range o.DCs {
		dcs[i] = path.Base(d)
	}
	if len(dcs) > 3 {
		dcs = []string{fmt.Sprintf("%d build definitions", len(o.DCs))}
	}
	fmt.Fprintf(&readme, "Converted by mcp-docs-convert from SUSE's DocBook sources (%s, %s",
		strings.TrimSuffix(fmt.Sprintf(o.SourceURL, ""), "/"), strings.Join(dcs, ", "))
	if o.Revision != "" {
		fmt.Fprintf(&readme, ", revision %s", o.Revision)
	}
	readme.WriteString(")")
	if o.SiteURL != "" {
		readme.WriteString("; each document's source is its page on documentation.suse.com")
	}
	readme.WriteString(". Section ids are the sources' xml:ids. Copyright SUSE LLC and contributors; " +
		"licensed under the GNU Free Documentation License 1.2 or (at your option) 1.3, with the copyright " +
		"notice and license as the Invariant Section")
	// The legal notices are the sources' shared files (common/).
	var legal []string
	seen := map[string]bool{}
	for _, a := range arts {
		for _, t := range a.topics {
			if legalTopic(t) && !seen[t.file] {
				seen[t.file] = true
				legal = append(legal, fmt.Sprintf("[%s](%s)", a.renderer.links[t.id].Title, t.file))
			}
		}
	}
	if len(legal) > 0 {
		readme.WriteString(": " + strings.Join(legal, ", "))
	}
	readme.WriteString(".\n")
	if err := os.WriteFile(filepath.Join(o.Out, "README.md"), []byte(readme.String()), 0o644); err != nil {
		return nil, err
	}
	docs++

	rep := &Report{Articles: len(arts), Documents: docs, Unknown: unknown, Conflicts: conflicts,
		Large: large, MaxSection: o.MaxSection}
	for u := range c.unresolved {
		rep.Unresolved = append(rep.Unresolved, u)
	}
	for b := range broken {
		rep.Broken = append(rep.Broken, b)
	}
	sort.Strings(rep.Unresolved)
	sort.Strings(rep.Broken)
	return rep, nil
}

// legalTopic reports whether a topic is a legal notice: the sources'
// shared files (common/), the license and the disclaimer.
func legalTopic(t *topic) bool { return strings.HasPrefix(t.src.rel, "common/") }

// largeSections are the ids ("#ID (12.3 KiB)") of the sections of a
// document whose own text, to the next heading, is over max bytes.
func largeSections(text string, max int64) ([]string, error) {
	o, err := markdown.Scan(context.Background(), strings.NewReader(text))
	if err != nil {
		return nil, err
	}
	var out []string
	for i, h := range o.Headings {
		own := h.Bytes
		for _, c := range o.Headings[i+1:] {
			if c.Parent == i {
				own -= c.Bytes
			}
		}
		if own > max {
			out = append(out, fmt.Sprintf("#%s (%s)", h.ID, kib(own)))
		}
	}
	return out, nil
}

func kib(n int64) string { return fmt.Sprintf("%.1f KiB", float64(n)/1024) }

// sourceOf is where a document comes from: its page and anchor on
// documentation.suse.com, or else its source file.
func (c *conversion) sourceOf(a *article, rel, page, id string) string {
	if c.o.SiteURL == "" {
		return fmt.Sprintf(c.o.SourceURL, rel)
	}
	u := fmt.Sprintf(c.o.SiteURL, a.name)
	if !strings.HasSuffix(u, "/") {
		u += "/"
	}
	u += page
	if id != "" && page == "index.html" {
		u += "#" + id
	}
	return u
}

// headings are the elements rendered as headings, whose ids are
// anchors.
var headings = set("section", "topic", "sect1", "sect2", "sect3", "simplesect", "chapter", "appendix",
	"article", "legalnotice", "glossary", "glossdiv")

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
