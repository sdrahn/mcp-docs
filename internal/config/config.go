// Package config reads collection files: one YAML file per collection
// in collections.d directories, shipped by the packages whose
// documentation they describe (/usr/share/mcp-docs/collections.d) and
// the administrator's (/etc/mcp-docs/collections.d), later ones
// replacing earlier ones of the same name; an empty file disables the
// collection of its name (docs/architecture.md, §4.2 and D6).
package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/sdrahn/mcp-docs/internal/glob"
)

// DefaultDirs are read when no collection is given on the command line.
var DefaultDirs = []string{"/usr/share/mcp-docs/collections.d", "/etc/mcp-docs/collections.d"}

// Collection is one collection file.
type Collection struct {
	// Name is the collection's name in addresses (default: the file's
	// name without .yaml).
	Name  string `yaml:"name"`
	Title string `yaml:"title"`
	// Root is the directory of the documents (absolute).
	Root        string `yaml:"root"`
	Description string `yaml:"description"`
	// Index is the document to read first, relative to Root (default:
	// README.md or index.md, if there is one).
	Index string `yaml:"index"`
	// Include and Exclude are glob patterns of paths relative to Root:
	// a document is one that Include matches (default: all with a
	// document's extension) and Exclude does not.
	Include []string `yaml:"include"`
	Exclude []string `yaml:"exclude"`
	// Also are single documents elsewhere (absolute paths), named by
	// their base names in the collection.
	Also []string `yaml:"also"`
	// Instructions are added to the server's instructions.
	Instructions string `yaml:"instructions"`

	// File is the file it was read from.
	File string `yaml:"-"`
}

// Load reads the collection files (*.yaml, *.yml) of the directories in
// order, in lexical order within each. A missing directory is no error.
// A file that cannot be used is reported in problems and left out; the
// others are still returned.
func Load(dirs []string) (colls []Collection, problems []error) {
	var order []string
	byName := map[string]*Collection{}
	fromFile := map[string]string{} // a file's base name → its collection's name
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			problems = append(problems, err)
			continue
		}
		var names []string
		for _, e := range entries {
			if n := e.Name(); !e.IsDir() && (strings.HasSuffix(n, ".yaml") || strings.HasSuffix(n, ".yml")) {
				names = append(names, n)
			}
		}
		sort.Strings(names)
		for _, n := range names {
			file := filepath.Join(dir, n)
			base := strings.TrimSuffix(strings.TrimSuffix(n, ".yaml"), ".yml")
			c, disabled, err := read(file)
			if err != nil {
				problems = append(problems, err)
				continue
			}
			if disabled {
				delete(byName, base)
				if name, ok := fromFile[base]; ok {
					delete(byName, name)
				}
				continue
			}
			fromFile[base] = c.Name
			if _, seen := byName[c.Name]; !seen && !contains(order, c.Name) {
				order = append(order, c.Name)
			}
			byName[c.Name] = c
		}
	}
	for _, n := range order {
		if c, ok := byName[n]; ok {
			colls = append(colls, *c)
		}
	}
	return colls, problems
}

func contains(l []string, s string) bool {
	for _, x := range l {
		if x == s {
			return true
		}
	}
	return false
}

// read reads one file; an empty one (or only comments) disables.
func read(file string) (*Collection, bool, error) {
	b, err := os.ReadFile(file)
	if err != nil {
		return nil, false, err
	}
	var c Collection
	d := yaml.NewDecoder(bytes.NewReader(b))
	d.KnownFields(true)
	if err := d.Decode(&c); errors.Is(err, io.EOF) {
		return nil, true, nil
	} else if err != nil {
		return nil, false, fmt.Errorf("%s: %v", file, err)
	}
	c.File = file
	if c.Name == "" {
		c.Name = strings.TrimSuffix(strings.TrimSuffix(filepath.Base(file), ".yaml"), ".yml")
	}
	if err := c.Check(); err != nil {
		return nil, false, fmt.Errorf("%s: %v", file, err)
	}
	return &c, false, nil
}

// Check validates a collection: its name, an absolute root, absolute
// also files, valid patterns, an index inside the root.
func (c *Collection) Check() error {
	if c.Name == "" || strings.ContainsAny(c.Name, "/#") || strings.TrimSpace(c.Name) != c.Name {
		return fmt.Errorf("name %q: give a name without / or #", c.Name)
	}
	if !filepath.IsAbs(c.Root) {
		return fmt.Errorf("root %q: give an absolute path", c.Root)
	}
	c.Root = filepath.Clean(c.Root)
	for i, a := range c.Also {
		if !filepath.IsAbs(a) {
			return fmt.Errorf("also %q: give an absolute path", a)
		}
		c.Also[i] = filepath.Clean(a)
	}
	for _, p := range append(append([]string{}, c.Include...), c.Exclude...) {
		if !glob.Valid(p) {
			return fmt.Errorf("pattern %q: not a valid glob pattern", p)
		}
	}
	if c.Index != "" {
		if filepath.IsAbs(c.Index) || strings.HasPrefix(filepath.Clean(c.Index), "..") {
			return fmt.Errorf("index %q: give a path relative to the root", c.Index)
		}
	}
	c.Description = strings.Join(strings.Fields(c.Description), " ")
	c.Instructions = strings.TrimSpace(c.Instructions)
	return nil
}
