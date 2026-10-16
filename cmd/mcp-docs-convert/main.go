// Command mcp-docs-convert converts articles of SUSE's DocBook
// documentation sources (SUSE/doc-modular) into a collection of
// Markdown documents that mcp-docs serves, a section at a time
// (docs/architecture.md, §14):
//
//	mcp-docs-convert --src doc-modular --dcs tools/suse-docs/sles-16.0 \
//	    --out /usr/share/mcp-docs/docs/suse/sles-16.0 --name sles-16.0 \
//	    --title "SUSE Linux Enterprise Server 16.0" \
//	    --site-url https://documentation.suse.com/sles/16.0/html/%s/
//
// It prints what it could not convert; with --strict, that is an error.
package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/sdrahn/mcp-docs/internal/docbook"
)

// list is a flag that can be given more than once.
type list []string

func (l *list) String() string     { return strings.Join(*l, ",") }
func (l *list) Set(v string) error { *l = append(*l, v); return nil }

func main() {
	var o docbook.Options
	var dcs list
	flag.StringVar(&o.Src, "src", "", "the checkout of the sources (SUSE/doc-modular)")
	flag.Var(&dcs, "dc", "a build definition, relative to --src (DC-SLES-SELinux); more than one --dc for more articles")
	dcFile := flag.String("dcs", "", "a file naming build definitions, one per line (# starts a comment)")
	flag.StringVar(&o.Out, "out", "", "the directory to write the documents to")
	flag.StringVar(&o.Name, "name", "suse", "the collection's name")
	flag.StringVar(&o.Title, "title", "SUSE documentation", "the collection's title")
	flag.StringVar(&o.Description, "description", "SUSE's product documentation, converted from its DocBook sources.",
		"the collection's description")
	flag.StringVar(&o.SourceURL, "source-url", "", "URL of a source file, %s its path (default: GitHub, SUSE/doc-modular, main)")
	flag.StringVar(&o.SiteURL, "site-url", "", "URL of an article on documentation.suse.com, %s its build definition without DC- "+
		"(https://documentation.suse.com/sles/16.0/html/%s/); documents' sources are then their pages there")
	flag.StringVar(&o.Revision, "revision", "", "the sources' revision, named in the collection's index")
	strict := flag.Bool("strict", false, "fail when an element, entity or link could not be converted")
	flag.Parse()
	o.DCs = dcs
	if *dcFile != "" {
		names, err := readList(*dcFile)
		if err != nil {
			fmt.Fprintln(os.Stderr, "mcp-docs-convert:", err)
			os.Exit(1)
		}
		o.DCs = append(o.DCs, names...)
	}
	if o.Src == "" || len(o.DCs) == 0 || o.Out == "" || flag.NArg() > 0 {
		fmt.Fprintln(os.Stderr, "usage: mcp-docs-convert --src DIR (--dc DC-FILE ... | --dcs FILE) --out DIR [options]")
		flag.PrintDefaults()
		os.Exit(2)
	}
	rep, err := docbook.Convert(o)
	if err != nil {
		fmt.Fprintln(os.Stderr, "mcp-docs-convert:", err)
		os.Exit(1)
	}
	fmt.Fprintln(os.Stderr, "mcp-docs-convert:", rep)
	if *strict && !rep.OK() {
		os.Exit(1)
	}
}

func readList(file string) ([]string, error) {
	f, err := os.Open(file)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		l, _, _ := strings.Cut(sc.Text(), "#")
		if l = strings.TrimSpace(l); l != "" {
			out = append(out, l)
		}
	}
	return out, sc.Err()
}
