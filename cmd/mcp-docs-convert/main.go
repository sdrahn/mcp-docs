// Command mcp-docs-convert converts an article of SUSE's DocBook
// documentation sources (SUSE/doc-modular) into Markdown documents that
// mcp-docs serves, a section at a time (docs/architecture.md, §14):
//
//	mcp-docs-convert --src doc-modular --dc DC-SLES-SELinux --out /usr/share/mcp-docs/docs/suse/sles-16.0 \
//	    --name sles-16.0 --title "SUSE Linux Enterprise Server 16.0"
//
// It prints what it could not convert; with --strict, that is an error.
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/sdrahn/mcp-docs/internal/docbook"
)

func main() {
	var o docbook.Options
	flag.StringVar(&o.Src, "src", "", "the checkout of the sources (SUSE/doc-modular)")
	flag.StringVar(&o.DC, "dc", "", "the build definition, relative to --src (DC-SLES-SELinux)")
	flag.StringVar(&o.Out, "out", "", "the directory to write the documents to")
	flag.StringVar(&o.Name, "name", "suse", "the collection's name")
	flag.StringVar(&o.Title, "title", "SUSE documentation", "the collection's title")
	flag.StringVar(&o.Description, "description", "SUSE's product documentation, converted from its DocBook sources.",
		"the collection's description")
	flag.StringVar(&o.SourceURL, "source-url", "", "URL of a source file, %s its path (default: GitHub, SUSE/doc-modular, main)")
	flag.StringVar(&o.Revision, "revision", "", "the sources' revision, named in the collection's index")
	strict := flag.Bool("strict", false, "fail when an element, entity or link could not be converted")
	flag.Parse()
	if o.Src == "" || o.DC == "" || o.Out == "" || flag.NArg() > 0 {
		fmt.Fprintln(os.Stderr, "usage: mcp-docs-convert --src DIR --dc DC-FILE --out DIR [options]")
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
