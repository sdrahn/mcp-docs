package asciidoc

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/sdrahn/mcp-docs/internal/markdown"
)

// Options say what to convert, and into what.
type Options struct {
	// Src is the checkout of the sources' repository; every file is read
	// below it.
	Src string
	// Start is the directory of the component version to convert,
	// relative to Src: the one with its antora.yml ("versions/v2.15").
	Start string
	// Descriptor is the component's descriptor, relative to Start, where
	// it is not antora.yml.
	Descriptor string
	// Shared are the directories of other components the pages include
	// from, relative to Src ("shared"): each with its antora.yml.
	Shared []string
	// Module is the module whose pages are converted (default "en": the
	// sources keep a language a module).
	Module string
	// AttributeFiles are YAML files of attributes, relative to Src, read
	// before the descriptor's (a site's global attributes).
	AttributeFiles []string
	// Attributes are set over all others (the playbook's:
	// build-type=product).
	Attributes map[string]string
	// NavOnly converts only the pages the navigation lists: a site that
	// does not publish the others.
	NavOnly bool
	// Out is the directory to write the collection to.
	Out string
	// Title and Description are the collection's, in its index.
	Title, Description string
	// SiteURL is the URL of a page on the site, "%s" its path without
	// ".adoc"; documents' sources are then their pages there.
	SiteURL string
	// SourceURL is the URL of a source file, "%s" its path in Src.
	SourceURL string
	// Revision names the sources' revision, in the index.
	Revision string
	// Product and Version are in every document's front matter.
	Product, Version string
	// License is the sentence on the documents' license, in the index.
	License string
	// MaxSection reports the sections whose own text is over this many
	// bytes (0: none).
	MaxSection int
}

// Report is what a conversion did, and what it could not.
type Report struct {
	Pages int
	// Unknown is what the renderer met and does not know, by what it is
	// and how often; Problems what could not be read. Broken are
	// cross-references that lead to no page or section of the
	// collection: the sources', which the site has too (such a link is
	// its text, or leads to the page), reported and no failure.
	Unknown  map[string]int
	Broken   []string
	Problems []string
	// Large are the sections over Options.MaxSection: a note for the
	// sources' authors, not a loss.
	Large      []string
	MaxSection int
}

// OK reports whether nothing was lost.
func (r *Report) OK() bool {
	return len(r.Unknown) == 0 && len(r.Problems) == 0
}

func (r *Report) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%d page(s)", r.Pages)
	if len(r.Unknown) > 0 {
		var names []string
		for n := range r.Unknown {
			names = append(names, n)
		}
		sort.Strings(names)
		for i, n := range names {
			names[i] = fmt.Sprintf("%s (%d)", n, r.Unknown[n])
		}
		b.WriteString("; not known: " + strings.Join(names, ", "))
	}
	if len(r.Broken) > 0 {
		b.WriteString("; cross-references that lead nowhere: " + strings.Join(r.Broken, ", "))
	}
	if len(r.Problems) > 0 {
		b.WriteString("; not read: " + strings.Join(r.Problems, "; "))
	}
	if len(r.Large) > 0 {
		fmt.Fprintf(&b, "; sections over %s of their own text: %s", size(int64(r.MaxSection)), strings.Join(r.Large, ", "))
	}
	return b.String()
}

func size(n int64) string {
	if n >= 1<<10 {
		return fmt.Sprintf("%.1f KiB", float64(n)/(1<<10))
	}
	return fmt.Sprintf("%d B", n)
}

// descriptor is a component version's antora.yml.
type descriptor struct {
	Name           string   `yaml:"name"`
	Title          string   `yaml:"title"`
	Version        any      `yaml:"version"`
	DisplayVersion string   `yaml:"display_version"`
	Nav            []string `yaml:"nav"`
	Asciidoc       struct {
		Attributes map[string]any `yaml:"attributes"`
	} `yaml:"asciidoc"`
}

// component is a component version: its name and where its files are.
type component struct {
	name string
	dir  string // relative to the sources
}

// page is a page of the component: its source, and what a first reading
// found of it.
type page struct {
	rel      string // relative to the module's pages, with ".adoc"
	doc      string // the document written for it, relative to Out
	title    string
	desc     string
	date     string
	headings []heading
	// anchors are the ids of blocks that are no sections, each with the
	// id of the section it is in ("": the page).
	anchors map[string]string
	body    string
}

type conversion struct {
	o      Options
	root   *os.Root
	src    string
	comps  map[string]*component // by name
	self   *component
	module string
	attrs  map[string]string
	pages  map[string]*page // by rel
	// second is the second reading: links are resolved, and what leads
	// nowhere is reported.
	second   bool
	broken   map[string]bool
	unknown  map[string]int
	problems []string
}

func (c *conversion) read(rel string) ([]byte, error) {
	f, err := c.root.Open(filepath.FromSlash(rel))
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	return io.ReadAll(f)
}

// resourceID is "[version@][component:module:][family$]path", as Antora
// names a file.
type resourceID struct {
	component, module, family, path string
}

func parseResource(id string) resourceID {
	var r resourceID
	if i := strings.Index(id, "@"); i >= 0 && !strings.Contains(id[:i], "/") {
		id = id[i+1:]
	}
	if parts := strings.Split(id, ":"); len(parts) == 3 {
		r.component, r.module, id = parts[0], parts[1], parts[2]
		if r.module == "" {
			r.module = "ROOT"
		}
	} else if len(parts) == 2 {
		r.module, id = parts[0], parts[1]
	}
	if i := strings.Index(id, "$"); i >= 0 {
		r.family, id = id[:i], id[i+1:]
	}
	r.path = id
	return r
}

var families = map[string]string{"page": "pages", "partial": "partials", "example": "examples", "attachment": "attachments", "image": "images"}

// file is the path of a resource in the sources; from is the file that
// names it.
func (c *conversion) file(r resourceID, from string) (string, error) {
	comp := c.self
	if r.component != "" {
		if comp = c.comps[r.component]; comp == nil {
			return "", fmt.Errorf("no component %q among the sources", r.component)
		}
	}
	module := r.module
	if module == "" {
		module = c.module
		if r.component != "" {
			module = "ROOT"
		}
	}
	dir, ok := families[r.family]
	if !ok {
		return "", fmt.Errorf("a %q is not a resource the converter reads", r.family)
	}
	p := path.Clean(path.Join(comp.dir, "modules", module, dir, r.path))
	if !strings.HasPrefix(p, comp.dir+"/") {
		return "", fmt.Errorf("%s leads out of its component", r.path)
	}
	return p, nil
}

// include reads an include's target: a resource id, or a path relative
// to the file that includes it.
func (c *conversion) include(target, from string) ([]string, string, error) {
	var file string
	if strings.Contains(target, "$") {
		f, err := c.file(parseResource(target), from)
		if err != nil {
			return nil, "", err
		}
		file = f
	} else {
		file = path.Clean(path.Join(path.Dir(from), target))
	}
	b, err := c.read(file)
	if err != nil {
		return nil, "", fmt.Errorf("%s: not there", file)
	}
	return strings.Split(strings.TrimSuffix(string(b), "\n"), "\n"), file, nil
}

// xref resolves a cross-reference from a page of the collection.
func (c *conversion) xref(target, from string) (string, string, bool) {
	target, fragment, _ := strings.Cut(target, "#")
	if !c.second {
		return "x", "", true
	}
	r := parseResource(target)
	if r.family != "" && r.family != "page" {
		return "", "", false // an attachment, an image: not in the collection
	}
	if (r.component != "" && r.component != c.self.name) || (r.module != "" && r.module != c.module) {
		return "", "", false
	}
	rel := r.path
	if strings.HasPrefix(rel, "./") || strings.HasPrefix(rel, "../") {
		rel = path.Join(path.Dir(from), rel)
	}
	rel = strings.TrimPrefix(path.Clean(rel), "/")
	if !strings.HasSuffix(rel, ".adoc") {
		rel += ".adoc"
	}
	p := c.pages[rel]
	if p == nil {
		// Written from the page's own directory, as Antora does not
		// read it and its authors do: a page beside this one.
		p = c.pages[path.Join(path.Dir(from), rel)]
	}
	if p == nil {
		return "", "", false
	}
	rel = p.rel
	href, err := filepath.Rel(path.Dir(c.pages[from].doc), p.doc)
	if err != nil {
		return "", "", false
	}
	href = filepath.ToSlash(href)
	title := p.title
	if fragment != "" {
		id, ok := p.section(fragment)
		if !ok {
			c.broken[strings.TrimSuffix(from, ".adoc")+" → "+strings.TrimSuffix(rel, ".adoc")+"#"+fragment] = true
		} else if id != "" {
			href += "#" + id
			for _, h := range p.headings {
				if h.id == id && fragment == id {
					title = h.title
				}
			}
		}
	}
	return href, title, true
}

// section finds the section a fragment names: a section's id, or the
// section an anchored block is in.
func (p *page) section(fragment string) (string, bool) {
	for _, h := range p.headings {
		if h.id == fragment {
			if h.level == 1 {
				return "", true
			}
			return fragment, true
		}
	}
	if id, ok := p.anchors[fragment]; ok {
		return id, true
	}
	// The id of the section's title, where the section is given another.
	for _, h := range p.headings {
		if h.auto == fragment {
			if h.level == 1 {
				return "", true
			}
			return h.id, true
		}
	}
	return "", false
}

// Convert converts the pages of a component version into documents in
// Out, a document a page at the page's path, and the collection's index
// (README.md) from the component's navigation.
func Convert(o Options) (*Report, error) {
	if o.Module == "" {
		o.Module = "en"
	}
	if o.Descriptor == "" {
		o.Descriptor = "antora.yml"
	}
	src, err := filepath.Abs(o.Src)
	if err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(src)
	if err != nil {
		return nil, err
	}
	defer func() { _ = root.Close() }()
	c := &conversion{o: o, root: root, src: src, comps: map[string]*component{}, module: o.Module, attrs: map[string]string{},
		pages: map[string]*page{}, broken: map[string]bool{}, unknown: map[string]int{}}

	// Attributes: the site's, the component's, then those given.
	for _, f := range o.AttributeFiles {
		b, err := c.read(f)
		if err != nil {
			return nil, err
		}
		var m map[string]any
		if err := yaml.Unmarshal(b, &m); err != nil {
			return nil, fmt.Errorf("%s: %v", f, err)
		}
		c.setAttrs(m)
	}
	start := path.Clean(filepath.ToSlash(o.Start))
	b, err := c.read(path.Join(start, o.Descriptor))
	if err != nil {
		return nil, err
	}
	var d descriptor
	if err := yaml.Unmarshal(b, &d); err != nil {
		return nil, fmt.Errorf("%s: %v", path.Join(start, o.Descriptor), err)
	}
	if d.Name == "" {
		return nil, fmt.Errorf("%s: no name", path.Join(start, o.Descriptor))
	}
	c.self = &component{name: d.Name, dir: start}
	c.comps[d.Name] = c.self
	c.setAttrs(d.Asciidoc.Attributes)
	c.attrs["page-component-name"], c.attrs["page-component-title"] = d.Name, d.Title
	c.attrs["page-component-version"] = fmt.Sprint(d.Version)
	c.attrs["page-module"] = o.Module
	for k, v := range o.Attributes {
		c.attrs[k] = v
	}
	for _, s := range o.Shared {
		dir := path.Clean(filepath.ToSlash(s))
		b, err := c.read(path.Join(dir, "antora.yml"))
		if err != nil {
			return nil, err
		}
		var sd descriptor
		if err := yaml.Unmarshal(b, &sd); err != nil || sd.Name == "" {
			return nil, fmt.Errorf("%s: no component's descriptor", path.Join(dir, "antora.yml"))
		}
		c.comps[sd.Name] = &component{name: sd.Name, dir: dir}
	}

	// The pages, and the navigation.
	pagesDir := path.Join(start, "modules", o.Module, "pages")
	err = fs.WalkDir(root.FS(), pagesDir, func(p string, e fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !e.IsDir() && strings.HasSuffix(p, ".adoc") {
			rel := strings.TrimPrefix(p, pagesDir+"/")
			c.pages[rel] = &page{rel: rel, doc: strings.TrimSuffix(rel, ".adoc") + ".md"}
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("%s: %v", pagesDir, err)
	}
	nav := c.navigation(d.Nav)
	if o.NavOnly {
		listed := map[string]bool{}
		for _, n := range nav {
			listed[n.rel] = true
		}
		for rel := range c.pages {
			if !listed[rel] {
				delete(c.pages, rel)
			}
		}
	}
	if len(c.pages) == 0 {
		return nil, fmt.Errorf("%s: no pages", pagesDir)
	}
	// A document README.md is the collection's index: a page of that
	// name has another.
	for _, p := range c.pages {
		if strings.EqualFold(p.doc, "README.md") {
			p.doc = "README-page.md"
		}
	}
	rels := make([]string, 0, len(c.pages))
	for rel := range c.pages {
		rels = append(rels, rel)
	}
	sort.Strings(rels)

	// Twice: the first reading finds the pages' titles and sections,
	// which the second one's links are made of.
	for _, second := range []bool{false, true} {
		c.second = second
		c.unknown, c.problems = map[string]int{}, nil
		for _, rel := range rels {
			if err := c.convert(c.pages[rel], pagesDir); err != nil {
				return nil, err
			}
		}
	}

	rep := &Report{Pages: len(rels), Unknown: c.unknown, Problems: c.problems, MaxSection: o.MaxSection}
	for b := range c.broken {
		rep.Broken = append(rep.Broken, b)
	}
	sort.Strings(rep.Broken)
	for _, rel := range rels {
		p := c.pages[rel]
		file := filepath.Join(o.Out, filepath.FromSlash(p.doc))
		if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
			return nil, err
		}
		fields := [][2]any{{"title", p.title}}
		if p.desc != "" {
			fields = append(fields, [2]any{"description", p.desc})
		}
		if o.Product != "" {
			fields = append(fields, [2]any{"product", o.Product})
		}
		if o.Version != "" {
			fields = append(fields, [2]any{"version", o.Version})
		}
		if p.date != "" {
			fields = append(fields, [2]any{"date", p.date})
		}
		if o.SiteURL != "" {
			fields = append(fields, [2]any{"source", urlOf(o.SiteURL, strings.TrimSuffix(rel, ".adoc"))})
		} else if o.SourceURL != "" {
			fields = append(fields, [2]any{"source", urlOf(o.SourceURL, path.Join(pagesDir, rel))})
		}
		text := frontMatter(fields) + p.body
		if err := os.WriteFile(file, []byte(text), 0o644); err != nil {
			return nil, err
		}
		if o.MaxSection > 0 {
			rep.Large = append(rep.Large, largeSections(p.doc, text, int64(o.MaxSection))...)
		}
	}
	if err := os.WriteFile(filepath.Join(o.Out, "README.md"), []byte(c.index(nav, d)), 0o644); err != nil {
		return nil, err
	}
	return rep, nil
}

func (c *conversion) setAttrs(m map[string]any) {
	for k, v := range m {
		switch x := v.(type) {
		case nil:
			c.attrs[k] = ""
		case bool:
			if x {
				c.attrs[k] = ""
			} else {
				delete(c.attrs, k)
			}
		default:
			// A value that ends in "@" may be set anew by a page: here it
			// is the value.
			c.attrs[k] = strings.TrimSuffix(fmt.Sprint(x), "@")
		}
	}
}

// convert reads a page and renders it.
func (c *conversion) convert(p *page, pagesDir string) error {
	file := path.Join(pagesDir, p.rel)
	b, err := c.read(file)
	if err != nil {
		return err
	}
	attrs := make(map[string]string, len(c.attrs)+8)
	for k, v := range c.attrs {
		attrs[k] = v
	}
	pre := &preprocessor{attrs: attrs, include: c.include}
	lines := pre.run(strings.Split(strings.TrimSuffix(string(b), "\n"), "\n"), file, 0)
	c.problems = append(c.problems, pre.problems...)
	// The page's title is read with the attributes its header sets after
	// it ("= Release {release-version}").
	for i, l := range lines {
		if strings.HasPrefix(l, "= ") {
			lines[i] = pre.refs(l)
			break
		}
	}

	anchors, titles := map[string]string{}, map[string]string{}
	for _, h := range p.headings {
		anchors[h.id] = h.title
		if h.auto != h.id {
			anchors[h.auto] = h.title
		}
		if _, ok := titles[h.title]; !ok && h.level > 1 {
			titles[h.title] = h.id
		}
	}
	r := &renderer{in: &inliner{links: c, page: p.rel, anchors: anchors, titles: titles, broken: map[string]bool{}}, ids: map[string]int{},
		unknown: c.unknown, anchors: map[string]string{}}
	if c.second {
		first := *p // the sections the first reading found
		r.in.local = first.section
	}
	blocks := r.blocks(lines)
	p.headings, p.anchors = r.headings, r.anchors
	p.title = ""
	for _, h := range r.headings {
		if h.level == 1 {
			p.title = h.title
			break
		}
	}
	if p.title == "" {
		// A page without a title of its own: its file's name, as a
		// heading the document then has.
		p.title = strings.TrimSuffix(path.Base(p.rel), ".adoc")
		if t := attrs["navtitle"]; t != "" {
			p.title = t
		}
		blocks = append([]block{{"# " + escapeText(p.title)}}, blocks...)
	}
	p.desc = strings.Join(strings.Fields(attrs["description"]), " ")
	p.date = ""
	if d := attrs["revdate"]; dateLike.MatchString(d) {
		p.date = d
	} else if d := attrs["page-revdate"]; dateLike.MatchString(d) {
		p.date = d
	}
	p.body = join(blocks)
	if c.second {
		for t := range r.in.broken {
			c.broken[strings.TrimSuffix(p.rel, ".adoc")+" → "+t] = true
		}
	}
	return nil
}

var dateLike = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

// navItem is an entry of the navigation: a page, or a text that groups
// the entries below it.
type navItem struct {
	depth int
	rel   string // "" for a text
	text  string
}

var navLine = regexp.MustCompile(`^(\*+)[ \t]+(.*)$`)

// navigation reads the module's navigation files.
func (c *conversion) navigation(files []string) []navItem {
	var out []navItem
	for _, f := range files {
		if !strings.Contains(f, "modules/"+c.module+"/") {
			continue
		}
		file := path.Join(c.self.dir, f)
		b, err := c.read(file)
		if err != nil {
			c.problems = append(c.problems, fmt.Sprintf("%s: not there", file))
			continue
		}
		attrs := make(map[string]string, len(c.attrs))
		for k, v := range c.attrs {
			attrs[k] = v
		}
		pre := &preprocessor{attrs: attrs, include: c.include}
		for _, l := range pre.run(strings.Split(string(b), "\n"), file, 0) {
			m := navLine.FindStringSubmatch(l)
			if m == nil {
				continue
			}
			it := navItem{depth: len(m[1]), text: plainText(m[2])}
			if x := xrefMacro.FindStringSubmatch(m[2]); x != nil {
				target, _, _ := strings.Cut(x[1], "#")
				r := parseResource(target)
				if r.component == "" && (r.module == "" || r.module == c.module) {
					it.rel = path.Clean(r.path)
				}
				it.text = linkText(x[2])
			}
			out = append(out, it)
		}
	}
	return out
}

// index is the collection's index: the navigation's upper two levels,
// each page with its title, short enough to read whole; the pages below
// are found by their sections.
func (c *conversion) index(nav []navItem, d descriptor) string {
	o := c.o
	title, desc := o.Title, o.Description
	if title == "" {
		title = d.Title
	}
	var b strings.Builder
	// The way in, not an answer: out of the ranking.
	b.WriteString(frontMatter([][2]any{{"title", title}, {"description", desc}, {"rank", false}}))
	fmt.Fprintf(&b, "# %s\n\n", title)
	if desc != "" {
		b.WriteString(desc + "\n\n")
	}
	b.WriteString("## Pages\n\n")
	listed := map[string]bool{}
	below := 0
	flush := func() {
		if below > 0 {
			fmt.Fprintf(&b, " (and %d below)", below)
		}
		below = 0
	}
	first := true
	for _, n := range nav {
		p := c.pages[n.rel]
		if n.rel != "" && p == nil {
			continue
		}
		if n.depth > 2 {
			if p != nil && !listed[n.rel] {
				below++
				listed[n.rel] = true
			}
			continue
		}
		flush()
		if !first {
			b.WriteString("\n")
		}
		first = false
		text := n.text
		if p != nil {
			if text == "" {
				text = p.title
			}
			listed[n.rel] = true
			fmt.Fprintf(&b, "%s- [%s](%s)", strings.Repeat("  ", n.depth-1), escapeText(text), p.doc)
		} else {
			fmt.Fprintf(&b, "%s- %s", strings.Repeat("  ", n.depth-1), escapeText(text))
		}
	}
	flush()
	b.WriteString("\n")
	// Pages the navigation does not list.
	var rest []string
	for rel, p := range c.pages {
		if !listed[rel] {
			rest = append(rest, fmt.Sprintf("- [%s](%s)", escapeText(p.title), p.doc))
		}
	}
	if len(rest) > 0 && len(rest) <= 40 {
		sort.Strings(rest)
		b.WriteString("\n## Other pages\n\n" + strings.Join(rest, "\n") + "\n")
	}
	b.WriteString("\n## Source and license\n\n")
	fmt.Fprintf(&b, "Converted by mcp-docs-convert from the documentation's AsciiDoc sources")
	if o.SourceURL != "" {
		fmt.Fprintf(&b, " (%s", strings.TrimSuffix(urlOf(o.SourceURL, c.self.dir), "/"))
		if o.Revision != "" {
			fmt.Fprintf(&b, ", revision %s", o.Revision)
		}
		b.WriteString(")")
	}
	if o.SiteURL != "" {
		b.WriteString("; each document's `source` is its page on the site")
	}
	b.WriteString(". Section ids are the sources' own.")
	if o.License != "" {
		b.WriteString(" " + o.License)
	}
	b.WriteString("\n")
	return b.String()
}

// urlOf is a URL pattern with its "%s" replaced by a name.
func urlOf(pattern, name string) string { return strings.ReplaceAll(pattern, "%s", name) }

func frontMatter(fields [][2]any) string {
	var b strings.Builder
	b.WriteString("---\n")
	for _, f := range fields {
		v, _ := json.Marshal(f[1])
		fmt.Fprintf(&b, "%s: %s\n", f[0], v)
	}
	b.WriteString("---\n\n")
	return b.String()
}

// largeSections are the sections of a document whose own text is over
// max bytes.
func largeSections(doc, text string, max int64) []string {
	o, err := markdown.Scan(context.Background(), strings.NewReader(text))
	if err != nil {
		return nil
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
			out = append(out, fmt.Sprintf("%s#%s (%s)", doc, h.ID, size(own)))
		}
	}
	return out
}
